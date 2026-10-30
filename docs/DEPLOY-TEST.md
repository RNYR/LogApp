# Test stack on the NAS — `https://sklad-test.local:8444`

A second, fully separate Sklad on the same QNAP for looking over a build before
it replaces the live `https://sklad.local`. It starts with an **empty
database** (only the first-boot admin) and shares nothing with the live stack:

| | Live | Test |
|---|---|---|
| Address | `https://sklad.local` (port 443, or 8443) | `https://sklad-test.local:8444` |
| Compose file / project | `docker-compose.yml` / `sklad` | `docker-compose.test.yml` / `sklad-test` |
| Containers | `sklad-app`, `sklad-caddy` | `sklad-test-app`, `sklad-test-caddy` |
| Image | `sklad:${SKLAD_IMAGE_TAG:-local}` | `sklad:test` |
| Volumes | `sklad_sklad_data`, `sklad_caddy_*` | `sklad_test_data`, `sklad_test_caddy_*` |
| Config | `.env` | `.env.test` |
| Root CA | Sklad Local Authority | Sklad **TEST** Local Authority |
| Cookies | `sklad_session`, `sklad_csrf` | `sklad_test_session`, `sklad_test_csrf` |

Always use `./scripts/test-stack` (or the push script), never plain
`docker compose`, for the test stack: the wrapper pins the project name, compose file and `.env.test`, and
refuses ports the live stack uses. A plain `docker compose up` in the folder
starts/rebuilds the **live** stack.

## 1. Install or replace it from your Mac

```sh
cd /Users/v4rdr/MUNINN/work/sklad_revised
./scripts/make-test-bundle                          # writes ./sklad-test
./scripts/push-test-to-nas David_Zitnik@<nas-ip>    # uploads + replaces
```

`push-test-to-nas` uploads the bundle, then on the NAS (`scripts/nas-replace`):

1. removes the test stack that is currently running: containers
   `sklad-test-app`/`sklad-test-caddy`, image `sklad:test`, network
   `sklad_test` and the volumes `sklad_test_*` (test database and test CA);
2. replaces `/share/Container/sklad-test` with the new files, keeping your
   `.env.test`;
3. builds and starts the new version (`./scripts/test-stack up`).

Options: `--keep-data` keeps the test database and CA (use it for code updates
once you have test data worth keeping); `--no-start` stops after step 2;
`--target DIR` uses another folder. Nothing outside the `sklad-test` /
`sklad_test` names is touched — the old `equipment-log` app keeps running.

## 2. First install only: configure

If there was no `.env.test` yet, the push stops after copying and tells you to:

```sh
cd /share/Container/sklad-test
cp .env.test.example .env.test && chmod 600 .env.test
vi .env.test       # SKLAD_TEST_LAN_IP=<nas-ip>, SKLAD_ADMIN_PASSWORD=<test password>
export PATH=/share/Container/bin:$PATH
./scripts/test-stack up
```

## 3. Open it

`https://<nas-ip>:8444` (accept the certificate warning until the test CA is
trusted). The landing page is Inventory in anonymous Editor mode; sign in as
`admin` with the `.env.test` password for administration and change it when
asked. A fresh database seeds only the Customer and Feasibility statuses: add
the equipment types and other statuses in Manage options before testing
rentals (see [DEPLOY-QNAP.md](DEPLOY-QNAP.md) §3).

## 4. Make `sklad-test.local` resolve (optional)

The live name is the NAS's own mDNS host name; a second name needs its own
address record. Pick one:

- **Avahi on the NAS:** add `<nas-ip>  sklad-test.local` to `/etc/avahi/hosts`,
  copy `deploy/avahi/sklad-test.service` to `/etc/avahi/services/`, restart
  Avahi.
- **Router DNS:** add a host entry `sklad-test.local → <nas-ip>`.
- **No name at all:** use `https://<nas-ip>:8444`.

## 5. Trust the test CA (per device)

The test stack has its own root CA, so each device that should use the camera
on the test site must trust it once — same steps as in
[TLS-CAMERA.md](TLS-CAMERA.md), but download from
`https://sklad-test.local:8444/ca.crt` (file `sklad-test-root-ca.crt`, shown as
"Sklad TEST Local Root CA"). Remove it from the devices when the test stack is
retired.

## 6. Day-to-day

```sh
./scripts/test-stack up        # after pulling new code: rebuild + restart
./scripts/test-stack restart
./scripts/test-stack down      # stop; data and CA kept for next time
./scripts/test-stack reset     # stop and DELETE test data + test CA (asks first)
```

`reset` gives you a clean empty database again. It only removes the
`sklad_test_*` volumes; the live data is never touched.

## 7. Retiring the test stack

```sh
./scripts/test-stack reset
docker image rm sklad:test
```

Then remove the `/etc/avahi/hosts` line, `/etc/avahi/services/sklad-test.service`
(or the router entry) and the test root CA from your devices.
