# Repository workflow

- Work exclusively with https://github.com/Der-Felix/ytmdl.
- Base development branches on GitHub `dev`; integrate through a GitHub PR.
  Check applicable branch rules and final-commit CI before merging. Never bypass
  branch protection. `main` is the reviewed stable branch; stable promotion,
  release tags and production deployment require separate authorization.
- Inspect and preserve local changes before switching branches. Import only
  reviewed patches onto GitHub history, never private repository history,
  internal configuration, credentials, runtime files or local overrides.
  Push only the intended feature branch, never all branches/tags or a mirror.
- Follow CONTRIBUTING.md for verification. The PR CI is test/build-only.
- Preview rejection must use explicit source markers, not a blanket duration
  cutoff. Valid short full tracks stay eligible. Candidate/source rejection must
  not trigger systemic provider cooldowns. Preserve matching, session recovery,
  bounded fallback and file cleanup unless an explicitly scoped change requires it.
- Tests must use isolated fixtures/databases. Do not generate production traffic,
  retry jobs, change cookies or bypass provider protections as incidental testing.
- Verification diagnostics must exclude credentials, URLs, file paths and raw
  subprocess output. Keep unknown evidence explicitly unknown.
