# Sklad on a QNAP NAS — install, cut-over, upgrade, rollback

The operator runbook for the **live** stack (`https://sklad.local`). The test
stack that runs beside it (`https://sklad-test.local:8444`) has its own guide,
[DEPLOY-TEST.md](DEPLOY-TEST.md). Every release goes to the test stack first.

Images are built **on the NAS** from a source bundle, so the workstation needs
only `git`, `ssh`, `tar` and `rsync` — no Docker. Four scripts do the work:

| Script | Runs on | Does |
|---|---|---|
| `scripts/make-bundle` | Mac | writes `./sklad-release` (sources + deployment files + `RELEASE` tag) |
| `scripts/push-to-nas` | Mac | uploads the bundle and runs `nas-deploy` on the NAS |
| `scripts/nas-deploy` | NAS | backup → build `sklad:<tag>` → swap folders → start → wait for healthy |
| `scripts/sklad-stack` | NAS | everyday operation: `preflight`, `up`, `ps`, `logs`, `backup`, `restore-db`, `restore-ca`, `rollback`, … |

Layout on the NAS:

| Path / name | What |
|---|---|
| `/share/Container/sklad` | live release folder (`.env` lives here, mode 600) |
| `/share/Container/sklad.previous` | the release before the last deployment (for `rollback`) |
| `/share/Container/.sklad-deploy-history` | one line per deploy/rollback: tag, commit, backup folder |
| `/share/Backup/sklad/<time>/` | backups made by `sklad-stack backup` (database + CA) |
| image `sklad:<tag>` | one image per release; the previous one stays for rollback |
| volumes `sklad_sklad_data`, `sklad_caddy_data`, `sklad_caddy_config` | database + in-volume backups, Caddy CA. Never deleted by the scripts |
| containers `sklad-app`, `sklad-caddy` | the running stack |

Never run plain `docker compose up --build` in the live folder: it would build
over the current image tag and lose the rollback point. Use the scripts.

## 1. Prepare

Confirm the NAS architecture (`uname -m` → `x86_64` = linux/amd64), memory, free
disk (a build needs ~2 GB temporarily) and which ports are taken. On QNAP,
`docker compose` is not on the SSH `PATH`; in every NAS shell:

```sh
export PATH=/share/Container/bin:$PATH
```

(The scripts set this themselves.) Choose a stable LAN IP and host name. Keep
access on the office LAN/VPN: anyone who can reach the app can make the
permitted rental updates without login.

## 2. First install

From the Mac, on a clean, committed checkout of the release (tag it first, e.g.
`v2.0.2`; `make-bundle` refuses uncommitted changes):

```sh
./scripts/make-bundle                         # writes ./sklad-release, tag from git describe
./scripts/push-to-nas David_Zitnik@<nas-ip>   # uploads to /share/Container/sklad
```

With no `.env` on the NAS yet, the push only copies the files and prints the next
steps. On the NAS:

```sh
cd /share/Container/sklad
cp .env.example .env && chmod 600 .env
vi .env
```

Set at least:

- `SKLAD_ADMIN_PASSWORD` — temporary, **12+ characters**; the server refuses to
  seed the Admin with anything shorter or with an example value. You change it
  on first login. Changing `.env` later never resets an existing account.
- `SKLAD_LAN_IP` — the NAS address (certificate SAN and `default_sni`).
- `SKLAD_HOST` / `SKLAD_HOST_ALT` — the names devices will use (default
  `sklad.local` / `sklad`).
- `SKLAD_IMAGE_TAG` — the tag the push printed (e.g. `2.0.2`).
- `SKLAD_HSTS_MAX_AGE=0` until every device trusts the CA (the default in
  `.env.example`).
- Ports: `80`/`443` by default. If QTS itself owns 443, use for example
  `SKLAD_HTTP_PORT=8180`, `SKLAD_HTTPS_PORT=8443`, `SKLAD_PUBLIC_PORT=:8443`, and
  set `<port>8443</port>` in `deploy/avahi/sklad.service`.
- `SKLAD_TZ=Europe/Prague`, `SKLAD_BACKUP_INTERVAL=24h` (defaults).

`SKLAD_COOKIE_SECRET` is reserved and unused (sessions are server-side).

Build the image once (10–30 min; the legacy app keeps running meanwhile) and
check the setup:

```sh
SKLAD_VERSION=<tag> docker compose build app    # tags sklad:$SKLAD_IMAGE_TAG from .env
./scripts/sklad-stack preflight
```

`preflight` checks the password length, LAN IP, that `sklad:<tag>` exists, and
who owns the HTTP/HTTPS ports. While `equipment-log` still runs it reports port
80 as taken — expected until the cut-over (§4). Do **not** start the live stack
before the cut-over unless it uses ports the legacy app does not hold.

