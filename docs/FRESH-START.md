# Fresh start: set up the stock on the test stack, then make it live

Use this to rebuild the inventory from scratch without disturbing the live app:

1. a **test copy** of Sklad runs beside the live one with an **empty database**
   (`https://<nas-ip>:8444`);
2. you configure types, statuses, personnel and items there, at your own pace;
3. when it is ready, the test database **replaces** the live one in one step.

Until step 3 the live app (`https://<nas-ip>:8443`) keeps working as usual.
Paths below are for the `Feasibility_NAS` share; `<nas-ip>` = the NAS address,
`<user>` = your SSH account.

## 1. Build the empty test copy

On the Mac, from a release tag (`git checkout v<version>`):

```sh
./scripts/make-test-bundle
./scripts/push-test-to-nas <user>@<nas-ip>
```

Without `--keep-data` the push **deletes any earlier test database** — that is
what makes it empty. The live stack is never touched. The first time, it only
copies files and asks you to configure. On the NAS:

```sh
cd /share/CACHEDEV1_DATA/Feasibility_NAS/Container/sklad-test
cp .env.test.example .env.test && chmod 600 .env.test
vi .env.test
```

Set:

| Key | Value |
|---|---|
| `SKLAD_TEST_LAN_IP` | `<nas-ip>` |
| `SKLAD_ADMIN_USERNAME` | **the same username as the live Admin** (see below) |
| `SKLAD_ADMIN_PASSWORD` | temporary, 12+ characters; changed on first login |

The username matters: only the account named in the live `.env`
(`SKLAD_ADMIN_USERNAME`) can sign in to the live app, and after step 3 the
live app uses the accounts from this test database. `restore-db` refuses a
database without that account.

Build and start (10–30 minutes the first time):

```sh
./scripts/test-stack up
./scripts/test-stack ps          # both containers healthy
```

## 2. Configure the stock

Open `https://<nas-ip>:8444` (accept the certificate warning, or trust the
test CA from `/ca.crt` on devices that need the camera). Sign in with the
`.env.test` username and password and set your real password — **this becomes
the live Admin password after step 3**.

- **Manage options:** equipment types and statuses (Customer and Feasibility
  exist already).
- **Personnel:** the people who rent equipment.
- **Inventory → + Add new entry:** the items. Leave the ID empty for the next
  free number, or type the ID that is already on an item's label (e.g. `PV-017`)
  so existing labels stay valid.
- **Settings:** leave Discord **off** here, so the test copy never posts. Set
  it up on the live app after step 3.

Scanning works on the test copy as well (Scan page, manual entry, photos).

## 3. Replace the live data

Pick a quiet moment: rentals recorded on the **live** app from now until the
switch are lost (the old data stays in the backup).

On the NAS, export the test database:

```sh
cd /share/CACHEDEV1_DATA/Feasibility_NAS/Container/sklad-test
./scripts/test-stack export
```

It prints the file, e.g.
`/share/CACHEDEV1_DATA/Feasibility_NAS/Backup/sklad-test/20261120T140000Z/sklad.db`.
Load it into the live app:

```sh
cd /share/CACHEDEV1_DATA/Feasibility_NAS/Container/sklad
./scripts/sklad-stack restore-db /share/CACHEDEV1_DATA/Feasibility_NAS/Backup/sklad-test/<time>/sklad.db
```

`restore-db` checks the admin account, asks you to type `restore`, **backs up
the current live database** to `Feasibility_NAS/Backup/sklad/<time>/`, swaps
the database, restarts the app and waits until it is healthy.

Then on `https://<nas-ip>:8443`:

- [ ] sign in with the Admin password you set on the test copy;
- [ ] items, types, statuses and personnel are what you prepared;
- [ ] **Settings:** enter the Discord webhook again and tick "Enabled" and
      "Post rental changes" (settings travel with the database, and the test
      copy had them off);
- [ ] record and return one rental from a phone.

## 4. Afterwards

Stop the test copy (its data stays until the next fresh push):

```sh
cd /share/CACHEDEV1_DATA/Feasibility_NAS/Container/sklad-test
./scripts/test-stack down
```

**Going back:** the live data from before the switch is in
`Feasibility_NAS/Backup/sklad/<time>/sklad.db` (the folder `restore-db`
printed). `./scripts/sklad-stack restore-db <that file>` restores it.

Things that do **not** carry over: the old rental history and audit log (they
stay in that backup), Discord settings (re-enter them), and sign-ins (everyone
signs in again). The live Caddy certificate is untouched, so devices that
trust the live CA keep working.
