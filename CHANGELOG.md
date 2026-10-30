# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security

- The first-boot Admin password must be 12+ characters and not a published
  example value; `.env.example` no longer ships a usable placeholder.
- Changing a password signs out every other session of that account.
- CSV export prefixes cells starting with `=`, `+`, `-`, `@`, tab or CR with an
  apostrophe, so spreadsheet applications do not evaluate them as formulas.
- `SKLAD_TRUST_PROXY` defaults to `false`; the compose files set it to `true`
  because Caddy is the only way in.

### Changed

- The server refuses to open a database migrated by a newer release instead of
  running on a schema it does not know (rollback = restore the pre-upgrade
  backup).
- `docker-compose.yml` pins the project name and volume names (`sklad`,
  `sklad_sklad_data`, `sklad_caddy_*`), and tags the image
  `sklad:${SKLAD_IMAGE_TAG:-local}` so the previous release stays available.
- `.env.example` sets `SKLAD_HSTS_MAX_AGE=0` for the device-enrolment period.
- `.env.test.example` and `deploy/Caddyfile.test` are now committed, so the
  test stack works from a fresh clone.

### Added

- Production deployment tooling: `scripts/make-bundle` and `scripts/push-to-nas`
  (Mac), `scripts/nas-deploy` (backup → build `sklad:<tag>` → swap → health
  check) and `scripts/sklad-stack` (`preflight`, `backup` with the Caddy CA,
  `restore-db`, `restore-ca`, `rollback`, `history`).
- `docs/DEPLOY-QNAP.md` rewritten as the live runbook: first install, cut-over
  from `equipment-log`, upgrade, rollback, scheduled and off-NAS backups,
  restore drill.

## [2.0.1] - 2026-10-29

Build and CI fixes only; the application is unchanged from 2.0.0.

### Fixed

- **Fresh checkouts compile:** `web/dist/.gitkeep` is committed, so
  `//go:embed all:dist` no longer fails before the SPA has been built.
- **Multi-arch image:** the web build stage runs on the build platform
  (`FROM --platform=$BUILDPLATFORM`), so the arm64 image no longer dies with
  SIGILL in `npm ci` under QEMU.
- **CI:** gitleaks install (checksum file name, version check by full path),
  `xmllint` installed for the deployment-config job, and the rental e2e test
  matches the "Save selected (N)" label. All six CI jobs pass.

## [2.0.0] - 2026-10-29

