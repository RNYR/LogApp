# Sklad — Architecture

This document records the design of Sklad as shipped in **v1.0.0**. It is
complementary to the frozen contracts in
[docs/contracts](contracts/schema.md) — the schema
([schema.md](contracts/schema.md)), the barcode rules
([barcode.md](contracts/barcode.md)), the configuration surface
([config.md](contracts/config.md)) and the REST API
([openapi/openapi.yaml](../openapi/openapi.yaml)) — and is the map for anyone
navigating the source.

## 1. Big picture

Sklad is a self-hosted, single-tenant equipment inventory & lending tracker that
runs as **two containers** on a QNAP TS-251 and is served at
`https://sklad.local`:

```
┌──────────────┐      HTTPS (80→443)      ┌─────────────────────────────┐
│  browser /   │ ───────────────────────▶ │  Caddy (deploy/Caddyfile)   │
│  phone       │    sklad.local + LAN IP  │  internal CA · HTTP→HTTPS    │
│  (camera)    │ ◀─────────────────────── │  /ca.crt · security headers  │
└──────────────┘                          └───────────┬─────────────────┘
                                                      │ reverse-proxy :8080
                                    ┌─────────────────▼─────────────────┐
                                    │  Go server (server/cmd/sklad)     │
                                    │  chi router · middleware · /api/v1 │
                                    │  embedded SPA (go:embed web/dist) │
                                    └─────────────────┬─────────────────┘
                                                      │ database/sql
                                    ┌─────────────────▼─────────────────┐
                                    │  SQLite (modernc.org/sqlite)      │
                                    │  WAL · migrations · /data volume  │
                                    └───────────────────────────────────┘
```

- **One static Go binary** serves both the REST API and the embedded React SPA,
  so the runtime footprint stays tiny (≈11 MB binary) for the NAS's ~1 GB RAM.
- **Caddy** owns TLS termination, the local certificate authority and the
  HTTP→HTTPS redirect. TLS is a hard requirement: browsers only expose the
  camera (`getUserMedia`) on a trusted secure context.
- **mDNS** (Avahi/Bonjour) advertises `_https._tcp` so the app is reachable as
  `sklad.local` without static DNS.

## 2. Repository & module map

```
server/
  cmd/sklad/            main(): config → logger → store → auth → scheduler → api
  cmd/import-json/      legacy equipment-log JSON → SQLite importer (CLI)
  internal/
    config/             env parsing + validation (docs/contracts/config.md)
    buildinfo/          version/commit/build-time (injected via -ldflags)
    store/              SQLite open, migrations, WithTx, backups & snapshots
    auth/               argon2id, sessions, CSRF, RBAC, login rate-limit
    audit/              audit_log writer (joins caller transactions)
    settings/           DB-backed runtime settings (Discord, scheduler, locale)
    notify/             Notifier interface + Discord webhook client
    jobs/               overdue scheduler (daily, idempotent, delivery log)
    api/                chi router, middleware, handlers (items/options/bulk/
                        export/settings/notifications/system), auth bridge
web/                    React + TypeScript + Tailwind (Vite)
  src/features/         auth, inventory, options, scan, bulk, labels, settings,
                        users, about
  src/lib/              api client, i18n, scanner, utils
  src/locales/          en.json (source of truth), cs.json, it.json
  dist/                 production build, embedded by the Go binary
deploy/                 Caddyfile, avahi service, cert docs
scripts/make-certs      local CA + leaf cert helper
docs/                   this file + contracts + QNAP/TLS/label runbooks
openapi/openapi.yaml    versioned REST contract (/api/v1)
```

The Go module is rooted at the repository root (`module sklad`); packages import
as `sklad/server/...`. `server/internal/` is internal by design — the importer
therefore lives under `server/cmd/import-json` (it needs `internal/store` for the
embedded migrations), **not** under `scripts/`.

## 3. Storage & concurrency

- **SQLite** via `modernc.org/sqlite` (pure Go, `CGO_ENABLED=0`), WAL journal,
  `foreign_keys=ON`, `busy_timeout=5000`, `_txlock=immediate`.
- **Append-only migrations** embedded in the binary (`store/migrations/*.sql`),
  tracked in `schema_migrations`; each migration commits atomically with its
  version row.
- **Optimistic concurrency** on `items.version`: mutating endpoints require the
  expected version and return `409 version_conflict` (with the current version)
  on a mismatch. Bulk updates are **all-or-nothing**: one transaction, per-item
  version check, one `bulk_update` audit row per changed item.
- **Audit**: every mutation writes `audit_log` (actor, action, entity,
  before/after JSON) inside the same transaction as the change it describes.
- **Backups**: `VACUUM INTO` database copies and JSON table snapshots, both with
  millisecond-resolution, collision-free names and retention pruning.

## 4. Identity, sessions & RBAC

- **Passwords** are argon2id PHC strings (`$argon2id$v=19$…`, t=3, m=64 MiB,
  p=1); verification is constant-time and a dummy hash is burned on unknown
  usernames to avoid enumeration.
- **Sessions** are server-side: the client holds a 256-bit bearer token in an
  `HttpOnly; Secure; SameSite=Lax` cookie; only the SHA-256 of the token is
  stored. `must_change_password` is enforced by `RequireAuth` (403
  `password_change_required` everywhere except `me`/`logout`/`change-password`).
