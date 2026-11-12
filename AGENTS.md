# Agents

Guidance for any coding agent working in this repository.

- Agent contract: [CLAUDE.md](CLAUDE.md)
- Estate conventions: https://github.com/JorisJonkers-dev/workspace/blob/main/CLAUDE.md
- Domain vocabulary: [CONTEXT.md](CONTEXT.md)

## Build and test

The toolchain is pinned in `mise.toml` (`mise install`); tasks are defined in `Taskfile.yml`.

- Full local gate (everything CI runs on a pull request except E2E): task check
- Unit tests: task test:unit
- Lint: task lint
- Format: task fmt
- Regenerate sqlc code and check for drift: task gen:check
- Build: task build
