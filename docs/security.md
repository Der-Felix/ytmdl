# Users & Security

YTMDL is designed for multi-user self-hosting environments with strict authentication and role-based access control.

## Authentication & Passwords

- **Argon2id Hashing:** User passwords are encrypted using modern Argon2id password derivation parameters.
- **Session Tokens:** Cryptographically random session tokens stored server-side in PostgreSQL with expiration tracking.
- **CSRF Protection:** Mutating state endpoints require valid CSRF tokens with double-submit cookie validation. Before sign-in the token is random and issued only by `GET /api/v1/auth/status`. From sign-in on it is HMAC-SHA256 keyed with the session token, so every response of a session carries the same value and a page that cannot read the HttpOnly session cookie cannot compute it. Sign-in replaces the pre-login cookie with that token.
- **Rate Limiting:** In-memory token bucket rate limiters protect login and setup endpoints against brute-force attempts.

## Role-Based Access Control (RBAC)

- **Administrator:** Full management access, including user creation, global settings, storage mount controls, audit repairs, and update checks.
- **User:** Personal library browsing, search, track streaming, and individual download requests.

## Security Vulnerability Reporting

If you discover a security vulnerability in YTMDL, please report it responsibly via **GitHub Private Vulnerability Reporting** on the official GitHub repository.

Reports are acknowledged within 48 hours. Please do not submit sensitive security vulnerabilities via public GitHub issues.
