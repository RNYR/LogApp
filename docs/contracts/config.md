# Sklad — Configuration Contract

> Status: **FROZEN CONTRACT** (task 1.a). This defines every environment variable, the runtime
> settings stored in the DB (`settings` table keys), and the network/port model. Task 1.b's
> `.env.example` and task 2.a's `server/internal/config` MUST follow this document.

## Environment variables

All optional unless marked **required**. Read at process start; changes require a restart.

| Variable                    | Default            | Description                                                        |
|-----------------------------|--------------------|--------------------------------------------------------------------|
| `SKLAD_HTTP_ADDR`           | `:8080`            | Internal listen address of the Go app (Caddy proxies to it)        |
| `SKLAD_DATA_DIR`            | `/data`            | Persisted volume root (SQLite, certs, backups)                     |
| `SKLAD_DB_PATH`             | `$DATA_DIR/sklad.db` | SQLite file path (override for dev)                              |
| `SKLAD_LOG_LEVEL`           | `info`             | `debug` \| `info` \| `warn` \| `error`                             |
| `SKLAD_LOG_FORMAT`          | `json`             | `json` \| `text`                                                    |
| `SKLAD_ADMIN_USERNAME`      | `admin`            | First-boot admin account (seed only)                               |
| `SKLAD_ADMIN_PASSWORD`      | *(required at boot)*| First-boot admin password; user is forced to change on first login |
| `SKLAD_ADMIN_EMAIL`         | `""`               | Seed admin email (optional)                                        |
| `SKLAD_COOKIE_SECRET`       | *(auto-generated)* | HMAC secret for cookie signing; set explicitly in prod             |
| `SKLAD_SESSION_TTL`         | `24h`              | Session lifetime (`time.ParseDuration`)                            |
| `SKLAD_CSRF_COOKIE_NAME`    | `sklad_csrf`       | Cookie name for the double-submit CSRF token                       |
| `SKLAD_SESSION_COOKIE_NAME` | `sklad_session`    | HttpOnly session cookie name                                       |
| `SKLAD_BASE_URL`            | `https://sklad.local` | Public origin, used to build links/labels                        |
| `SKLAD_TZ`                  | `Europe/Prague`    | IANA timezone for the scheduler & date rendering                   |
| `SKLAD_ALLOWED_ORIGINS`     | `""`               | Comma-separated CORS origins (empty = same-origin only)            |
| `SKLAD_TRUST_PROXY`         | `true`             | Honor `X-Forwarded-For`/`X-Real-IP` (Caddy is the proxy)           |

### Network / port model (Caddy edge)

| Component | Port (container) | Host port (configurable) | Notes                                  |
|-----------|------------------|--------------------------|----------------------------------------|
| Caddy     | 80 → 443         | 80 / 443 (or 8080 / 8443) | HTTP→HTTPS 301, TLS termination        |
| Go app    | 8080 (internal)  | not exposed              | Caddy reverse-proxies `/` → app:8080   |

- `sklad.local` + LAN IP SANs are issued by Caddy's **internal CA** (see task 5.a); a
  **BYO-certificate path** is supported for real domains/Let's Encrypt.
- The root CA must be downloadable for device trust (`/ca.crt`).
- **QNAP conflict warning**: QTS web admin commonly holds 80/443. If it does, map host
  `8080→80` and `8443→443` and point clients at `https://sklad.local:8443` (documented in
  `docs/DEPLOY-QNAP.md`). mDNS advertises the chosen port.

## Runtime settings (DB `settings` table)

These are editable in the admin UI (`GET/PUT /api/v1/settings`) and override defaults at runtime.

| key                        | type   | default | description                                        |
|----------------------------|--------|---------|----------------------------------------------------|
| `discord.webhook_url`      | string | `""`    | Discord webhook URL (secret; masked in API responses) |
| `discord.enabled`          | bool   | `false` | Master switch for Discord alerts                   |
| `scheduler.check_hour`     | int    | `11`    | Local hour 0–23 for the daily overdue run          |
| `scheduler.days_ahead`     | int    | `0`     | Report items due within N days (0 = overdue only)  |
| `locale.default`           | string | `en`    | `en` \| `cs` \| `it`                               |
| `general.organization_name`| string | `""`    | Shown on labels/reports                            |

## Secrets policy

- `SKLAD_ADMIN_PASSWORD` and `discord.webhook_url` are **secrets**: never logged, never baked into
  images, never echoed unmasked by the API. The settings API returns `discord.webhook_url` as a
  masked preview (e.g. `https://discord.com/api/webhooks/…/****`) plus a `webhook_set: bool`.
- `SKLAD_COOKIE_SECRET` is auto-generated into the data dir on first boot when unset; if set, it
  is read verbatim and not persisted to disk.

## Operational additions (2026-10-18)

`SKLAD_BACKUP_INTERVAL`: duration, default `24h`, minimum `1m`; used by the server for periodic SQLite backups after the startup backup. Failed attempts retry after one minute. Retention is 50 successful database copies; JSON snapshots are not automatically scheduled. `SKLAD_COOKIE_SECRET` is currently reserved; random server-side sessions do not use cookie signing or create a signing-key file. These statements supersede earlier backup/secret descriptions above.
