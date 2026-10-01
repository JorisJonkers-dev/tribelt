package stats

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"time"

	"github.com/JorisJonkers-dev/tribelt/internal/integrations"
)

// IntegrationsPage is the Integrations view: the cards plus the form state of this request.
type IntegrationsPage struct {
	*integrations.View
	CSRF            string
	Flash, FlashErr string
	FlashKind       integrations.Kind
	// Confirm is the Integration whose removal awaits a second POST.
	Confirm integrations.Kind
}

func (s *Service) actor(r *http.Request) integrations.Actor {
	if s.Actor == nil {
		return integrations.Actor{}
	}
	return s.Actor(r)
}

// csrfToken binds a form to the signed-in session; without a session there is no valid token.
func (s *Service) csrfToken(r *http.Request) string {
	if s.Session == nil {
		return ""
	}
	sess := s.Session(r)
	if sess == "" {
		return ""
	}
	mac := hmac.New(sha256.New, s.CSRFKey)
	mac.Write([]byte("tribelt-csrf-v1:" + sess))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Service) csrfOK(r *http.Request) bool {
	want := s.csrfToken(r)
	return want != "" && hmac.Equal([]byte(want), []byte(r.PostFormValue("csrf")))
}

func flashText(action string) string {
	return map[string]string{
		"connect": "Credential saved. It is sealed and never shown again.", "select": "Selection saved; the first sync backfills.",
		"test": "Connection test passed.", "sync": "Sync finished.", "notify": "Submitted to IndexNow.",
		"remove": "Credential removed.", "enable": "Scheduled sync resumed.", "disable": "Scheduled sync paused.",
		"generate": "New IndexNow key saved; submit once the key file is reachable.",
	}[action]
}

func (s *Service) integrationsView(w http.ResponseWriter, r *http.Request) {
	s.renderIntegrations(w, r, "", http.StatusOK)
}

func (s *Service) renderIntegrations(w http.ResponseWriter, r *http.Request, confirm integrations.Kind, status int) {
	q := r.URL.Query()
	f := ParseFilter(q, s.Now())
	f.Path = ""
	p, err := s.chrome(r.Context(), r, "integrations", f)
	if err != nil {
		s.fail(w, err)
		return
	}
	a := s.actor(r)
	pick, _ := integrations.ParseKind(q.Get("pick"))
	v, err := s.Integrations.View(r.Context(), a.Admin, pick)
	if err != nil {
		s.fail(w, err)
		return
	}
	page := IntegrationsPage{View: v, Confirm: confirm}
	if a.Admin {
		page.CSRF = s.csrfToken(r)
		page.FlashKind, _ = integrations.ParseKind(q.Get("k"))
		if c := q.Get("err"); c != "" {
			page.FlashErr = integrations.Code(c).Message()
		} else if t := flashText(q.Get("ok")); t != "" {
			page.Flash = t
		}
	}
	p.Data = page
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, "integrations", p); err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func forbidden(w http.ResponseWriter, msg string) {
	http.Error(w, msg, http.StatusForbidden)
}

// readForm parses an urlencoded or multipart form within the credential size limit.
func readForm(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 2*integrations.MaxCredential+(16<<10))
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt == "multipart/form-data" {
		return r.ParseMultipartForm(1 << 20)
	}
	return r.ParseForm()
}

// credential is the uploaded file if one was chosen, else the pasted text.
func credential(r *http.Request) ([]byte, error) {
	if r.MultipartForm != nil {
		if file, _, err := r.FormFile("credential_file"); err == nil {
			defer func() { _ = file.Close() }()
			raw, err := io.ReadAll(io.LimitReader(file, integrations.MaxCredential+1))
			if err != nil {
				return nil, err
			}
			if len(raw) > integrations.MaxCredential {
				return nil, integrations.Fail(integrations.CodeTooLarge)
			}
			if len(raw) > 0 {
				return raw, nil
			}
		}
	}
	text := r.PostFormValue("credential")
	if len(text) > integrations.MaxCredential {
		return nil, integrations.Fail(integrations.CodeTooLarge)
	}
	return []byte(text), nil
}

// integrationsAction handles every change. Order matters: admin first, then CSRF, then the action.
func (s *Service) integrationsAction(w http.ResponseWriter, r *http.Request) {
	a := s.actor(r)
	if !a.Admin {
		s.Log.Warn("integration change refused: not an admin", "sub", a.Sub)
		forbidden(w, "Only an administrator can change integrations.")
		return
	}
	if err := readForm(w, r); err != nil {
		code := integrations.CodeInvalid
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			code = integrations.CodeTooLarge
		}
		s.redirect(w, r, "", "", code)
		return
	}
	if !s.csrfOK(r) {
		s.Log.Warn("integration change refused: bad CSRF token", "sub", a.Sub)
		forbidden(w, "This form has expired. Reload the page and try again.")
		return
	}
	kind, ok := integrations.ParseKind(r.PathValue("kind"))
	action := r.PathValue("action")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if action == "remove" && r.PostFormValue("confirm") != "yes" {
		s.renderIntegrations(w, r, kind, http.StatusOK)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 4*time.Minute)
	defer cancel()
	err := s.runAction(ctx, r, a, kind, action)
	if errors.Is(err, errUnknownAction) {
		http.NotFound(w, r)
		return
	}
	if integrations.CodeOf(err) == integrations.CodeInternal {
		s.Log.Error("integration action failed", "kind", kind, "action", action, "error", err)
	}
	s.redirect(w, r, kind, action, integrations.CodeOf(err))
}

var errUnknownAction = errors.New("unknown action")

func (s *Service) runAction(ctx context.Context, r *http.Request, a integrations.Actor, kind integrations.Kind, action string) error {
	m := s.Integrations
	switch action {
	case "connect":
		secret, err := credential(r)
		if err != nil {
			return err
		}
		return m.Connect(ctx, a, kind, secret, r.PostFormValue("target"))
	case "generate":
		if kind != integrations.IndexNow {
			return errUnknownAction
		}
		return m.GenerateKey(ctx, a)
	case "select":
		return m.Select(ctx, a, kind, r.PostFormValue("target"))
	case "test":
		_, err := m.Test(ctx, a, kind)
		return err
	case "sync", "notify":
		_, err := m.Sync(ctx, &a, kind)
		return err
	case "remove":
		return m.Remove(ctx, a, kind)
	case "enable", "disable":
		return m.SetEnabled(ctx, a, kind, action == "enable")
	}
	return errUnknownAction
}

// redirect answers a POST with a 303 to the page, carrying only a fixed action name or error code.
func (s *Service) redirect(w http.ResponseWriter, r *http.Request, kind integrations.Kind, action string, code integrations.Code) {
	q := url.Values{}
	if kind != "" {
		q.Set("k", string(kind))
	}
	if code != "" {
		q.Set("err", string(code))
	} else if action != "" {
		q.Set("ok", action)
	}
	if action == "connect" && code == "" && (kind == integrations.Google || kind == integrations.Bing) {
		q.Set("pick", string(kind))
	}
	http.Redirect(w, r, "/stats/integrations?"+q.Encode()+"#"+string(kind), http.StatusSeeOther)
}
