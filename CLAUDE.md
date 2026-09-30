# Agent contract

The estate-wide conventions live in one place and are **not duplicated here**:

**https://github.com/JorisJonkers-dev/workspace/blob/main/CLAUDE.md**

Read it before doing anything non-trivial in this repository. It covers the
things that most often go wrong, including:

- **Pull request labels.** The estate uses a prefixed taxonomy — `type:`,
  `area:`, `component:`, `priority:`, `status:`. Plain `bug` / `enhancement` /
  `documentation` survive in some repositories and not others, so
  `gh pr create` can fail with `'bug' not found`. Run
  `gh label list --repo <owner>/<repo>` once before passing `--label`.
- **Verify the value, not the command.** An exit code, a `Ready` condition or
  an accepted object is not evidence that a consumer sees what you intended.
- Traps around workflow runs, `zsh` word-splitting, and detached submodule
  HEADs.

Duplicating that content into every repository guarantees the copies drift, so
this file stays a pointer. Add repo-specific guidance below.

---

## This repository

- `task check` is what CI runs; `Pipeline Complete` is the only required check.
- Every change under `content/` needs a new release label in `content/site.yml`
  (`task release-gate`), and a `feat(content):` / `fix(content):` title so
  release-please tags an image that carries it.
- Releases: release-please opens the release PR; merging it tags `vX.Y.Z`, which
  triggers `publish.yml`. Never tag by hand.