The volume names are pinned in `docker-compose.yml` (project `sklad`), so the
folder can be renamed or moved without losing data or the CA. For a bind-mounted
data directory instead of the named volume, give the app user write permission
(UID/GID 10001 by default; `SKLAD_UID`/`SKLAD_GID` at build time).

## 3. Initial data

Startup migrates the database, seeds the Admin if missing, and starts scheduled
backups and overdue notifications. The landing page is Inventory in anonymous
Editor mode; Admin signs in with the bootstrap credentials and must change the
password (which also signs out any other Admin session).

Fresh databases seed only the Customer and Feasibility statuses. Before handover:

1. Configure equipment types and the other rental statuses in Manage options, or
   import them with the inventory. Keep the exact Feasibility name.
2. Add office personnel; Customer remains a permanent choice.
3. Reconcile existing rentals that have no personnel reference. Their next
   non-Feasibility update requires one. Do not mark items returned just to
   satisfy validation.
4. Verify asset IDs, item counts and representative status/date/note values.

**Legacy import.** `import-json` reads `inventory.json` and `options.json` and is
idempotent: rows that already exist (same asset tag / option value) are
skipped, never updated. So rehearse on the **test stack**, and import into the
live database exactly once, during the cut-over (§4), from a final copy of the
legacy data. Rehearsal on the NAS:

```sh
legacy=$(docker inspect equipment-log -f '{{range .Mounts}}{{.Source}}{{end}}')
mkdir -p /share/Container/legacy-copy && cp -a "$legacy"/. /share/Container/legacy-copy/
cd /share/Container/sklad-test
./scripts/test-stack compose stop test-app
./scripts/test-stack compose run --rm --no-deps --entrypoint import-json \
  -v /share/Container/legacy-copy:/legacy:ro test-app --source /legacy --dry-run
./scripts/test-stack compose run --rm --no-deps --entrypoint import-json \
  -v /share/Container/legacy-copy:/legacy:ro test-app --source /legacy
./scripts/test-stack compose start test-app
```

Check the counts it prints (81 items expected, IDs `PV-###`, `PV-049` missing in
the source), then the items, types and statuses in the UI. `--pre-snapshot`
(on by default) backs up the target database after opening it; it does not
replace a backup taken before the import.

## 4. Cut-over from `equipment-log`

Today the legacy container `equipment-log` publishes ports **80** and **8787**
and is reached as `http://sklad.local` / `http://sklad.local:8787`. After the
cut-over Sklad answers `https://sklad.local` (port 80 redirects to HTTPS) and
**port 8787 stops answering** — tell users the new address.

Before the window:

- [ ] The release passed the test stack (§11), including the import rehearsal.
- [ ] Files and image are on the NAS (§2), `.env` filled in, `preflight` shows
      only the port-80 conflict.
- [ ] Find how `sklad.local` resolves today, so you can repoint it:
      `ls /etc/avahi/services/` (the legacy `logapp.service` advertises
      `_http._tcp` on port 80), `cat /etc/avahi/hosts`, the router DNS, or the
      NAS's own host name. QTS rewrites parts of `/etc` at boot, so whatever
      you change must survive a reboot (test it).
- [ ] Announce a short freeze: no rental changes in the old app during the window.

In the window, on the NAS:

```sh
export PATH=/share/Container/bin:$PATH
# 1. Stop the legacy app; keep the container and its data for rollback.
docker stop equipment-log
docker update --restart=no equipment-log        # stays stopped after a reboot

# 2. Final copy of the legacy data, then import into the new, empty live DB.
legacy=$(docker inspect equipment-log -f '{{range .Mounts}}{{.Source}}{{end}}')
rm -rf /share/Container/legacy-final && mkdir -p /share/Container/legacy-final
cp -a "$legacy"/. /share/Container/legacy-final/
cd /share/Container/sklad
./scripts/sklad-stack compose run --rm --no-deps --entrypoint import-json \
  -v /share/Container/legacy-final:/legacy:ro app --source /legacy --dry-run
./scripts/sklad-stack compose run --rm --no-deps --entrypoint import-json \
  -v /share/Container/legacy-final:/legacy:ro app --source /legacy

# 3. Start Sklad and wait until both containers are healthy.
./scripts/sklad-stack preflight
./scripts/sklad-stack up
./scripts/sklad-stack backup                     # first backup out of the volume
```

Then:

4. **Name resolution:** replace the legacy mDNS advertisement with
   `deploy/avahi/sklad.service` (HTTPS, port 443 or your `SKLAD_HTTPS_PORT`)
   wherever `logapp.service` lived, and keep `sklad.local` pointing at the NAS
   (Avahi host name, `/etc/avahi/hosts`, or router DNS). Android resolves
   `.local` unreliably — a router DNS entry or the IP address
   (`https://<nas-ip>`) always works because the certificate covers the LAN IP.
