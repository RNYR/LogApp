> Follow-up: overdue queries, scheduled SQLite backups, offline Admin recovery and staging documentation have since been implemented. Go builder and CI have also been updated. Findings below record the original audit; target-NAS acceptance, off-NAS backup scheduling and restore validation remain required.

# NAS readiness audit — 2026-10-18

Verdict: ready for a staging installation, not yet signed off for live inventory.
This was a source/configuration review and frontend regression run, not a NAS test.
No application behavior or deployment was changed during the audit.

## Verification

- Production frontend typecheck and build: passed.
- Frontend unit tests: 46 passed.
- Desktop/mobile browser regression tests: 64 passed (mock API).
- Locale parity: 284 keys in en/cs/it, passed.
- Backend and six real-backend browser integration tests passed in the preceding implementation work, but were not rerun today: Go is no longer installed at the earlier temporary location and is not on PATH.
- Docker is not available here. The actual image build, Caddy startup, volumes, NAS restart and restore were not verified. Existing CI definitions are not evidence of a current successful run.
- Sites preview remains browser-local and is not the production database or authentication system.

## Must resolve before live use

### 1. Returned equipment can still be reported overdue

`server/internal/api/items.go` itemFilter.where and `server/internal/jobs/scheduler.go` dueItems filter on return_date without excluding Feasibility. Admin return confirmation intentionally preserves old rental fields. Therefore a device returned to Feasibility with a past return date still qualifies for the overdue filter and Discord reports. Exclude Feasibility consistently and add regression coverage for a complete loan → return → overdue-check cycle.

### 2. Automatic backups are not wired up

`store.BackupDB` and `store.Snapshot` exist, but the production server does not invoke them. The importer optionally invokes BackupDB; no recurring backup job exists. The deployment guide's claim that copies/snapshots are written automatically is incorrect.

Schedule transactionally consistent backups, retain copies outside the application volume, include Caddy's CA data, and prove restoration. The guide currently suggests tar-copying an active SQLite volume; that is not a guaranteed consistent backup. Use SQLite backup/VACUUM INTO or stop the writer for the copy. See [SQLite backup guidance](https://sqlite.org/backup.html) and [copying during transactions](https://www.sqlite.org/howtocorrupt.html).

### 3. Production build and dependency review

Dockerfile pins the Go builder to 1.23, which is outside the supported Go release window. Update to a supported toolchain and scan Go/npm dependencies and the resulting image before releasing. Go supports a major release only until two newer major releases exist: [official policy and release history](https://go.dev/doc/devel/release).

Build the real linux/amd64 image if the target remains TS-251, boot it with fresh and migrated data, and test through Caddy over HTTPS. The real-backend rental suite is not currently invoked by CI; add it to release checks. Confirm actual NAS model/architecture, QTS/Container Station versions, RAM, storage and available ports before selecting the artifact.

### 4. Initial data and options need an explicit setup path

A fresh production database seeds Admin, Customer and Feasibility only. It does not seed the preview's equipment types or remaining statuses. With only Feasibility, public users have no rental status to choose. Import verified existing options/inventory or populate the approved defaults before handover. Add personnel and reconcile legacy non-Feasibility items with missing renters. Preserve asset IDs and test duplicate-free re-import.

The legacy JSON importer exists as a separate Go command but is not included in the runtime image. Build/package an operator tool or import into a staging database on a workstation; document and rehearse that path. Do not treat browser preview data as already migrated.

### 5. Admin recovery and operator instructions

There is no supported offline Admin reset command. Changing the bootstrap environment password does not reset an existing account. The current guide mentions deleting the database as a last resort, which loses inventory and is not an acceptable recovery plan. Provide a local operator-only reset procedure that preserves data and revokes sessions.

Update stale runbook/README statements about login-first landing, Viewer/Editor accounts, label generation, automatic backups, cookie-secret behavior and recovery. Current sessions use server-side random tokens; the CookieSecret setting is reserved and does not currently implement the signing described in the old docs.

## NAS acceptance checklist

- Correct model/architecture, ports, host/IP and base URL; persistent volume ownership.
- Trusted HTTPS on the actual phones/laptops; DNS resolves; camera works without certificate warnings.
- LAN/VPN-only reachability appropriate for anonymous rental writes; no unintended Internet port forwarding.
- Admin first-login password change; public rental-only access and rejected direct administrative API requests.
- Two clients updating the same item produce a clear conflict, not lost data.
- Device restart/NAS reboot preserves items, personnel, settings and access; services restart healthy.
- PT-E300 CODE128 labels for representative asset IDs resolve with actual cameras and any keyboard scanner.
- Optional Discord webhook: test delivery, correct timezone, real overdue report and returned-device exclusion after the fix.
- Scheduled backup and restore into a separate instance; restored personnel references, records and Admin access verified.

## Useful follow-ups, not essential deployment blockers

- Audit records are written, but there is no usable audit-history UI/read API (`/audit` is a placeholder).
- Public edits cannot identify the actual operator; selected renter is not proof of who submitted the change. This follows the approved no-login model.
- Archive restoration has no dedicated UI.
- Review whether starting a new rental should explicitly reset/confirm carried-over renter, note and dates. Current behavior preserves them unless changed; this meets the existing nonblank validation but can reuse stale details.
- Customer note validation ensures nonblank text; it cannot verify that the entered text is a real customer name.

Recommended order: fix overdue semantics → backup/recovery tooling → production build/dependency and CI checks → staged import/setup → NAS acceptance and restore drill → live handover.
