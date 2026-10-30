# Sklad NAS staging and deployment

Updated 2026-10-18 for anonymous Editor rentals, Admin-only administration and
manual Brother PT-E300 labels. The Sites URL is a browser-local preview; deploy
the normal Go/React build on the NAS for shared persistent data.

## 1. Prepare the staging environment

Confirm the actual NAS model/CPU, QTS and Container Station versions, available
memory, disk and ports. The original target TS-251 is linux/amd64. Build on a
workstation if the NAS has limited memory. Docker/Compose must be installed.

Choose a stable LAN IP and hostname, free HTTP/HTTPS ports, and a persistent data
location. Keep access on the intended office LAN/VPN: anyone who can reach the
app can perform the permitted rental updates without login.

```sh
cd /share/Container/sklad
cp .env.example .env
chmod 600 .env
```

Edit `.env`:

- `SKLAD_ADMIN_USERNAME=admin` and a strong, temporary `SKLAD_ADMIN_PASSWORD`.
- `SKLAD_LAN_IP` = actual NAS IP.
- `SKLAD_HOST`/`SKLAD_HOST_ALT` = chosen resolvable names.
- For QTS port conflicts, for example `SKLAD_HTTP_PORT=8180`,
  `SKLAD_HTTPS_PORT=8443`, `SKLAD_PUBLIC_PORT=:8443`.
- `SKLAD_TZ=Europe/Prague` (or the office timezone).
- `SKLAD_BACKUP_INTERVAL=24h` (default; minimum 1m).

Compose derives `SKLAD_BASE_URL` from hostname and public HTTPS port. Resolve the
name via the office DNS/mDNS setup, or use the configured IP. Verify resolution
on actual devices; do not assume every phone resolves `.local`.

`SKLAD_COOKIE_SECRET` is a reserved compatibility setting. Current authentication
uses random server-side sessions, not cookies signed with this setting.

Only Caddy exposes host ports; the Go server stays on the Docker network. Default
volumes are `sklad_data`, `caddy_data` and `caddy_config` with Compose project
prefixes. For a bind-mounted data directory, give the app user write permission
(default UID/GID 10001; configurable through SKLAD_UID/SKLAD_GID at build time).

## 2. Build the production image

```sh
# On a workstation, from the project root. Never build with --mode preview.
# Tag the release; set SKLAD_IMAGE_TAG to the same value in the NAS .env so the
# previous image stays loaded for a rollback.
docker build --platform linux/amd64 -t sklad:2.0.1 .
docker save sklad:2.0.1 | gzip > sklad-image.tar.gz
# Copy image archive and deployment sources to the NAS using your normal transfer method.
```

On the NAS:

```sh
gzip -dc /share/Container/sklad-image.tar.gz | docker load
cd /share/Container/sklad
docker compose up -d --no-build
docker compose ps
docker compose logs --tail=100 app caddy
```

Alternatively build on the NAS with `docker compose build app`. The image uses a
supported Go builder and includes the SPA, server and legacy JSON importer.
Do not copy local node_modules, preview data or development databases into production.

## 3. First start and initial data

Startup migrates the database, seeds the configured Admin if missing and starts
scheduled backups and overdue notifications. The public landing page is Inventory
in Editor mode. Admin signs in using the bootstrap credentials and must change
the password. Changing `.env` afterward does not reset an existing account.

Fresh databases seed Customer and Feasibility only. Before handover:

1. Configure equipment types and other rental statuses in Manage options, or
   import the existing inventory/options. Keep the exact Feasibility name.
2. Add office personnel; Customer remains a permanent choice.
3. Reconcile existing rentals that have no personnel reference. Their next
   non-Feasibility update requires one. Do not mark items returned just to satisfy validation.
4. Verify original asset IDs, item counts and representative status/date/note values.

For legacy JSON, stop the server, mount a copy of the legacy input directory,
and use `import-json --help` for its arguments. Rehearse a dry run on staging:

```sh
docker compose stop app
docker compose run --rm --no-deps --entrypoint import-json \
  -v /share/Container/legacy-copy:/legacy:ro app --source /legacy --dry-run
# After reviewing counts and verifying a pre-import backup:
docker compose run --rm --no-deps --entrypoint import-json \
  -v /share/Container/legacy-copy:/legacy:ro app --source /legacy --pre-snapshot
docker compose start app
```

Back up before any import or migration. Importer --pre-snapshot occurs after
opening/migrating the database; it is not a substitute for a pre-upgrade copy.

## 4. HTTPS, cameras and labels

Follow [TLS-CAMERA.md](TLS-CAMERA.md) to install and trust Caddy's root CA on the
actual devices. Download at `/ca.crt` on the selected HTTPS origin. Verify the
certificate fingerprint through a trusted administrator before installing it.
A certificate-warning bypass is not a camera setup solution.

Confirm both containers become healthy and the app responds over trusted HTTPS.
Caddy’s container health check requests `/api/v1/health` through its internal
HTTPS listener, so it works with internal or custom certificates without requiring
a downloadable `/ca.crt`. This probe skips certificate trust verification; the
trusted-client check below is still required.
Use `curl --cacert sklad-root-ca.crt https://HOST:PORT/api/v1/health` where needed.
Caddy's CA resides in its data volume; preserve that volume across upgrades.

PT-E300 labels are made with the printer keyboard, not uploaded. Follow
[LABELS.md](LABELS.md): CODE128 with the exact asset ID, then scan before attachment.
Test phone cameras, USB/HID scanner if used, and long/short representative IDs.

