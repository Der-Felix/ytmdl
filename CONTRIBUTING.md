# Contributing to YTMDL

Thank you for your interest in contributing to YTMDL! We welcome community contributions, bug reports, feature requests, and improvements.

## Code of Conduct & Community

We expect contributors to maintain a respectful, constructive, and collaborative environment. Be thoughtful, considerate, and focused on building great software together.

## How to Contribute

1. **Fork the Repository**: Create your own fork of the repository on GitHub.
2. **Create a Feature Branch**: Branch off from the current GitHub `dev` with a descriptive name (e.g., `feature/my-new-feature` or `fix/issue-description`).
3. **Keep Changes Focused**: Make small, surgical, and well-tested changes. Avoid mixing unrelated refactors with bugfixes or features.
4. **Run Tests & Linters**: Ensure all backend and frontend checks pass locally before opening a pull request.
5. **Submit a Pull Request**: Open a pull request against `dev` describing your changes and testing approach.

GitHub at [Der-Felix/ytmdl](https://github.com/Der-Felix/ytmdl) is the only
canonical development repository. `dev` is the integration branch; `main` holds
the reviewed stable state. Start new work from GitHub history, preserve local
changes, and merge through a GitHub PR after CI passes for its final commit.
Respect all applicable branch rules; do not bypass them using administrator
permissions. Promotion to `main`, release tags and deployments are separate work.

Do not publish private repository history, internal configuration, credentials,
runtime data or local overrides. Transfer reviewed patches onto a branch based
on GitHub `dev`, rather than merging unrelated/private histories. Configure the
push remote for GitHub; never push all local branches or tags as part of a fix.

## Development Workflow & Verification

`.github/workflows/ci.yml` runs tests/builds on PRs to `dev`/`main` and pushes
to `dev`. It has read-only repository permissions and never releases or deploys.
Backend CI uses a disposable PostgreSQL 18 service; local integration tests also
require `MUSICDL_TEST_DATABASE_URL` pointing to an isolated test database, never
production. See [the development guide](docs/development.md).

### Backend (Go)

```sh
cd backend
gofmt -w .
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
go build ./cmd/server
```

### Frontend (TypeScript / React)

```sh
cd frontend
bun test
bunx tsc -b         # TypeScript check
bun run lint        # oxlint
bun run build       # vite build
```

## Contribution Licensing

Under Section 5 of the Apache License, Version 2.0:

> By intentionally submitting a contribution for inclusion in YTMDL, the contributor agrees that the contribution is submitted under the terms and conditions of the Apache License, Version 2.0, without any additional terms or conditions, unless explicitly stated otherwise in writing.

## Security Vulnerabilities

Please do not report sensitive security vulnerabilities through public GitHub issues. Refer to [SECURITY.md](SECURITY.md) for instructions on confidential disclosure.