- **CSRF** uses the synchronizer-token pattern: `X-CSRF-Token` must equal the
  session's stored token on every mutating verb.
- **Access**: anonymous visitors are read-only Viewers (inventory, scan lookup, labels,
  options, export). Only the configured bootstrap Admin and the single `editor`
  account can sign in. Editor mutates items; Admin manages options, settings,
  notifications and the Editor account. Historical user records remain stored,
  but other accounts cannot authenticate. Public read-only scan POST requires no CSRF.
- **Login rate-limiting** is a per-process fixed-window limiter keyed on client
  IP and username (10 failures / 15 min).

> Note: `SKLAD_COOKIE_SECRET` is accepted for forward compatibility but unused by
> the server-side session design (the bearer token needs no signing). See the
> configuration warning in `config.go`.

## 5. Barcode & labels

- **Decoding** runs client-side: native `BarcodeDetector` fast path with a
  `@zxing/library` fallback (iOS Safari), restricted to CODE128, plus an HID
  keyboard-wedge listener and manual entry.
- **Payload** is the immutable `asset_tag` (`PV-###`, verbatim from legacy data —
  the `PV-049` gap is preserved and new tags are `max+1`, never resequenced).
- **Scan → buffer → bulk**: scanned codes are resolved via `POST /items/resolve`
  (deduped, unknown codes returned for draft creation), buffered, then applied
  transactionally via `POST /items/bulk`.
- **Labels** are rendered client-side with `bwip-js` (code-split) at true
  physical scale for Brother P-Touch E300 tape (6/9/12/18 mm), downloadable as
  SVG or 300 DPI PNG. Narrow-bar width meets the contract minimum at 300 DPI.

## 6. Notifications & scheduling

- A `Notifier` interface has one implementation: the **Discord webhook** client
  (embeds, batching under Discord's limits, 429/5xx retry with backoff, no token
  leakage on permanent errors).
- The **scheduler** runs once per local day at `scheduler.check_hour`, with
  catch-up after restart. Idempotency is per-item-per-day, derived from
  successful `notification_deliveries` payloads; the delivery log records
  pending → sent/failed with a stable idempotency key.
- Email/SMTP has been removed entirely; Discord replaces it.

## 7. API conventions

- All routes are under `/api/v1`, JSON in/out; errors use the frozen envelope
  `{"error":{"code","message","details"}}`.
- List responses are `{"data":[…],"meta":{page,per_page,total,total_pages}}`;
  single resources are `{"data":{…}}`.
- Every request carries `X-Request-ID`; `GET /api/v1/health` reports `ok`/`degraded`
  from a SQLite ping; `GET /api/v1/version` serves build metadata.
- Option labels are localised (`?lang=cs|it`, fallback `en`), and all three
  labels are always present in the `labels` object.

### Contract addenda (deliberate, documented here)

1. **`label_printed_at` is read-only** in the frozen API: it is returned on
   `Item` but has no write path in `ItemPatch`/bulk (the label UI shows it
   read-only). A write path can be added as an additive contract change later.
2. **`POST /api/v1/notifications/run-now`** returns `409` (run in progress),
   `422` (`discord_not_configured` / `discord_rejected`), `502`
   (`discord_unreachable`) and `503` in addition to the contracted `200`/`403` —
   recorded in `openapi/openapi.yaml`.
3. **Test sends** (`POST /notifications/test`) are audited but not recorded in
   `notification_deliveries` (the schema's `test` trigger exists but is unused).
4. **Importer location** is `server/cmd/import-json` (not `scripts/import-json`)
   because it must import the internal store package.
5. **Audit visibility** is write-only in v1.0.0: `audit_log` rows are written on
   every mutation, but there is no `GET /api/v1/audit` read endpoint yet, so the
   `/audit` SPA route remains a placeholder. Adding the read endpoint is the
   natural post-v1.0.0 follow-up.

## 8. i18n

`en.json` is the source of truth; `cs.json` and `it.json` must have identical key
sets and placeholder parity, enforced by `web/scripts/check-i18n.mjs` (wired into
CI). Statuses and types are data, not UI strings: they carry per-locale labels
(`label_en/cs/it`) editable from the options manager.

## 9. Security posture

- No secrets in the image or git (`.env` + `data/` gitignored; gitleaks in CI).
- Hardened app container: read-only rootfs, `tmpfs /tmp`, `cap_drop: ALL`,
  `no-new-privileges`, non-root UID/GID, image-level healthcheck.
- Caddy applies CSP, `Permissions-Policy: camera=(self)`, HSTS, and serves the
  root CA at `/ca.crt` for device trust (see `docs/TLS-CAMERA.md`).
- Secrets are masked in API responses (Discord webhook shows only a preview +
  `webhook_set` boolean) and never logged.

## 10. Testing & CI

- **Go**: `go test -race ./...` — store, auth, items/options/bulk concurrency,
  notifier (mock Discord), scheduler determinism, importer lossless diff, and a
  black-box API harness over the real `API.Handler()` chain.
- **Frontend**: `tsc --noEmit`, ESLint, Vitest (scanner + pure helpers), i18n
  key-parity, Playwright smoke (fake camera) and a production `vite build`.
- **CI** (`.github/workflows/ci.yml`): go / web / e2e / secrets (gitleaks) /
  deploy (compose + Caddy validate + Avahi XML) / image (amd64 build + boot
  smoke test + multi-arch proof).