5. **Trust the CA** on each device ([TLS-CAMERA.md](TLS-CAMERA.md)); the camera
   needs it.
6. **Verify:** Admin login + forced password change, item count, a rental
   update from a phone, a label scan, and that `http://sklad.local` redirects to
   HTTPS. Then reboot the NAS once and check that both containers come back
   healthy and the name still resolves.

**If the cut-over has to be undone** (before users rely on Sklad):

```sh
cd /share/Container/sklad && ./scripts/sklad-stack down     # volumes are kept
docker update --restart=unless-stopped equipment-log
docker start equipment-log
```

and put the legacy mDNS file back. Changes made in Sklad meanwhile are not in
the legacy app; note them by hand. Keep the stopped `equipment-log` container
and its data folder until Sklad has run through at least one full rental cycle;
then remove both and the avahi file, and **revoke the old Gmail app password**.

## 5. Upgrade

1. Tag the release in git and test it on the test stack
   (`./scripts/make-test-bundle && ./scripts/push-test-to-nas … --keep-data`).
2. From the same commit:

   ```sh
   ./scripts/make-bundle
   ./scripts/push-to-nas David_Zitnik@<nas-ip>
   ```

`nas-deploy` then, on the NAS:

1. copies the live `.env` and `deploy/certs/*` into the new bundle;
2. backs up the running database and CA to `/share/Backup/sklad/<time>/` —
   if that fails, nothing changes;
3. builds `sklad:<tag>` while the old release keeps serving — a failed build
   changes nothing;
4. moves `/share/Container/sklad` to `sklad.previous`, puts the new release in
   place, sets `SKLAD_IMAGE_TAG=<tag>` in `.env`, recreates the containers and
   waits up to 3 minutes for both to be healthy;
5. appends a line to `/share/Container/.sklad-deploy-history`.

Downtime is the container restart (seconds). The first start of a new release
may run database migrations; after that an older image refuses the database
(see §6). The same tag is never built twice (`--rebuild` overrides, at the cost
of that rollback point). Remove images older than the previous release now and
then: `./scripts/sklad-stack images`, then `docker image rm sklad:<old-tag>`.

## 6. Rollback

```sh
cd /share/Container/sklad
./scripts/sklad-stack rollback
```

swaps `sklad.previous` back in (with its `.env`), starts the previous image and
waits for health. The rolled-back folder is kept as `sklad.rolledback-<time>`.

If the new release migrated the database, the old image refuses to start with
*"database has migrations this release does not know … written by a newer Sklad
version"*. Then restore the backup `nas-deploy` took just before the upgrade
(the `backup=` field in `./scripts/sklad-stack history`):

```sh
./scripts/sklad-stack restore-db /share/Backup/sklad/<time>/sklad.db
```

Rentals recorded between the upgrade and the restore are lost; note them before
restoring. `restore-db` stops the app, keeps the current files in the volume
under `/data/pre-restore-<time>/`, writes the backup and starts the app again.

**Manual rollback** (no `sklad.previous`, e.g. a second rollback in a row): set
`SKLAD_IMAGE_TAG` in `.env` to a tag listed by `./scripts/sklad-stack images`
and run `./scripts/sklad-stack up`; restore the matching backup if the schema
differs.

## 7. HTTPS, cameras and labels

Follow [TLS-CAMERA.md](TLS-CAMERA.md) to install and trust Caddy's root CA on
the devices. Download it from `/ca.crt` on the HTTPS origin and verify the
fingerprint with a trusted administrator before installing it. Clicking through
a certificate warning does not enable the camera. Once every device trusts the
CA, raise `SKLAD_HSTS_MAX_AGE` (e.g. `31536000`) and run
`./scripts/sklad-stack up`.

Caddy's health check requests `/api/v1/health` through its own HTTPS listener
(skipping trust verification); check trusted access from a client with
`curl --cacert sklad-root-ca.crt https://HOST:PORT/api/v1/health`. The CA lives
in the `sklad_caddy_data` volume: losing it means every device has to trust a
new CA, so it is part of every `sklad-stack backup`.

PT-E300 labels are made on the printer keyboard, not uploaded. Follow
[LABELS.md](LABELS.md): CODE128 with the exact asset ID, scan before attaching.
Test phone cameras, a USB/HID scanner if used, and long/short IDs.

## 8. Backups and restore

**Automatic, inside the data volume.** The server writes a consistent SQLite
copy (`VACUUM INTO`) at startup and every `SKLAD_BACKUP_INTERVAL`, keeps the
newest 50 in `/data/backups` (mode 0600) and retries failures after a minute.
Watch the logs for `database backup completed` / `database backup failed`.
These copies sit on the same disk as the database: they cover mistakes, not NAS
failure.

