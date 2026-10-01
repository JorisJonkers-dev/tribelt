package integrations

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseServiceAccount(t *testing.T) {
	good := `{"type":"service_account","client_email":"stats@p.iam.gserviceaccount.com","private_key":"-----BEGIN PRIVATE KEY-----\nx\n-----END PRIVATE KEY-----\n"}`
	sa, err := ParseServiceAccount([]byte(good))
	if err != nil || sa.ClientEmail != "stats@p.iam.gserviceaccount.com" {
		t.Fatalf("%+v %v", sa, err)
	}
	for raw, want := range map[string]Code{
		"":   CodeInvalid,
		"{":  CodeInvalidJSON,
		`{}`: CodeNotServiceAccount,
		`{"type":"authorized_user","client_email":"a@b","private_key":"PRIVATE KEY"}`: CodeNotServiceAccount,
		`{"type":"service_account","private_key":"PRIVATE KEY"}`:                      CodeIncompleteKey,
		`{"type":"service_account","client_email":"a@b"}`:                             CodeIncompleteKey,
		strings.Repeat(" ", MaxCredential+1):                                          CodeInvalid,
	} {
		if _, err := ParseServiceAccount([]byte(raw)); CodeOf(err) != want {
			t.Errorf("%.40q: %v, want %s", raw, err, want)
		}
	}
}

func TestValidators(t *testing.T) {
	for s, want := range map[string]bool{
		"0123456789abcdef0123": true, "short": false, "has space in it 12345": false, strings.Repeat("a", 257): false,
	} {
		if ValidAPIKey(s) != want {
			t.Errorf("ValidAPIKey(%q)", s)
		}
	}
	if !ValidZoneID(strings.Repeat("ab", 16)) || ValidZoneID(strings.Repeat("AB", 16)) || ValidZoneID("abc") {
		t.Fatal("zone ids are 32 lowercase hex")
	}
	key, err := NewIndexNowKey()
	if err != nil || len(key) != 32 || !ValidIndexNowKey(key) || ValidIndexNowKey("robots") || ValidIndexNowKey("a/b.txtxxxx") {
		t.Fatalf("indexnow key %q %v", key, err)
	}
	if k, ok := ParseKind("cloudflare"); !ok || k != Cloudflare {
		t.Fatal("known kind")
	}
	if _, ok := ParseKind("vault"); ok {
		t.Fatal("unknown kind")
	}
}

func TestCodes(t *testing.T) {
	all := []Code{
		CodeInvalid, CodeInvalidJSON, CodeNotServiceAccount, CodeIncompleteKey, CodeTooLarge, CodeNoStorage, CodeNotConfigured,
		CodeNoTarget, CodeTargetMissing, CodeKeyChanged, CodeAuth, CodeForbidden, CodeNotFound, CodeRateLimited,
		CodeProviderDown, CodeBadResponse, CodeNetwork, CodeBusy, CodeVaultManaged, CodeKeyMismatch,
	}
	seen := map[string]bool{}
	for _, c := range all {
		m := c.Message()
		if m == "" || m == CodeInternal.Message() || seen[m] {
			t.Errorf("%s: %q", c, m)
		}
		seen[m] = true
	}
	if Code("<script>").Message() != "Something went wrong." {
		t.Fatal("unknown codes render a fixed text")
	}
	for status, want := range map[int]Code{401: CodeAuth, 403: CodeForbidden, 404: CodeNotFound, 429: CodeRateLimited, 503: CodeProviderDown, 400: CodeBadResponse} {
		if statusCode(status) != want {
			t.Errorf("%d", status)
		}
	}
	if CodeOf(nil) != "" || CodeOf(errors.New("x")) != CodeInternal || CodeOf(fmt.Errorf("w: %w", Fail(CodeBusy))) != CodeBusy {
		t.Fatal("CodeOf")
	}
	f := &CodedError{Code: CodeAuth, Err: errors.New("detail")}
	if f.Error() != "auth_failed: detail" || Fail(CodeBusy).Error() != "busy" || !errors.Is(f, f.Err) {
		t.Fatal("CodedError text")
	}
}

// A transport error must not carry the request URL: Bing's API key is in the query string.
func TestTransportErrorsDropTheURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	b := &BingClient{HTTP: http.DefaultClient, BaseURL: "http://127.0.0.1:1", APIKey: "very-secret-bing-key"}
	_, err := b.Sites(ctx)
	if CodeOf(err) != CodeNetwork || strings.Contains(err.Error(), "very-secret-bing-key") || strings.Contains(err.Error(), "apikey") {
		t.Fatalf("leaks the URL: %v", err)
	}
	if err := doJSON(ctx, http.DefaultClient, "GET", "::", nil, nil); CodeOf(err) != CodeInternal {
		t.Fatalf("bad url: %v", err)
	}
	if CodeOf(redact(errors.New("plain"))) != CodeNetwork {
		t.Fatal("redact")
	}
}
