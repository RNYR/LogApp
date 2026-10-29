# Rental workflow implementation and verification

See [roles and functions](roles-and-functions.md) for the current permission matrix.

## Repeatable checks

From `web/`:

```sh
npm run check:i18n
npm test
npm run build
npm run test:e2e -- --workers=2
npm run test:rentals
```

`test:rentals` requires Go on PATH (or `GO_BINARY=/absolute/path/to/go`) and Playwright Chromium. It builds the real Go server, creates a disposable database, starts backend port 18080 and Vite port 4198, and removes test data on shutdown. It does not use the Sites mock or a production database. Ports must be free.

From `server/`, run `go test ./...` for API validation, authentication, atomic updates, migration preservation, jobs and store tests.

Coverage includes camera decoding with simulated media, manual label input, checkboxes, shared-buffer navigation, individual/bulk updates, locked Feasibility fields, required renters, Customer notes, Admin-only return confirmation, directory lifecycle and optimistic concurrency. Physical scanner/camera and printer hardware still require acceptance testing on the target devices.

## NAS rollout

1. Back up the existing database/data directory and retain the previous executable for rollback.
2. Build the normal frontend (`npm run build`), then the Go executable for the NAS architecture using the existing deployment instructions. Never use `--mode preview` for NAS production.
3. Start with the existing persistent data directory and configured Admin identity. Migration 0002 runs automatically; verify the backup before upgrading. To roll back, restore both the pre-upgrade database and binary.
4. Sign in as Admin and populate the Personnel directory. Review legacy rentals and assign missing renters as needed; migration preserves their original status/history.
5. Verify anonymous rental access, Admin sign-in, TLS/camera access, persistence across restart, backups, and any configured notifications on the NAS. Follow [DEPLOY-QNAP.md](DEPLOY-QNAP.md).

The hosted Sites version remains an interactive browser-local preview with simulated Admin authentication and notifications. It is not the NAS backend and contains no real shared accounts. Only `admin` signs in there (any test password); Editor is the default anonymous mode.

## Operational regression checks (2026-10-18)

Feasibility is excluded from inventory overdue filtering and scheduled/manual
reports, while prior dates remain stored. Regression tests exercise that transition.
The backup worker runs at startup and repeats, retries failures and cancels on
shutdown. Store tests restore a completed snapshot and verify personnel references
and SQLite integrity; failed attempts do not publish a `.db` file. Recovery tests
verify inventory preservation, old-password rejection, session revocation and a
required password change, including the offline CLI path.

Verified locally: full backend suite, race checks on store/jobs/auth/maintenance,
six desktop/mobile real-backend rental tests, linux/amd64 cross-build, and a real
process smoke test covering startup backup → graceful stop → offline recovery →
restart → login requiring password change. Docker image execution and NAS hardware
acceptance remain staging tasks. The CI browser job now also runs the real-backend
rental suite and is no longer allowed to fail silently.
