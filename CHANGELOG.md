# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.0.6] - 2026-11-03

### Fixed

- **Stalls and dropped requests in Chrome/Brave.** Caddy advertised HTTP/3
  (`Alt-Svc: h3=":443"`) on its container port, but HTTP/3 needs UDP, which
  the compose files do not publish — and on the NAS host port 443 belongs to
  QTS (Sklad answers on 8443). Browsers kept retrying HTTP/3 against the wrong
  service. All Caddyfiles now serve HTTP/1.1 and HTTP/2 only; CI checks it and
  also validates `deploy/Caddyfile.test`.

## [2.0.5] - 2026-11-03

### Fixed

- **Deployment survived no reboot.** On QNAP, `/share` is a RAM disk rebuilt at
  boot; only shared folders under `/share/CACHEDEV1_DATA` persist. The scripts
  installed to `/share/Container/sklad` and backed up to `/share/Backup/sklad`,
  so a reboot wiped the release folder (scripts, Caddyfile, `.env`) and the
  backups; Caddy then could not start. Defaults are now
  `/share/CACHEDEV1_DATA/Feasibility_NAS/Container/sklad` (and `sklad-test`),
  backups go to `<share>/Backup/sklad` next to the install, and `nas-deploy`,
  `nas-replace` and `sklad-stack backup` refuse RAM-disk paths. Data in Docker
  volumes was not affected.
- The scripts find `docker-compose` inside the Container Station package when
  the `/share/Container/bin` symlink is gone (it also lives on the RAM disk).
- `nas-cutover finish` creates a fresh archive of the legacy app when the one
  recorded by `go` is missing, before deleting anything.

## [2.0.4] - 2026-11-02

### Fixed

- `nas-cutover finish` removes the legacy image by ID when its tag has moved
  on (the live `equipment-log` container runs an untagged image), so the old
  app is deleted completely; `status` reports such an image instead of
  calling it deleted.
- `nas-cutover status` / `sklad-stack` print `missing` on one line for a
  container that does not exist yet.

## [2.0.3] - 2026-11-02

### Fixed

- The NAS deploy scripts (`nas-deploy`, `sklad-stack`) now work with QNAP's
  standalone `docker-compose`; they required the `docker compose` plugin, which
  Container Station does not ship, so the first install stopped with
  "'docker compose' is not available".
- `push-to-nas` / `push-test-to-nas` no longer send macOS extended attributes,
  which made the NAS's tar print hundreds of harmless warnings.

### Added

- `./scripts/sklad-stack build` builds `sklad:<SKLAD_IMAGE_TAG>` with the
  release metadata (used by the first install; replaces the manual
  `docker compose build` step in CUTOVER.md and DEPLOY-QNAP.md).

## [2.0.2] - 2026-11-02

Go-live release: security hardening from the readiness review, production
deployment and cut-over tooling, and better scanning of thin labels.

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

- Scanner quiet-zone assist: when a frame does not decode, ZXing retries on
  crops that end inside the label's white margins. Thin CODE128 labels wrapped
  tightly around dark parts (e.g. lens barrels) now read in the live view and
  with Scan from photo; on browsers with a native `BarcodeDetector` it runs on
  every 4th empty frame. `docs/LABELS.md` has printing tips for such parts.
- Production deployment tooling: `scripts/make-bundle` and `scripts/push-to-nas`
  (Mac), `scripts/nas-deploy` (backup → build `sklad:<tag>` → swap → health
  check) and `scripts/sklad-stack` (`preflight`, `backup` with the Caddy CA,
  `restore-db`, `restore-ca`, `rollback`, `history`).
- `docs/DEPLOY-QNAP.md` rewritten as the live runbook: first install, cut-over
  from `equipment-log`, upgrade, rollback, scheduled and off-NAS backups,
  restore drill.
- `scripts/nas-cutover` and `docs/CUTOVER.md`: one-time switch from
  `equipment-log` — archive and stop it, import its inventory, start Sklad,
  `undo` until verified, then `finish` deletes the old container, image,
  folder, data and mDNS file.

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
