# Security Policy

## Supported versions

Sklad is pre-release; only the latest `v1.x` line will receive security fixes
once released.

| Version | Supported |
|---------|-----------|
| v1.x (latest) | ✅ |
| < v1.0        | ❌ |

## Reporting a vulnerability

**Please do not open a public issue for security problems.**

Report privately using GitHub's **Security → Report a vulnerability** tab on this
repository (private security advisory). If you cannot use that channel, contact
the maintainers through the repository's listed maintainer profile and ask for a
private channel.

Please include:

- a description of the issue and its impact,
- reproduction steps or a proof of concept,
- affected version/commit,
- any suggested mitigation.

We aim to acknowledge reports within a few business days and will keep you
informed as we investigate and prepare a fix. Please give us a reasonable window
to release a fix before public disclosure.

## Scope

In scope include, for example:

- authentication/authorisation bypass, session or CSRF weaknesses,
- SQL injection or unsafe deserialisation,
- secret leakage (logs, API responses, images, repository),
- path traversal in file/export/backup handling,
- privilege escalation between the `viewer` / `editor` / `admin` roles.

Out of scope: issues that require a pre-compromised host or physical access,
denial of service by resource exhaustion on the NAS, and problems caused by a
user installing an untrusted CA onto their device.

## Security design notes

- **HTTPS is mandatory.** The app is served behind Caddy with a local CA; camera
  access requires a trusted secure context. Do not run it over plain HTTP on a
  shared network.
- **Secrets are never committed.** `.env`, the `data/` volume, databases, and
  certificate/key material are gitignored. `SKLAD_ADMIN_PASSWORD` and the
  Discord webhook URL are treated as secrets: never logged, never baked into
  images, and masked in API responses.
- **Sessions** use HttpOnly, Secure, SameSite cookies; state-changing requests
  require a double-submit CSRF token.
- **Passwords** are stored as Argon2id hashes. The first-boot admin is forced to
  change its password on first login.
- **All mutations are audit-logged** (actor, action, before/after).
- **Dependencies** are scanned in CI (e.g. gitleaks for secret leakage); please
  keep dependency manifests committed so this stays effective.

If you operate Sklad, also: set a strong, unique `SKLAD_COOKIE_SECRET`, keep the
NAS and Docker images patched, back up the `data/` volume, and protect access to
the host and container runtime.
