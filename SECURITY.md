# Security Policy

## Supported Versions

Only the latest release of `milanuncios-cli` receives security updates.

| Version | Supported          |
| ------- | ------------------ |
| >= 0.1  | :white_check_mark: |
| < 0.1   | :x:                |

## Reporting a Vulnerability

If you discover a security vulnerability in `milanuncios-cli`, please report it responsibly:

1. **Do NOT** open a public GitHub issue.
2. Use GitHub's private vulnerability reporting feature on the repository: [Report a vulnerability](https://github.com/gek0v/milanuncios-cli/security/advisories/new).
3. Alternatively, contact the maintainers directly via email.

Please include:
- A description of the issue and potential impact.
- Steps or a minimal proof of concept to reproduce the issue.
- Any suggested mitigations or fixes if available.

We will acknowledge receipt within 48 hours and work with you on a coordinated fix and disclosure.

## Security Guarantees & Non-Goals

- **Zero Credential Leaks**: Sensitive session cookies, passwords, and proxy credentials must never be written to `stdout`, logs, or error envelopes.
- **Local Credentials Store**: Session tokens stored locally in `credentials.toml` must strictly maintain `0600` POSIX file permissions.
- **Unofficial Tool Disclaimer**: `milanuncios-cli` is an unofficial community project and is not affiliated with Milanuncios or Adevinta Spain.
