# Contributing

## Before you start

- Use the terms in [CONTEXT.md](CONTEXT.md); decisions live in [docs/](docs/).
- Run `mise install`, then `task check` before pushing. CI runs the same targets.

## Pull requests

1. Branch from `main`; one concern per pull request.
2. Title in [Conventional Commits](https://www.conventionalcommits.org/) form (`feat:`, `fix:`,
   `chore:`, `docs:`, `feat!:` for breaking changes). Pull requests are squash-merged and the title
   becomes the commit.
3. Title a change under `content/` `feat(content):` or `fix(content):` so release-please cuts the
   image that ships it; `docs:` and `chore:` never produce a release. Content PRs may share one
   release label, but the release PR fails until `content/site.yml` carries a label the previous
   release did not (README, "Editing content").
4. Regenerate and commit generated code (`task gen`) when queries or migrations change.
5. Do not hand-edit `CHANGELOG.md`; release-please owns it.
6. Never commit secrets.

`Pipeline Complete` is the required status check.

## Security

Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md), never in public issues.