## 5. Automatic and off-NAS backups

The server creates a consistent SQLite copy using VACUUM INTO at startup and
then every `SKLAD_BACKUP_INTERVAL` after a successful attempt. A failure is logged
and retried after one minute. Each attempt has a five-minute timeout. Shutdown
cancels and waits for the backup worker. The newest 50 successful `.db` copies
are retained in `/data/backups`; files are mode 0600. In-progress files use
`.pending`; only successfully completed files are atomically renamed to `.db`.
Copy only `.db` snapshots; a hard crash may leave an unused `.pending` file. Automatic JSON snapshots
are not produced. SQLite backups contain all tables, including personnel,
accounts, settings and audit records; protect them as sensitive data.

Check logs for `database backup completed` / `database backup failed`. Local
copies protect against application mistakes, not NAS/disk failure. Schedule a
separate NAS backup job to copy completed snapshots to another device/storage
location and alert on stale/missing backups.

To create and copy a consistent on-demand backup while the app is running:

```sh
backup_dir=/share/Backup/sklad
mkdir -p "$backup_dir"
chmod 700 "$backup_dir"
backup_path=$(docker compose exec -T app /usr/local/bin/sklad backup) &&
  docker cp "sklad-app:$backup_path" "$backup_dir/"
```

Check the command exit status. Use the returned completed snapshot; do not copy
the live `sklad.db`, WAL and SHM independently while writes continue.
Also back up Caddy's CA data (including its private key), securely and separately.
For a complete cold snapshot before an upgrade, stop both services, archive their
actual named volumes/bind mounts with NAS backup tooling, verify completion, and
restart. Determine actual names with `docker inspect sklad-app sklad-caddy`.
Do not use the old runbook's live-volume tar example as a consistent DB backup.

### Restore drill

First restore to an isolated staging volume/instance, not the live service:

1. Stop the target app. Preserve its current full data directory for rollback.
2. Copy a chosen completed `.db` backup to `/data/sklad.db`. Remove only the
   target database's stale `sklad.db-wal` and `sklad.db-shm` while the app is stopped.
3. Set ownership to the configured app UID/GID (10001:10001 by default).
4. Use a matching executable/image; start the target app. A newer image may migrate
   the restored database. Do not downgrade a migrated DB with an older image.
5. Verify health, Admin access, counts, personnel references, options, a rental
   update and persistence after restart. Isolate outbound notifications during the drill.
6. Record the backup date, restored version and results. Test restoration of the
   Caddy volume too when rehearsing full NAS replacement.

SQLite session records are also restored. For a security recovery, use the Admin
reset procedure below to invalidate restored Admin sessions before reopening access.

## 6. Recover a forgotten Admin password without losing inventory

This is a local operator command, not a public reset endpoint. It requires shell
access to the deployment/data volume. Stop the server first to prevent in-flight
requests from racing the reset. It targets only `SKLAD_ADMIN_USERNAME` with role
Admin; it will not create a missing account or elevate an Editor.

In a Bash shell on the NAS (password entry is hidden and not placed in arguments):

```bash
docker compose stop app
read -r -s -p 'Temporary Admin password (12–1024 bytes): ' recovery_password
printf '\n'
printf '%s\n' "$recovery_password" | docker compose run --rm --no-deps -T app reset-admin --password-stdin
recovery_result=${PIPESTATUS[1]}
unset recovery_password
if [ "$recovery_result" -eq 0 ]; then
  docker compose start app
else
  echo 'Recovery failed; inspect the error before restarting.'
fi
```

The command requires an existing DB, creates a backup before changing credentials,
updates only the configured Admin, reactivates it, revokes its sessions and records
an audit event without the password/hash. Backup failure aborts recovery. Sign in
with the temporary password and complete the required password change. Keep the
pre-recovery snapshot private: it contains the old credentials/session state.
Never delete the database to reset a password.

## 7. Optional Discord configuration

Admin → Settings: configure the webhook, enable automatic alerts, choose local
check hour and days ahead. A test message or manual overdue run is an explicit
operator action and can work while automatic alerts are disabled. Verify delivery,
timezone and duplicate suppression. Feasibility items must never be included,
even if their preserved return date is past. Test the full loan → physical
return confirmation → overdue check sequence.

## 8. Staging exit checklist

- [ ] Production image built for the confirmed NAS architecture; both services healthy.
- [ ] Types/statuses, inventory and personnel loaded and reviewed.
- [ ] Admin bootstrap/password change and offline recovery tested on staging.
- [ ] Anonymous rental updates work; identity/options/personnel changes are rejected.
- [ ] Individual/bulk Customer notes and required renters enforced.
- [ ] Only Admin returns items to Feasibility; returned devices are excluded from overdue reports.
- [ ] Concurrent edits produce a conflict rather than lost updates.
- [ ] Actual cameras/scanner and PT-E300 labels tested over trusted HTTPS.
- [ ] Backups occur automatically; failure/staleness is monitored by the operator.
- [ ] Off-NAS copy and isolated restore drill completed, including personnel references.
- [ ] NAS reboot preserves data/certificates and restarts the services.
- [ ] Optional Discord test/report checked with approved recipients.

Local automated checks do not replace these target-device tests. Keep the previous
image and verified pre-upgrade data/CA backup until staging acceptance is complete.
