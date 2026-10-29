# Contributing to Sklad

Thanks for your interest in improving Sklad! This document explains how to set
up the project, the conventions we follow, and what we expect from a pull
request.

By participating you agree to be respectful and constructive. Be kind, assume
good faith, and keep discussion focused on the code.

## Ways to contribute

- Report bugs and request features via issues.
- Improve documentation (including `cs`/`it` translations).
- Submit pull requests for fixes, tests, and features.
- Report security issues privately — see [SECURITY.md](SECURITY.md), **not** a
  public issue.

## Prerequisites

| Tool     | Version | Notes                                            |
|----------|---------|--------------------------------------------------|
| Go       | 1.23+   | Backend. Module root is the repository root.     |
| Node.js  | 20+     | Frontend (Vite/React). `npm` is fine.            |
| Docker   | recent  | Only for the TLS/mDNS stack and full-stack tests.|

## Project layout

Backend Go sources live under `server/` while the Go module is rooted at the
repository root, so packages import as `sklad/server/...`. The frontend is under
`web/`. Frozen contracts live in `docs/contracts/` and `openapi/openapi.yaml`;
**do not change a frozen contract without a dedicated PR and maintainer review.**

## Local setup

```bash
git clone <repository-url> sklad && cd sklad
cp .env.example .env          # set SKLAD_ADMIN_PASSWORD (+ SKLAD_COOKIE_SECRET)

# Backend
go build ./server/...
go test -race ./server/...

# Frontend
cd web
npm install
npm run dev                   # SPA dev server, proxies /api to the backend
```

## Checks that must pass

Run these before opening a PR (CI runs the same set):

```bash
# Backend
gofmt -l server scripts        # must print nothing
go vet ./server/...
go test -race ./server/...

# Frontend
cd web
npm run lint
npm run typecheck
npm run test
npm run build
npm run check:i18n             # en/cs/it key parity
```

## Coding conventions

- **Go:** `gofmt`/`goimports`, small focused packages, errors wrapped with
  context, no `panic` for expected failures. Keep `CGO_ENABLED=0`.
- **TypeScript/React:** strict TypeScript, function components with hooks,
  Tailwind utility classes, no `any` unless justified. Prefer the typed API
  client generated from `openapi/openapi.yaml`.
- **SQL:** migrations are **append-only** — never edit a shipped migration; add
  a new numbered one.
- **Security:** never log or commit secrets, never disable CSRF or cookie
  flags, always use parameterised SQL.

## Internationalisation (i18n)

`web/src/locales/en.json` is the **source of truth**. When you add or change a
user-facing string:

1. Add the key to `en.json`.
2. Add the identical key to `cs.json` and `it.json` (translate it; keep the key
   path identical).
3. Run `npm run check:i18n` — CI fails on missing or extra keys.

Never rename or delete a key that another locale still references in the same
PR without updating all three files.

## Tests

- New backend behaviour needs a Go test; use `go test -race`.
- Bug fixes should include a regression test.
- Bulk operations must be covered for transactionality (all-or-nothing) and
  optimistic-concurrency conflicts.
- Frontend logic (scanner, label rendering, i18n) needs Vitest coverage;
  user-critical flows get a Playwright smoke test.

## Commits and pull requests

- Keep commits focused and the history readable; small PRs are reviewed faster.
- Write imperative, descriptive commit subjects
  (e.g. `fix(scan): debounce duplicate CODE128 frames`).
- Reference the issue a PR closes (`Closes #123`).
- Describe **what** changed, **why**, and **how you tested it**.
- Update `CHANGELOG.md` under `[Unreleased]` for user-visible changes.
- Ensure CI is green before requesting review.

## Licensing of contributions

Sklad is MIT-licensed. By submitting a contribution you agree that it is your
own work (or you have the right to submit it) and that it may be distributed
under the project's MIT License.
