// Package scripts holds tests that drive the repository's shell scripts against throwaway git repos.
package scripts

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const releaseBranch = "release-please--branches--main"

type repo struct {
	t   *testing.T
	dir string
}

// newRepo is a git repo whose first commit holds content/ with the given release label.
func newRepo(t *testing.T, label string) *repo {
	t.Helper()
	r := &repo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	r.write("content/site.yml", site(label))
	r.write("content/pages/nl/home.md", "# Home\n")
	r.write("CHANGELOG.md", "# Changelog\n")
	r.write(".release-please-manifest.json", `{".": "0.1.0"}`)
	r.commit("feat: first")
	return r
}

func site(label string) string {
	return "release:\n  label: " + label + "\n  note: \"a note\"\nbaseUrl: https://example.test\n"
}

func gitEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case k == "HEAD_REF", k == "GITHUB_HEAD_REF", k == "BASE_REF", k == "GITHUB_ACTIONS", strings.HasPrefix(k, "GIT_"):
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.test", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.test")
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir, cmd.Env = r.dir, gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *repo) write(name, body string) {
	r.t.Helper()
	p := filepath.Join(r.dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) commit(msg string) {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
}

// gate runs the script in the repo with extra environment, returning its output and whether it passed.
func (r *repo) gate(env []string, args ...string) (string, bool) {
	r.t.Helper()
	script, err := filepath.Abs("check-content-release.sh")
	if err != nil {
		r.t.Fatal(err)
	}
	cmd := exec.Command(script, args...)
	cmd.Dir, cmd.Env = r.dir, append(gitEnv(), env...)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		r.t.Fatalf("run gate: %v\n%s", err, out)
	}
	return string(out), err == nil
}

var onReleasePR = []string{"HEAD_REF=" + releaseBranch}

// The release PR is the enforcing check; the head ref alone selects it.
func TestReleasePRGate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		setup func(r *repo)
		pass  bool
		want  string
	}{
		{"content and label changed", func(r *repo) {
			r.git("tag", "v0.1.0")
			r.write("content/pages/nl/home.md", "# Home\n\nNew text.\n")
			r.commit("feat(content): new text")
			r.write("content/site.yml", site("v2-new-text"))
			r.commit("feat(content): start v2-new-text")
		}, true, "moved 'v1-baseline' -> 'v2-new-text'"},
		{"content changed, label unchanged", func(r *repo) {
			r.git("tag", "v0.1.0")
			r.write("content/pages/nl/home.md", "# Home\n\nNew text.\n")
			r.commit("feat(content): new text")
		}, false, "content changed since v0.1.0 but release.label is still 'v1-baseline'; bump it in content/site.yml on main"},
		{"no content change", func(r *repo) {
			r.git("tag", "v0.1.0")
			r.write("internal/x.go", "package x\n")
			r.commit("feat: code only")
		}, true, "content/ unchanged since v0.1.0"},
		{"no previous tag", func(r *repo) {
			r.write("content/pages/nl/home.md", "# Home\n\nNew text.\n")
			r.commit("feat(content): new text")
		}, true, "no previous v*.*.* tag"},
		{"only changelog and manifest changed", func(r *repo) {
			r.git("tag", "v0.1.0")
			r.write("CHANGELOG.md", "# Changelog\n\n## 0.2.0\n")
			r.write(".release-please-manifest.json", `{".": "0.2.0"}`)
			r.commit("chore(main): release 0.2.0")
		}, true, "content/ unchanged since v0.1.0"},
		{"latest tag wins over older ones", func(r *repo) {
			r.git("tag", "v0.1.0")
			r.write("content/site.yml", site("v2-new-text"))
			r.commit("feat(content): start v2-new-text")
			r.git("tag", "v0.2.0")
			r.write("content/pages/nl/home.md", "# Home\n\nNewer text.\n")
			r.commit("feat(content): newer text")
		}, false, "content changed since v0.2.0 but release.label is still 'v2-new-text'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newRepo(t, "v1-baseline")
			tc.setup(r)
			out, ok := r.gate(onReleasePR)
			if ok != tc.pass || !strings.Contains(out, tc.want) {
				t.Fatalf("pass=%v, want %v with %q:\n%s", ok, tc.pass, tc.want, out)
			}
			if !strings.Contains(out, "release mode") {
				t.Fatalf("head ref %s must select release mode:\n%s", releaseBranch, out)
			}
		})
	}
}

// Content PRs are advisory: an unchanged label is a notice, never a failure.
func TestContentPRNotice(t *testing.T) {
	t.Parallel()
	r := newRepo(t, "v1-baseline")
	r.git("tag", "v0.1.0")
	r.git("checkout", "-q", "-b", "feat/text")
	r.write("content/pages/nl/home.md", "# Home\n\nNew text.\n")
	r.commit("feat(content): new text")

	env := []string{"HEAD_REF=feat/text", "BASE_REF=main", "GITHUB_ACTIONS=true"}
	out, ok := r.gate(env)
	if !ok || !strings.Contains(out, "pr mode") || !strings.Contains(out, "::notice title=Content Release::content/ changed and release.label is still 'v1-baseline'") {
		t.Fatalf("a content PR without a bump passes with a notice, got pass=%v:\n%s", ok, out)
	}

	r.write("content/site.yml", site("v2-new-text"))
	r.commit("feat(content): start v2-new-text")
	if out, ok := r.gate(env); !ok || strings.Contains(out, "::notice") {
		t.Fatalf("a bumped label needs no notice, got pass=%v:\n%s", ok, out)
	}

	// The same commits on the release branch are what the enforcing check sees.
	r.git("checkout", "-q", "-b", releaseBranch, "HEAD~1")
	if out, ok := r.gate(onReleasePR); ok {
		t.Fatalf("the release PR must fail without the bump:\n%s", out)
	}
}

func TestUsage(t *testing.T) {
	t.Parallel()
	r := newRepo(t, "v1-baseline")
	if out, ok := r.gate(nil, "bogus"); ok || !strings.Contains(out, "usage:") {
		t.Fatalf("unknown mode must fail with usage:\n%s", out)
	}
}
