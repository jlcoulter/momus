# Security Policy

## Supported Versions

Only the latest release of Momus is supported with security updates.

| Version | Supported |
|---------|-----------|
| latest  | ✅ Yes    |
| < latest| ❌ No     |

## Reporting a Vulnerability

If you discover a security vulnerability in Momus, please report it through **GitHub Security Advisories**:

[https://github.com/jlcoulter/momus/security/advisories/new](https://github.com/jlcoulter/momus/security/advisories/new)

This private reporting mechanism ensures that vulnerabilities are disclosed responsibly and that the community has time to patch before details are made public.

### What to include

- A clear description of the vulnerability
- Steps to reproduce (proof of concept is ideal)
- The affected version(s)
- Any potential impact or exploit scenarios

### Response timeline

- **Acknowledgment:** within 48 hours of submission
- **Triage and validation:** within 3 business days
- **Fix or mitigation:** within 7 days of confirmation (depending on severity)
- **Public disclosure:** coordinated with the reporter after a fix is released

We ask that you refrain from publicly disclosing the vulnerability until a fix has been released and announced.

## Scope

The following areas are in scope for security reporting:

- The Momus CLI and library crates
- The mock server (`momus mock`)
- Network request handling and response parsing
- Credential and secret management
- Template resolution and script execution

If you are unsure whether something is in scope, please report it anyway — we would rather be over-notified than miss a critical issue.
