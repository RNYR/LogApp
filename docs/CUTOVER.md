# Go-live: replace `equipment-log` with Sklad and delete the old app

A checklist for the one-time switch on the QNAP. At the end the legacy
`equipment-log` app is gone from the NAS — container, image, folder, data and
mDNS file — and Sklad runs in its place as `https://sklad.local`. Only a
private archive of the old app remains in `/share/CACHEDEV1_DATA/Feasibility_NAS/Backup/sklad`.

Background and the commands behind each step: [DEPLOY-QNAP.md](DEPLOY-QNAP.md).
The Sklad test stack (`sklad-test`) is not touched by any step here.

Placeholders: `<nas-ip>` = the NAS address, `<user>` = your NAS SSH account,
`<tag>` = the release, e.g. `2.0.6`.

## 1. Before the day

- [ ] The release is tagged in git (`v<tag>`) and CI is green.
- [ ] Optional but recommended: the same release passed the test stack,
      including an import rehearsal ([DEPLOY-QNAP.md §3](DEPLOY-QNAP.md#3-initial-data)).
- [ ] You can SSH to the NAS from the Mac: `ssh <user>@<nas-ip>`.
- [ ] Users know about a short freeze (about 30 minutes, no rentals) and the
      new address `https://sklad.local`. The old `http://sklad.local:8787`
      stops working.
- [ ] Decide the Admin password you will use on first login.

## 2. Install the files and build the image (any time before the window)

The old app keeps running during this step. On the Mac, in a clean checkout of
the release:

```sh
git checkout v<tag>
./scripts/make-bundle
./scripts/push-to-nas <user>@<nas-ip>
```

This is a first install, so the push only copies the files to
`/share/CACHEDEV1_DATA/Feasibility_NAS/Container/sklad`. On the NAS:

```sh
export PATH=/share/CACHEDEV1_DATA/.qpkg/container-station/bin:$PATH
cd /share/CACHEDEV1_DATA/Feasibility_NAS/Container/sklad
cp .env.example .env && chmod 600 .env
vi .env
```

In `.env` set:

| Key | Value |
|---|---|
| `SKLAD_ADMIN_PASSWORD` | temporary password, **12+ characters** (changed on first login) |
| `SKLAD_LAN_IP` | `<nas-ip>` |
| `SKLAD_IMAGE_TAG` | `<tag>` |
| `SKLAD_HSTS_MAX_AGE` | `0` (already the default) |
| `SKLAD_HTTPS_PORT` etc. | only if QTS itself uses port 443: `SKLAD_HTTP_PORT=8180`, `SKLAD_HTTPS_PORT=8443`, `SKLAD_PUBLIC_PORT=:8443` |

Build the image (10–30 minutes) and look at what the cut-over will find:

```sh
./scripts/sklad-stack build
./scripts/nas-cutover status
```

`status` changes nothing. Check that it found the legacy **folder**, its
**data** with the expected item count (81), and whether an mDNS file
(`logapp.service`) is installed. If no mDNS file is listed, find out how
`sklad.local` resolves today (router DNS, `/etc/avahi/hosts`, the NAS host
name) — after the switch it has to point at the NAS again.

## 3. Switch (in the window)

```sh
cd /share/CACHEDEV1_DATA/Feasibility_NAS/Container/sklad
./scripts/nas-cutover go
```

It asks for confirmation, then:

1. stops `equipment-log` (it stays installed, set not to restart);
2. archives the whole legacy folder and mDNS file to
   `/share/CACHEDEV1_DATA/Feasibility_NAS/Backup/sklad/legacy-equipment-log-<time>.tar.gz` (mode 600; it
   contains the old SMTP password);
3. exports only `inventory.json` and `options.json` for the import;
4. runs an import **dry run** and shows the counts — compare them with
   `status` and type `import` to continue;
5. imports, checks ports (`preflight`) and starts Sklad, waiting until both
   containers are healthy;
6. takes the first backup.

If anything fails on the way, the script stops and says so; the old app is
stopped but intact. Fix the cause and run `go` again, or go back with
`./scripts/nas-cutover undo`.

## 4. Verify (old app still installed)

- [ ] `https://<nas-ip>` opens (accept the warning until the CA is trusted).
- [ ] Sign in as `admin` with the `.env` password; change it when asked.
- [ ] Item count and a few items (IDs, status, dates, notes) match the old app.
- [ ] Manage options: equipment types and statuses are there; add personnel.
- [ ] Trust the CA on one phone ([TLS-CAMERA.md](TLS-CAMERA.md)); scan a label;
      record one rental and return it.
- [ ] `http://<nas-ip>` redirects to HTTPS.
- [ ] Reboot the NAS once; afterwards `./scripts/sklad-stack ps` shows both
      containers healthy and the data is still there.

Something wrong and not quickly fixable? `./scripts/nas-cutover undo` stops
Sklad (its data stays) and starts `equipment-log` again.

## 5. Delete the old app

```sh
./scripts/nas-cutover finish
```

It lists exactly what it will delete and asks you to type `delete`:

- the `equipment-log` container and its image;
- the legacy folder, including `app/data` (inventory, options, SMTP config,
  old backups);
- the installed `logapp.service` mDNS file — replaced by Sklad's
  `sklad.service` in the same Avahi directory (restart Avahi or reboot);
- the exported copies (`legacy-final`, and `legacy-copy` from a rehearsal).

It refuses while Sklad is unhealthy or before `go` has made the archive. After
`finish`, `undo` is no longer possible; the only way back is the archive
(see below). `./scripts/nas-cutover status` then shows everything as deleted.

## 6. Afterwards

- [ ] `https://sklad.local` resolves on a laptop and a phone (Android may need
      a router DNS entry or the IP).
- [ ] Trust the CA on the remaining devices; then raise `SKLAD_HSTS_MAX_AGE`
      to `31536000` and run `./scripts/sklad-stack up`.
- [ ] Schedule the nightly backup and the off-NAS copy
      ([DEPLOY-QNAP.md §8](DEPLOY-QNAP.md#8-backups-and-restore)).
- [ ] **Revoke the old Gmail app password** of the legacy notification account.
- [ ] After a few weeks of normal use, delete the legacy archive in
      `/share/CACHEDEV1_DATA/Feasibility_NAS/Backup/sklad` (it holds that password and the old data).

## Emergency: bring the old app back after `finish`

Only from the archive, with Sklad stopped:

```sh
cd /share/CACHEDEV1_DATA/Feasibility_NAS/Container/sklad && ./scripts/sklad-stack down
tar -xzf /share/CACHEDEV1_DATA/Feasibility_NAS/Backup/sklad/legacy-equipment-log-<time>.tar.gz -C /
cd <legacy folder shown by status> && docker-compose up -d --build
```

Rentals recorded in Sklad meanwhile are not in the old app.