**Daily copy out of the volume, including the CA.** `sklad-stack backup` writes
`/share/Backup/sklad/<time>/{sklad.db, caddy-pki.tar.gz, INFO}` (mode 600,
newest 30 kept; `SKLAD_BACKUP_KEEP` changes that). Schedule it in QNAP's
persistent crontab:

```sh
echo '30 2 * * * /share/Container/sklad/scripts/sklad-stack backup /share/Backup/sklad >>/share/Backup/sklad/backup.log 2>&1' >> /etc/config/crontab
crontab /etc/config/crontab && /etc/init.d/crond.sh restart
```

**Off the NAS.** Point a Hybrid Backup Sync 3 job (or rsync to another machine)
at `/share/Backup/sklad`, daily, after 02:30, with alerts on failure; check
`backup.log` and the newest folder date weekly. The backups contain every table
— accounts, personnel, audit log — and the CA private key: store them as
sensitive data.

On demand (before risky changes): `./scripts/sklad-stack backup`.

**Restore** a database or CA on the live stack:

```sh
./scripts/sklad-stack restore-db /share/Backup/sklad/<time>/sklad.db
./scripts/sklad-stack restore-ca /share/Backup/sklad/<time>/caddy-pki.tar.gz
```

Both ask for confirmation, keep the replaced files and wait for health. Use a
backup from the same or an older release than the running image (a newer schema
is refused — run that release instead). Restored session rows come back too; for
a security recovery, follow §9 to revoke them.

**Restore drill** (before go-live, then yearly) on the test stack, so the live
service is untouched:

```sh
cd /share/Container/sklad-test
./scripts/test-stack compose stop test-app
docker run --rm -i --entrypoint sh -v sklad_test_data:/data sklad:test \
  -c 'rm -f /data/sklad.db-wal /data/sklad.db-shm && cat > /data/sklad.db' \
  < /share/Backup/sklad/<time>/sklad.db
./scripts/test-stack compose start test-app
```

Then check health, Admin login, counts, personnel references, options, one
rental update and persistence after a restart. Disable Discord alerts on the
test stack first. Record the backup date, release and result. `test-stack
reset` returns the test stack to an empty database afterwards.

## 9. Recover a forgotten Admin password without losing inventory

A local operator command, not a public reset endpoint. It targets only
`SKLAD_ADMIN_USERNAME` with role Admin; it will not create a missing account or
elevate an Editor. In a Bash shell on the NAS (the password is not echoed and
not put in arguments):

```bash
cd /share/Container/sklad
./scripts/sklad-stack compose stop app
read -r -s -p 'Temporary Admin password (12–1024 bytes): ' recovery_password
printf '\n'
printf '%s\n' "$recovery_password" | ./scripts/sklad-stack compose run --rm --no-deps -T app reset-admin --password-stdin
recovery_result=${PIPESTATUS[1]}
unset recovery_password
if [ "$recovery_result" -eq 0 ]; then
  ./scripts/sklad-stack up
else
  echo 'Recovery failed; inspect the error before restarting.'
fi
```

It requires an existing database, takes a backup first (failure aborts),
updates and reactivates the Admin, revokes its sessions and writes an audit
event without the password. Sign in with the temporary password and change it.
Keep the pre-recovery snapshot private. Never delete the database to reset a
password.

## 10. Optional Discord configuration

Admin → Settings: webhook, automatic alerts, local check hour and days ahead. A
test message or manual overdue run works while automatic alerts are disabled.
Verify delivery, timezone and duplicate suppression. Feasibility items must
never be included, even with a past return date. Test the loan → physical
return confirmation → overdue check sequence.

## 11. Acceptance checklist (test stack, then live)

- [ ] Image built on the NAS for its architecture; both containers healthy.
- [ ] Legacy import rehearsed: counts, IDs, types/statuses; personnel added.
- [ ] Admin bootstrap, forced password change and offline recovery (§9) tested.
- [ ] Anonymous rental updates work; identity/options/personnel changes are rejected.
- [ ] Individual/bulk Customer notes and required renters enforced.
- [ ] Only Admin returns items to Feasibility; returned devices are excluded from overdue reports.
- [ ] Concurrent edits produce a conflict, not lost updates.
- [ ] Phone cameras/scanner and PT-E300 labels work over trusted HTTPS.
- [ ] `sklad-stack backup` scheduled, off-NAS copy running, restore drill (§8) done.
- [ ] Upgrade and `rollback` rehearsed once on the live folder before users depend on it.
- [ ] NAS reboot keeps data, CA, name resolution and restarts both containers.
- [ ] Discord test/report checked with approved recipients (if used).
- [ ] Only LAN/VPN clients reach the ports.

Local automated checks do not replace these on-device tests.