**Sklad 2.0** replaces LogApp 1.x (the Python/Flask app with JSON-file storage
in this repository's earlier history) with a ground-up rewrite: Go + SQLite
backend, React SPA, HTTPS via Caddy, packaged for a QNAP NAS. See
`docs/contracts/` and `openapi/openapi.yaml` for the architecture contracts.

### Highlights of the tested 2.0 build

- **Rental workflow:** anonymous Editor mode for rentals (no login on the office
  LAN), Admin-only administration, personnel directory, shared rental buffer
  with individual or all-or-nothing bulk updates, Admin-confirmed returns to
  Feasibility (see `docs/RENTAL-WORKFLOW.md`, `docs/roles-and-functions.md`).
- **Operations:** scheduled consistent SQLite backups (`SKLAD_BACKUP_INTERVAL`),
  offline `reset-admin` recovery, legacy `import-json` tool shipped in the image,
  supported Go builder.
- **Staging on the NAS:** separate test stack at `https://sklad-test.local:8444`
  (`docker-compose.test.yml`, `scripts/test-stack`, `scripts/make-test-bundle`,
  `scripts/push-test-to-nas`, `scripts/nas-replace`, `docs/DEPLOY-TEST.md`).
- **iPhone scanning:** fixed the ZXing fallback (camera frames were passed in
  the wrong pixel format, so iOS Safari never decoded), decode only the visible
  video region, 1080p capture, continuous focus, 1×/2×/3× zoom and torch
  controls, aiming line, and a "Scan from photo" mode using the native camera.
- **Phone layout:** swipeable navigation pills, single control row, one-line
  table actions, 16 px fields (no iOS zoom on focus), date fields that fit,
  tidier rental buffer, filters, export and pagination rows.
- **Fixes:** HTTPS by IP address (`default_sni` in the Caddyfiles),
  `CADDYFILE=./deploy/Caddyfile` in `.env.example`, `SKLAD_COOKIE_SECRET`
  documented as reserved/unused.

### Added

- **Project & OSS hygiene:** MIT `LICENSE` (© David Žitník), `NOTICE`,
  `CONTRIBUTING.md`, `SECURITY.md`, this changelog, `.env.example`,
  `.gitignore`, `.dockerignore`, and the root `go.mod` (module `sklad`).
- **Backend:** Go 1.23 + `chi` HTTP server with request-ID/structured logging
  and a consistent error envelope; `/api/v1/health` and `/api/v1/version`.
- **Storage:** SQLite via CGO-free `modernc.org/sqlite`, WAL mode, append-only
  embedded migrations, and `VACUUM INTO` + JSON snapshot backups with retention.
- **Auth & RBAC:** Argon2id password hashing, SQLite-backed sessions with
  HttpOnly/Secure/SameSite cookies, CSRF protection, login rate limiting, roles
  (`admin`/`editor`/`viewer`), first-boot admin seed with forced password change,
  and an audit log for every mutation.
- **Inventory API & UI:** items CRUD with search/filter/pagination, options
  manager (types/statuses with colours and rename cascade), CSV/JSON export, and
  optimistic concurrency via row `version` (HTTP 409 on conflict).
- **Barcode:** CODE128 label generation for Brother P-Touch E300 tape
  (6/9/12/18 mm) with print preview and SVG/PNG download; in-app camera scanning
  (`BarcodeDetector` with a ZXing fallback for iOS Safari), HID keyboard-wedge
  capture, and manual entry.
- **Scan → buffer → bulk actions:** scan many codes into a buffer, resolve and
  deduplicate against inventory, and apply status/date/note changes to all
  buffered items in a single transactional operation.
- **Notifications:** Discord webhook integration (embeds, batching, 429
  backoff) with a scheduled daily overdue report, a manual "notify now" action,
  a delivery log, and duplicate-send protection.
- **Internationalisation:** runtime language switching for **Czech, English, and
  Italian**, with CI-enforced key parity across locales.
- **Deployment:** multi-stage Dockerfile (non-root, healthcheck),
  `docker-compose.yml` for the app + Caddy, Caddy internal CA issuing
  `sklad.local` certificates (HTTP→HTTPS redirect, security headers, `/ca.crt`
  bootstrap), configurable host ports for QNAP, and Avahi mDNS advertising
  `_https._tcp`.
- **Docs:** architecture, schema, barcode, and configuration contracts, plus
  QNAP deployment, TLS/camera trust, and label-printing runbooks.
- **Migration:** idempotent importer (`scripts/import-json`, `--dry-run`) that
  preserves all legacy `PV-###` IDs verbatim, including the `PV-049` gap.

### Changed

- Replaced the legacy JSON-file storage and in-memory editor lock with SQLite
  and optimistic concurrency.
- Replaced the global admin-credentials flow with session-based auth, RBAC, and
  an audit trail.
- Generalised the legacy "new entries" buffer into a single scan/new-entry
  buffer that supports bulk actions.

### Removed

- **Email/SMTP overdue alerts** — superseded by Discord notifications. All SMTP
  code, UI, and configuration are removed; legacy SMTP settings are ignored
  during import.

### Security

- Secrets (`.env`, databases, certificates/keys, and the `data/` volume) are
  excluded from version control and Docker build contexts.
- HTTP is redirected to HTTPS; session cookies are HttpOnly/Secure/SameSite and
  state-changing requests require a CSRF token.
- The legacy committed SMTP credential is **not** carried over and must be
  revoked by the operator; see [SECURITY.md](SECURITY.md).

## Earlier versions

- **LogApp 1.x** — Python/Flask app with JSON-file storage, an admin edit mode
  and file-lock based editing; the earlier commits of this repository.
- **Internal `equipment-log`** — the internal single-binary Go app with JSON
  file storage, a vanilla-JS UI, and SMTP email alerts that this project
  replaces. It remains available under `equipment-log/` for reference and data
  migration only; it is not maintained.
