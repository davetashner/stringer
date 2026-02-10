# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| latest  | Yes       |

## Reporting a Vulnerability

If you discover a security vulnerability in Stringer, please report it responsibly.

**Do NOT open a public GitHub issue for security vulnerabilities.**

Instead, please use [GitHub's private vulnerability reporting](https://github.com/davetashner/stringer/security/advisories/new) to submit your report. This ensures the issue can be assessed and addressed before public disclosure.

### What to include

- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

### What to expect

- Acknowledgment within 48 hours
- A fix or mitigation plan within 7 days for confirmed vulnerabilities
- Credit in the release notes (unless you prefer to remain anonymous)

## Security Practices

- All dependencies are monitored via Dependabot and `govulncheck`
- CI runs SAST (gosec via golangci-lint, CodeQL) on every PR
- Releases are signed with Sigstore cosign and include SBOMs
- The repository participates in the [OpenSSF Scorecard](https://securityscorecards.dev/viewer/?uri=github.com/davetashner/stringer) program
