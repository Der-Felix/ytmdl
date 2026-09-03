# Contributing to YTMDL

Thank you for your interest in contributing to YTMDL! We welcome community contributions, bug reports, feature requests, and improvements.

## Code of Conduct & Community

We expect contributors to maintain a respectful, constructive, and collaborative environment. Be thoughtful, considerate, and focused on building great software together.

## How to Contribute

1. **Fork the Repository**: Create your own fork of the repository on GitHub.
2. **Create a Feature Branch**: Branch off from `main` with a descriptive name (e.g., `feature/my-new-feature` or `fix/issue-description`).
3. **Keep Changes Focused**: Make small, surgical, and well-tested changes. Avoid mixing unrelated refactors with bugfixes or features.
4. **Run Tests & Linters**: Ensure all backend and frontend checks pass locally before opening a pull request.
5. **Submit a Pull Request**: Open a pull request against `main` describing your changes and testing approach.

## Development Workflow & Verification

### Backend (Go)

```sh
cd backend
gofmt -w .
go vet ./...
go test -count=1 ./...
go build ./cmd/server
```

### Frontend (TypeScript / React)

```sh
cd frontend
bun test
bun run typecheck   # tsc -b
bun run lint        # oxlint
bun run build       # vite build
```

## Contribution Licensing

Under Section 5 of the Apache License, Version 2.0:

> By intentionally submitting a contribution for inclusion in YTMDL, the contributor agrees that the contribution is submitted under the terms and conditions of the Apache License, Version 2.0, without any additional terms or conditions, unless explicitly stated otherwise in writing.

## Security Vulnerabilities

Please do not report sensitive security vulnerabilities through public GitHub issues. Refer to [SECURITY.md](SECURITY.md) for instructions on confidential disclosure.
