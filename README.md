> Current access model: anonymous Editor for rentals; authenticated Admin for administration. See [role permissions](docs/roles-and-functions.md) and [rental rollout and tests](docs/RENTAL-WORKFLOW.md). Earlier Viewer/Editor account descriptions are superseded.

# Sklad

> **Equipment Inventory & Lending** — a self-hosted web app for tracking company
> equipment: what you own, where it is, who has it, and when it is due back.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.23-00ADD8.svg)](https://go.dev)
[![TypeScript](https://img.shields.io/badge/TypeScript-React-3178C6.svg)](https://www.typescriptlang.org/)

**Created by David Žitník.** Open source under the MIT License — see
[LICENSE](LICENSE) and [NOTICE](NOTICE).

---

## Overview

Sklad is a ground-up, production-minded rewrite of an internal equipment-log
tool. It is designed to be small enough for a five-person workshop yet solid
enough for a larger organisation: real authentication and roles, an audit trail,
optimistic concurrency instead of file locks, a normalised SQLite database, and
a modern single-page frontend.

It is built to run on a **QNAP TS-251** (x86-64) via Docker/Container Station and
to be reachable on the LAN at **https://sklad.local**, with HTTPS and camera
access working on laptops, Android, and iOS.

### Highlights

- 🔐 **HTTPS by default** — automatic local CA + certificate for `sklad.local`,
  so browser camera APIs (which require a secure context) work everywhere.
- 📷 **Built-in CODE128 scanner** — scan labels with a laptop/phone camera
  (`BarcodeDetector` with a ZXing fallback for iOS Safari), plus HID
  keyboard-wedge and manual entry.
- 🏷️ **PT-E300 label preparation** — exact asset IDs and manual CODE128 keypad instructions, followed by scan verification.
- 📦 **Scan → buffer → bulk actions** — scan many items into a buffer and apply
  status/date/note changes in one transaction.
- 🤖 **Discord notifications** — overdue-item reports are posted to a Discord
  webhook instead of email, with a delivery log and no duplicate sends.
- 🌍 **Trilingual UI** — Czech, English, and Italian, switchable at runtime.
- 👥 **Real auth & RBAC** — Argon2id password hashing, session cookies, CSRF
  protection, anonymous Editor rental access, authenticated Admin administration, and an audit log.
- 🗄️ **SQLite storage** — pure-Go `modernc.org/sqlite` (no CGO), WAL mode,
  append-only migrations, and scheduled consistent `VACUUM INTO` database backups.
- 🧭 **mDNS** — discoverable as `sklad.local` on the local network.

> **Version 2.0.1.** The architecture, API, schema and i18n contracts are frozen
> in [docs/contracts](docs/contracts) and [openapi/openapi.yaml](openapi/openapi.yaml);
> the implementation is complete and covered by Go + frontend test suites and CI
> (see [.github/workflows/ci.yml](.github/workflows/ci.yml)).

---

## Architecture at a glance

| Layer      | Technology                                                                 |
|------------|----------------------------------------------------------------------------|
| Frontend   | React + TypeScript + Tailwind CSS (Vite), i18next (en/cs/it)               |
| Backend    | Go 1.23, `net/http` + `chi`, embedded SPA (`go:embed`)                     |
| Storage    | SQLite via `modernc.org/sqlite` (CGO-free), WAL, embedded migrations      |
| Edge / TLS | Caddy with an internal CA for `sklad.local` (HTTP→HTTPS redirect)          |
| Discovery  | Avahi/Bonjour mDNS (`_https._tcp`)                                         |
| Packaging  | Multi-stage Dockerfile, `docker-compose.yml` (app + Caddy), non-root       |

---

## Quick start (Docker)

> Requires Docker and Docker Compose. On the QNAP, install **Container Station**
> and use the SSH/compose workflow described in
> [docs/DEPLOY-QNAP.md](docs/DEPLOY-QNAP.md).

```bash
# 1. Get the code
git clone https://github.com/RNYR/LogApp.git sklad && cd sklad

# 2. Configure
cp .env.example .env
#    Edit .env and set at least SKLAD_ADMIN_PASSWORD (and the correct
#    NAS hostname/IP and ports).

# 3. Build and start
docker compose up -d --build

# 4. Open the app
#    https://sklad.local           (after trusting the Sklad local CA)
#    https://<nas-lan-ip>:8443     (if QTS owns ports 80/443)
```

On first start the app seeds the admin account from `SKLAD_ADMIN_USERNAME` /
`SKLAD_ADMIN_PASSWORD`. You are **required to change that password on first
login**.

### Trusting the local CA (required for camera access)

Browsers only expose the camera on a *trusted* HTTPS origin. On first boot Caddy
issues a certificate from Sklad's local root CA; download it at
`https://sklad.local/ca.crt` and install it on each device (iOS profile, Android
CA install, macOS Keychain, Windows certificate store). Full per-OS instructions
are in [docs/TLS-CAMERA.md](docs/TLS-CAMERA.md).

---

## `sklad.local` (mDNS)

An Avahi/Bonjour service record advertises the app as `sklad.local` on
`_https._tcp`. If the QNAP already provides mDNS, or if ports 80/443 are taken
by QTS, the advertised port and host ports are configurable — see the port table
in [docs/contracts/config.md](docs/contracts/config.md) and the QNAP runbook.

---

## Configuration

All environment variables and their defaults are defined in the frozen contract
[docs/contracts/config.md](docs/contracts/config.md) and mirrored in
[.env.example](.env.example). Runtime-editable settings (Discord webhook, daily
check hour, default locale) live in the database and are managed from the admin
UI.

**Secrets policy:** `SKLAD_ADMIN_PASSWORD` and the Discord webhook URL are
secrets — never logged, never baked into images, never returned unmasked by the
API. `.env` and the `data/` volume are gitignored.

---

## Project layout

```
sklad/
├── server/                 # Go backend (module root is the repo root)
│   ├── cmd/sklad/          # server main package
│   ├── cmd/import-json/    # legacy JSON → SQLite importer
│   └── internal/           # config, api, store, auth, notify, jobs, …
├── web/                    # React + TypeScript SPA (Vite)
│   ├── src/locales/        # en.json (source of truth), cs.json, it.json
│   └── dist/               # built assets, embedded into the Go binary
├── openapi/openapi.yaml    # versioned REST API contract (/api/v1)
├── docs/                   # contracts + deployment/TLS/label runbooks
│   └── contracts/          # schema.md, barcode.md, config.md (frozen)
├── deploy/                 # Caddyfile, avahi service, cert docs
├── scripts/                # cert helpers (make-certs)
├── equipment-log/          # legacy reference implementation (nested module)
├── Dockerfile
├── docker-compose.yml
├── go.mod                  # module sklad
└── .env.example
```

> Directories marked above that do not yet exist are created by their respective
> build tasks; the contracts they must implement are already frozen.

---

## Development

**Prerequisites:** Go **1.23+** (backend) and Node **20+** (frontend). Docker is
optional for local work but required for the full TLS/mDNS stack.

```bash
# Backend
go build ./server/...
go test -race ./server/...

# Frontend
cd web
npm install
npm run dev          # dev server (proxies /api to the Go backend)
npm run build        # production build -> web/dist (embedded by Go)
npm run lint
npm run typecheck
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for conventions, testing expectations,
and the i18n key-parity rule.

---

## Migrating from the legacy `equipment-log` app

The legacy JSON data is imported into SQLite by an idempotent importer
(`server/cmd/import-json`, with `--dry-run` and `--pre-snapshot`), preserving
every item ID verbatim — including the intentional `PV-049` gap — along with
types, statuses, dates, and notes. Legacy SMTP settings are intentionally
ignored (Discord replaces email).

```bash
go run ./server/cmd/import-json \
  -db /path/to/data/sklad.db \
  -data-dir /path/to/data \
  -source equipment-log/app/data
```

---

## Documentation

**Runbooks**

- [docs/DEPLOY-QNAP.md](docs/DEPLOY-QNAP.md) — install on a QNAP NAS (Container
  Station, SSH compose, port conflicts, backups, upgrades)
- [docs/TLS-CAMERA.md](docs/TLS-CAMERA.md) — root CA trust per platform and
  camera troubleshooting (HTTPS is required for scanning)
- [docs/LABELS.md](docs/LABELS.md) — CODE128 labels and the Brother P-Touch E300
  workflow

**Contracts**

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — design decisions and module map
- [docs/contracts/schema.md](docs/contracts/schema.md) — SQLite schema v1
- [docs/contracts/barcode.md](docs/contracts/barcode.md) — CODE128 / label rules
- [docs/contracts/config.md](docs/contracts/config.md) — env + settings keys
- [openapi/openapi.yaml](openapi/openapi.yaml) — REST API contract (`/api/v1`)
- [deploy/certs/README.md](deploy/certs/README.md) — certificate modes and
  `scripts/make-certs`

---

## Contributing

Issues and pull requests are welcome. Please read
[CONTRIBUTING.md](CONTRIBUTING.md) first.

## Security

Please report vulnerabilities privately — see [SECURITY.md](SECURITY.md).

## License

MIT © 2026 David Žitník. See [LICENSE](LICENSE). Third-party attributions are in
[NOTICE](NOTICE).

## Operations

Automatic SQLite backups run at startup and every 24h by default (`SKLAD_BACKUP_INTERVAL`), retaining 50 copies. `sklad backup` makes an on-demand copy. Offline `sklad reset-admin --password-stdin` recovers Admin access without deleting inventory. Follow [the NAS staging guide](docs/DEPLOY-QNAP.md) for safe off-NAS backups, restoration and recovery.
