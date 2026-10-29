# UI Overhaul — Phase 0 Baseline & Phase 1 Foundation

This document is the Phase 0 baseline artifact for
[`docs/ui-overhaul-plan.md`](../ui-overhaul-plan.md) and the record of the
Phase 1 (additive foundation) gate. It contains only commands that were actually
executed and their real output. Historical claims in the plan (for example the
previously noted `6 passed` E2E baseline) are identified as historical where
relevant and were re-verified here.

- **Executed:** 2026-10-13 (UTC)
- **Repository revision:** `058b67b82af02be7682178f130af178449d49fbc`
- **Tooling:** `node v26.10.0`, `npm 11.19.1`, Playwright `1.63.0`,
  bundled Chromium `153.0.8010.12` (revision `1243`)
- **Working-tree note:** the checked-out tree contained unrelated deletions under
  `EFR-743/` and an untracked copy of `docs/ui-overhaul-plan.md` at the time of
  the runs. No application source was modified when the "Before Phase 1" gate was
  run.

> **No screenshots are committed in this repository.** See
> [§5](#5-screenshot--visual-baseline-procedure) for exact regeneration
> instructions. The only Phase 0 artifacts captured here are text-based (gate
> results, route/role matrix, selector contract).

---

## 1. Phase-gate results

All commands were run from `web/` unless stated otherwise, individually, so that
one failure could not hide the next result. `EXIT` is the process exit code.

### 1.1 Before Phase 1 (unmodified `web/` source)

| Command | Result | Key output |
|---|---|---|
| `npm ci` | EXIT 0 | `added 305 packages`; `17 vulnerabilities (7 moderate, 8 high, 2 critical)`; warn deprecated `eslint@9.39.5`; 2 install-script warnings (`esbuild@0.21.5`, `fsevents@2.3.3`) |
| `npm run typecheck` | EXIT 0 | no output (0 errors) |
| `npm run lint` | EXIT 0 | `0 errors, 9 warnings` — 6 `react-refresh`, 3 `react-hooks` (all pre-existing) |
| `npm run check:i18n` | EXIT 0 | `cs.json — 237 keys`, `it.json — 237 keys`, `en -> cs, it` consistent |
| `npm run test` | EXIT 0 | `scanner.test.ts (17 tests)`, `17 passed` |
| `npm run build` | EXIT 0 | `354 modules transformed`, `built in 2.20s`, CSS `18.75 kB`; chunk-size >500 kB warning |
| `npx playwright install chromium` | EXIT 0 | (Chromium already present) |
| `npm run test:e2e` | EXIT 0 | `6 passed (3.7s)` — chromium + mobile-chrome |

The environment/version checks were run from the repository root:

```text
node --version   -> v26.10.0
npm --version    -> 11.19.1
git rev-parse HEAD -> 058b67b82af02be7682178f130af178449d49fbc
```

The pre-existing 9 lint warnings are:
`react-refresh/only-export-components` (**6**: 2 in `BulkActionPanel.tsx`, 4 in
`ItemEditor.tsx`) and `react-hooks/exhaustive-deps` (**3**: 2 in
`InventoryTable.tsx`, 1 in `ScanView.tsx`).

`npm ci` also reports **17 pre-existing dependency vulnerabilities** (7
moderate, 8 high, 2 critical). They exist in the locked dependency tree, are
unrelated to this styling change, and must be remediated separately (not by
expanding Phase 1). No dependency versions were changed by this work.

### 1.2 After Phase 1 (semantic tokens + base-layer + controls)

| Command | Result | Key output |
|---|---|---|
| `npm ci` | EXIT 0 | `added 305 packages`; `17 vulnerabilities (7 moderate, 8 high, 2 critical)`; same deprecation/install-script warnings as baseline |
| `npm run typecheck` | EXIT 0 | no output (0 errors) |
| `npm run lint` | EXIT 0 | `0 errors, 9 warnings` — identical set to the baseline (6 refresh, 3 hooks) |
| `npm run check:i18n` | EXIT 0 | `237 keys`, `en -> cs, it` consistent |
| `npm run test` | EXIT 0 | `scanner.test.ts (17)` + new `controls.test.ts` (4) = `21 passed` |
| `npm run build` | EXIT 0 | `354 modules transformed`, `built in ~2.2s`, CSS `19.51 kB`; chunk-size >500 kB warning |
| `npx playwright install chromium` | EXIT 0 | (Chromium already present) |
| `npm run test:e2e` | EXIT 0 | `18 passed` (~7s) — chromium + mobile-chrome |

Phase 1 added **no new lint warnings, no new dependencies, and no new UI
strings**; the 9 warnings are the same pre-existing issues as the baseline.

Post-change E2E totals: 18 executions = 3 pre-existing spec tests × 2 projects
(6), plus 6 new Phase 1 regression tests × 2 projects (12).

- `scan → buffer → bulk › uses the Phase 1 primary color and shared control focus tokens`
- `phase 1 foundation › honors reduced motion while keeping the light color scheme and dark canvas`
- `phase 1 foundation › shows a visible global focus indicator on headers, glyph actions and swatches`
- `phase 1 foundation › uses the shared control geometry and focus ring for Input and Select`
- `phase 1 foundation › renders invalid Input and Select in the browser with danger borders and rings`
- `phase 1 foundation › keeps the dark canvas and light color scheme at desktop and mobile widths`

All new tests pass under `chromium` (Desktop Chrome) and `mobile-chrome`
(Pixel 5). The full project set was run; the gate was **not** narrowed with
`--project`.

### 1.3 Token and contrast verification (Phase 1)

The following values were read back from `web/tailwind.config.js` via a Node
`import()` and match plan §4.1 exactly:

```text
canvas #f8fafc
panel.DEFAULT #ffffff / panel.subtle #f1f5f9
content.DEFAULT #0f172a / content.muted #475569
stroke.DEFAULT #cbd5e1 / stroke.control #64748b
action.DEFAULT #0b6287 / action.hover #0d506e / action.selected #eaf7fd
success.fg #166534 / success.bg #f0fdf4
warning.fg #92400e / warning.bg #fffbeb
danger.fg #b91c1c / danger.bg #fef2f2
info.fg #0b6287 / info.bg #eaf7fd
fontFamily.sans[0] = ui-sans-serif      (Montserrat removed from the stack)
boxShadow.overlay = 0 8px 24px rgba(15, 23, 42, 0.12)
borderRadius.card = 1rem                (retained for existing Card consumers)
```

Primary-button contrast (WCAG 2.1 relative-luminance formula, computed locally):

| Pair | Ratio | Requirement |
|---|---|---|
| white on `#0b6287` (`brand-700`, primary) | **6.75:1** | ≥ 4.5:1 normal text |
| white on `#0d506e` (`brand-800`, primary hover) | **8.79:1** | ≥ 4.5:1 normal text |

The legacy `ink`, `surface`, `brand` and `status` tokens were preserved unchanged
and remain available; `ink`/`surface` carry deprecation comments in the config.

### 1.4 Shared-control token verification

Grep of the built stylesheet (`dist/assets/index-*.css`) confirms the control
classes compile to the intended values:

```text
.border-stroke-control                     -> border-color: rgb(100 116 139 / …)
.focus\:border-action:focus                -> border-color: rgb(11 98 135 / …)
.focus\:ring-action:focus                  -> --tw-ring-color: rgb(11 98 135 / …)
.focus\:ring-offset-2:focus                -> --tw-ring-offset-width: 2px
.border-danger-fg                          -> border-color: rgb(185 28 28 / …)
.focus\:border-danger-fg:focus             -> border-color: rgb(185 28 28 / …)
.focus\:ring-danger-fg:focus               -> --tw-ring-color: rgb(185 28 28 / …)
:focus-visible                             -> outline: 2px solid #0b6287; outline-offset: 2px
@media (prefers-reduced-motion: reduce)    -> .animate-fade-in{animation:none!important}
                                              *,*:before,*:after{transition-duration:0s!important; …}
```

A new Vitest suite (`web/src/components/ui/controls.test.ts`, 4 tests) renders
`Input`/`Select` to static markup and asserts the valid (neutral) and invalid
(danger) class contract. `npm run test` → `2 files, 21 passed`. Because no
screen passes `invalid`, browser-rendered coverage is supplied by a committed
test fixture (`web/e2e/fixtures/invalid-controls.{html,tsx}`) plus the Playwright
test `phase 1 foundation › renders invalid Input and Select in the browser …`.

---

## 2. Route / role matrix

Source of truth: `web/src/app/router.tsx` (`createBrowserRouter`),
`web/src/lib/auth/RequireAuth.tsx`, and `web/src/components/layout/AppShell.tsx`
(`NAV_ITEMS`).

| Route | Screen | Anonymous | Authenticated non-admin (viewer/editor) | Admin | Notes |
|---|---|---|---|---|---|
| `/login` | `features/auth/Login.tsx` | Public login form | Redirect to `/` (or the `state.from` target) | Redirect to `/` | Only public route. |
| `/` | `features/inventory/InventoryTable.tsx` | → `/login` | Inventory table | Inventory table | Index route. |
| `/scan` | `features/scan/ScanView.tsx` | → `/login` | Scan → buffer → bulk | Scan → buffer → bulk | |
| `/labels` | `features/labels/LabelView.tsx` | → `/login` | Label generator | Label generator | |
| `/options` | `features/options/OptionsManager.tsx` | → `/login` | **Not in nav**; direct visit renders a forbidden `Alert` (`errors.forbidden`) | Full manager | Client gate + API RBAC. |
| `/settings` | `features/settings/SettingsView.tsx` | → `/login` | **Not in nav**; forbidden `Alert` on direct visit | Full settings | Client gate + API RBAC. |
| `/users` | `features/users/UsersView.tsx` | → `/login` | **Not in nav**; forbidden `Alert` on direct visit | User admin | Client gate + API RBAC. |
| `/audit` | `app/pages/Placeholder.tsx` | → `/login` | Placeholder | Placeholder | Intentional placeholder (audit log is write-only; post-v1.0.0). |
| `/about` | `features/about/About.tsx` | → `/login` | Version/licenses | Version/licenses | |
| `/change-password` | `app/pages/ChangePasswordPage.tsx` | → `/login` | Reachable; forced when `must_change_password` | Reachable; forced when `must_change_password` | |
| `*` (unmatched) | `app/pages/NotFoundPage.tsx` | → `/login` | `EmptyState` + link home | `EmptyState` + link home | Example URL in §5.3. |

### 2.1 Behavioral rules

- **Anonymous redirect:** `RequireAuth` shows `FullPageSpinner` while the session
  is restoring (`status === 'loading'`). When anonymous it renders
  `<Navigate to="/login" replace state={{ from: pathname + search }} />`, so the
  user returns to the originally requested page after logging in.
- **Forced password change:** a user with `must_change_password === true` is
  redirected to `/change-password` from any other route (all roles). It applies
  to `admin`, `editor` and `viewer`. `/change-password` itself is not
  re-redirected.
- **Admin-only navigation:** `AppShell.NAV_ITEMS` marks `/options`, `/settings`
  and `/users` with `adminOnly: true`; they are filtered out for non-admins.
- **Navigation filtering vs. authorization:** hiding a nav item is **not** a
  security boundary. The admin screens additionally render a forbidden `Alert`
  for non-admins (client-side UX), and the API enforces RBAC on every request
  regardless of what the client renders. Direct navigation to a hidden route
  yields the forbidden state, not a redirect.

---

## 3. Locale parity

`npm run check:i18n` passes with **237 keys** and `en -> cs, it` parity, both
before and after Phase 1. Phase 1 introduces **no new user-visible or accessible
strings**, so `en.json`, `cs.json` and `it.json` are unchanged.

---

## 4. Selector contract (plan §9.3) — must keep passing

The Playwright suite in `web/e2e/scan-bulk.spec.ts` relies on these selectors and
strings. They are an explicit frozen contract and were preserved by Phase 1:

- **Buttons by accessible name:** `Start camera`, `Stop camera`,
  `Add to buffer`, `Apply to 1 item(s)`, `Confirm`, `Undo`, `Download SVG`,
  `Download PNG`.
- **Labels:** `Enter code manually`, `Apply status`, `Tape width`.
- **Text:** `Scanning…`, `Matched: PV-001`, `Test Camera`,
  `Updated 1 item(s).`, `Bulk update undone.`
- **Locator:** `#label-print-area svg`.

Phase 1 adds assertions **without changing any existing selector**: the new
tests reuse `useEnglish`, `mockApi`, `installFakeCamera` and the same
button/label locators above.

### 4.1 Existing unit-test coverage

- `web/src/lib/scanner/scanner.test.ts` — **17 Vitest tests** covering the
  scanner state machine / helpers, unchanged in Phase 1. The full
  `npm run test` run is **21 passed** (17 scanner + 4 new control-token tests).

---

## 5. Screenshot / visual baseline procedure

> **Status: instructions only — not executed for this baseline.** No screenshots
> were captured or committed. This section documents exactly how a future
> implementer reproduces them locally.

Screenshots are deliberately **not** committed (they are large binaries and
would drift). Capture them into a local, untracked directory and keep them out
of git.

### 5.1 Prerequisites (from the repository README, "Development"/"Quick start")

- Go **1.23+** (backend) and Node **20+** (frontend).

- **Option A — Docker (recommended, mirrors production).** Follow the README
  quick start: `cp .env.example .env`, set at least `SKLAD_ADMIN_PASSWORD` and a
  real `SKLAD_COOKIE_SECRET`, then `docker compose up -d --build` and open the
  app. Because the camera API requires a secure origin, trust the local CA per
  the README section "Trusting the local CA (required for camera access)" and
  [`docs/TLS-CAMERA.md`](../TLS-CAMERA.md).

- **Option B — local dev server.** The Go binary reads configuration from the
  process environment (`server/internal/config/config.go`; it does **not**
  auto-load `.env`), so export the required variables from
  [`docs/contracts/config.md`](../contracts/config.md) first. Then:

  ```bash
  go run ./server/cmd/sklad          # from the repository root, API on :8080

  cd web
  npm install
  npm run dev                        # Vite dev server, proxies /api to :8080
  ```

  Open `http://localhost:5173`. Camera access needs a secure context, so use the
  Docker/TLS path above (or a trusted HTTPS tunnel) when capturing `/scan`.

- Seed representative data (the importer in the README, or create items in the
  UI) so tables, badges, the options manager and the scan buffer are populated.
- Ensure at least three accounts exist: `admin`, `editor` and `viewer`.
- Complete any forced password change **first**, so ordinary authenticated
  captures are not redirected to `/change-password`.

### 5.2 Capture procedure

For each route, viewport and role:

1. Open the app in the browser and open DevTools (Device toolbar / responsive
   mode).
2. Set the UI language to **English** (language switcher in the header).
3. Set the viewport to **1440 × 900** (desktop), reload, and use DevTools'
   **"Capture full size screenshot"**.
4. Set the viewport to **360 × 800** (mobile), reload and capture again.
5. Optionally repeat at **768 × 900** (tablet) to confirm no page-level
   horizontal overflow beyond the intentional contained table scroll.
6. Log out / switch accounts to capture the role-specific states.

### 5.3 What to capture

| Capture | Route | Session / state |
|---|---|---|
| Login | `/login` | Anonymous (logged out) |
| Inventory | `/` | Admin, populated table |
| Scan | `/scan` | Admin, buffer populated (e.g. scan `PV-001`) |
| Labels | `/labels?tag=PV-001` | Admin |
| Options | `/options` | Admin |
| Settings | `/settings` | Admin |
| Users | `/users` | Admin |
| Audit placeholder | `/audit` | Admin |
| About | `/about` | Admin |
| Change password | `/change-password` | Logged-in user with `must_change_password` |
| Not found | `/definitely-not-a-route` | Authenticated (a concrete unmatched URL) |
| Forbidden screens | `/options`, `/settings`, `/users` | **Editor** and **viewer** (expect the forbidden `Alert`) |

Because Phase 1 does not switch the global canvas, the shell is still the dark
`bg-ink` canvas; the Phase 2 shell migration is what will change the background
and white-on-dark headings, so capture the "before" set before Phase 2 starts.

### 5.4 Suggested filenames and output directory

Write to a local, untracked directory (do **not** commit):

```text
docs/ui-overhaul/screenshots/        # add to .gitignore; not committed
  phase-1-before/
    {role}/{route}-{width}x{height}.png
  phase-1-after/
    {role}/{route}-{width}x{height}.png
```

Example names:
`admin/inventory-1440x900.png`, `admin/inventory-360x800.png`,
`anonymous/login-1440x900.png`, `viewer/options-1440x900.png`.

If you prefer to keep the working tree clean, capture into a system temp
directory (e.g. `/tmp/sklad-ui-baseline/`) instead.

### 5.5 Playwright screenshots are not a substitute

The Playwright suite runs against the Vite dev server with **all `**/api/v1/**`
requests mocked** and the camera replaced by a fake `getUserMedia` fixture — **no
Go server is required**. It therefore:

- does not exercise real API data, real roles or real authentication;
- captures a screenshot only for a **failing** test
  (`screenshot: 'only-on-failure'` in `web/playwright.config.ts`);
- covers only `/scan` and `/labels` (plus the new Phase 1 assertions), **not**
  every route.

Failure screenshots and the `6`/`18` passing executions are useful as a
functional baseline, but they are **not** a complete visual baseline. Use the
procedure in §5.2–§5.4 for that.

---

## 6. Manual verification (Phase 1 foundation)

**Performed 2026-10-13** against the Vite dev server with the mocked API on
Playwright `1.63.0` / Chromium `153.0.8010.12`, using two independent methods:

1. **Visual observation.** Real browser screenshots were captured at desktop
   **1440×900** and mobile **360×800** with Playwright `page.screenshot` into a
   local `/tmp/sklad-manual/` directory (not committed, per the plan) and were
   then visually inspected. The “Visual observation” column records what is
   actually rendered on screen — the focus indicator being genuinely visible,
   the control borders, and the unchanged canvas — rather than only CSS values.
2. **Committed regression assertions.** `web/e2e/scan-bulk.spec.ts` encodes the
   same checks using **real Tab navigation** (`tabToTest` presses Tab until the
   element is `document.activeElement`) plus computed-style assertions. All pass
   on both `chromium` and `mobile-chrome`.

| Check | Route / element | Visual observation (screenshot) | Computed/asserted value |
|---|---|---|---|
| Keyboard focus — sortable header | `/`, “Item name” header reached by Tab | A teal **2px outline with a 2px gap** is clearly visible around the header button | `outline: solid 2px rgb(11, 98, 135)`, `outline-offset: 2px` |
| Keyboard focus — glyph action | `/`, row “Edit” glyph reached by Tab | A teal 2px rounded outline is clearly visible around the pencil action | same as above |
| Keyboard focus — colour swatch | `/options`, `#93c47d` swatch reached by Tab | A teal 2px outline is visible around the green swatch (in addition to its selected ring) | same as above |
| Primary button | `/scan`, “Start camera” | Dark-teal fill with white text, clearly legible | `background-color: rgb(11, 98, 135)`; hover `rgb(13, 80, 110)` |
| Normal `Input` | `/scan`, “Enter code manually” | Neutral grey-blue border (~`#64748b`), 6px radius, white fill; focused teal ring | `border-radius: 6px`; `border-color: rgb(100, 116, 139)`; focused `box-shadow` contains `rgb(11, 98, 135)`, `0 0 0 2px` (offset) and `0 0 0 4px` (2px ring + 2px offset) |
| Normal `Select` | `/scan`, “Apply status” | Same neutral border and 6px radius as the Input | same radius/border/ring as the Input |
| Invalid `Input`/`Select` | browser-rendered fixture `/e2e/fixtures/invalid-controls.html` | Both controls render with a clearly visible **red border** on white; the focused ring is red | `border-color: rgb(185, 28, 28)`; focused ring `rgb(185, 28, 28)` at 2px/2px |
| Reduced motion | `/about` (fade-in card) and `/` | Page renders identically with no fade-in; transitions are instant | `.animate-fade-in` → `animation-name: none`; `nav a` → `transition-duration: 0s`; spinner `.animate-spin` **not** disabled |
| Canvas, desktop | `/` at 1440×900 | Unchanged dark-teal canvas with light cards | `body` background `rgb(3, 46, 63)`; root `color-scheme: light` |
| Canvas, mobile | `/` at 360×800 | Unchanged dark-teal canvas; nav wraps and no destination is hidden | `body` background `rgb(3, 46, 63)`; root `color-scheme: light` |

Because `invalid` has no in-app caller, its browser rendering is provided by a
committed test fixture (`web/e2e/fixtures/invalid-controls.{html,tsx}`) that
mounts the real components in the invalid state.

Still **not** performed (deferred to later phases / the plan’s full manual
matrix): 200% zoom, Czech/Italian long-label rendering, and a real-backend
per-role walkthrough with real data. The screenshot capture was performed with a
throwaway spec that is not committed; §5 documents the repeatable procedure.

---

## 7. Artifacts captured vs. instructions not executed

**Actually captured (this document):**

- The Before/After gate tables in §1, from real command executions.
- The route/role matrix in §2, read from `router.tsx`, `RequireAuth.tsx` and
  `AppShell.tsx`.
- The selector contract and scanner-test coverage in §4.
- The token values and contrast ratios in §1.3, and the compiled control-token
  rules in §1.4.
- The Phase 1 manual verification observations in §6 (browser screenshots at
  1440×900 and 360×800, plus committed regression assertions).

**Not committed:**

- Screenshots — captured locally for §6 but deliberately **not** committed (per
  the plan). §5 documents how to regenerate them.
- The throwaway screenshot-capture spec used for §6 `rm`’d after the review.
- The remaining plan manual-matrix items (200% zoom, Czech/Italian long labels,
  real-backend per-role visual review) — these belong to later phase gates.

**Deliberately out of scope for Phase 1 (per the plan):** switching the global
canvas, `Card` elevation, `index.html` metadata, `LanguageSwitcher` restyling,
`Alert`/`Badge`/`Spinner` tokenization, and all new shared primitives. Those are
Phase 2+.

---

# UI Overhaul — Phase 2 (coordinated light shell)

**Executed:** 2026-10-13 (UTC) · **Repository revision:** `058b67b8` (working
tree; Phase 1 changes retained). All commands were run individually from `web/`.

Phase 2 switched the app canvas from the dark `bg-ink` shell to the light
`canvas`/`panel`/`content` tokens **in one coordinated change** so no surface was
ever light-on-light or dark-on-dark:

- `index.css` `body` → `bg-canvas text-content`; Phase 1 focus/color-scheme/
  reduced-motion rules untouched.
- `AppShell` → `bg-canvas` root, `bg-panel` header with `border-stroke`, neutral
  nav (`bg-action-selected` active, muted inactive + `hover:bg-panel-subtle`),
  neutral footer, focus-visible skip link to `<main id="main-content" tabIndex={-1}>`.
- `FullPageSpinner` → `bg-canvas text-action`; `Card` → `rounded-lg border
  border-stroke bg-panel text-content` with no shadow; secondary/ghost Button
  variants and `EmptyState` migrated to neutral tokens.
- Every `text-white` heading/footer and dark-only border across Inventory,
  Scan, Labels, Settings, Users, Options, Login, About, ChangePassword and
  Placeholder migrated. Intentional white-on-fill button text and the scanner
  video overlay were preserved.
- `LanguageSwitcher` now renders the shared `Select` (locale handling,
  `<html lang>` sync, `common.language` accessible label and `className` kept);
  its `!h-9 !w-auto` important modifiers are required because `cn` does not
  resolve conflicting utilities.
- `index.html` → `color-scheme: light`, `theme-color #f8fafc`.
- Added the `common.skipToContent` key to all three catalogs → **238 keys**.

## Phase 2 gate

| Command | Result | Key output |
|---|---|---|
| `npm ci` | EXIT 0 | `added 305 packages`; 17 pre-existing vulnerabilities (7 moderate, 8 high, 2 critical); same deprecation/install-script warnings as baseline |
| `npm run typecheck` | EXIT 0 | 0 errors |
| `npm run lint` | EXIT 0 | `0 errors, 9 warnings` — identical set to the baseline (6 refresh, 3 hooks) |
| `npm run check:i18n` | EXIT 0 | `238 keys`, `en -> cs, it` consistent |
| `npm run test` | EXIT 0 | `2 files, 21 passed` (17 scanner + 4 controls) |
| `npm run build` | EXIT 0 | `354 modules transformed`, CSS `20.19 kB`; chunk-size warning only |
| `npx playwright install chromium` | EXIT 0 | (Chromium already present) |
| `npm run test:e2e` | EXIT 0 | `18 passed` — chromium + mobile-chrome |

The two dark-body Playwright assertions were updated to
`rgb(248, 250, 252)` and their tests renamed to describe the light canvas; all
other selectors/focus/contrast assertions were retained.

---

# UI Overhaul — Phase 3 (component consolidation)

Phase 3 added each shared primitive with its first consumer and migrated the
screens in the planned order. All new files live under
`web/src/components/ui/` and are exported from `ui/index.ts` only once adopted:

| Primitive | Responsibility |
|---|---|
| `PageHeader` | one `h1` + optional description/actions; adopted by Inventory, Scan, Settings, Users, Options, Labels |
| `Field` | label/control association + hint/error ids via a render-prop child; adopted by Inventory, ItemEditor, Scan, Bulk, Settings, Users, Options, Labels |
| `Notice` | `Alert` + optional accessible dismiss button (`onDismiss` + required `dismissLabel`); adopted by Inventory, Scan, Bulk, Settings, Users, Options |
| `LoadingState` | `Spinner` + caller-supplied translated status text in `role="status"`; adopted by Inventory, Settings, Users, Options, Labels |
| `IconButton` | `Button` composition that requires an accessible name, enforces `!h-11 !w-11 !p-0` (>=44px) and restores the action outline; adopted by Inventory + Scan + Options |
| `Table` (`TableScroll`, `Table`, `TableHead`, `TableHeader`, `TableCell`) | thin ref-forwarding native wrappers with `aria-sort`; adopted by Inventory, Settings, Users |
| `ConfirmDialog` | controlled `open`/`busy`, `role="dialog"` `aria-modal`, labelled title, focus trap/restoration, Escape-ignored-while-busy, focus-Cancel on destructive, duplicate-submit guard |

Key behaviour changes:

- Inventory's local `ConfirmDialog` and BulkActionPanel's inline modal were both
  replaced by the shared `ConfirmDialog`; both mutation sites now stay open until
  the request settles.
- BulkActionPanel closes the dialog in `apply.onSuccess` **and** `apply.onError`
  (previously it closed before the mutation resolved). Success surfaces Undo and
  the success notice; failure surfaces the existing error notice.
- Destructive archive opens with Cancel focused; on a successful archive the row
  opener may be removed, so the Refresh button is used as the focus fallback. On
  a successful bulk apply the Apply button is disabled by the form reset, so the
  Undo button is used as the focus fallback.
- `SortHeader` now sets `aria-sort` (`ascending`/`descending`/`none`).
- `LabelView` surrounding controls were migrated to `PageHeader`/`Field`/
  `LoadingState`; `PRINT_STYLES`, `#label-print-area`, `TAPE_PRESETS` and the
  rendered SVG geometry were not touched.

## Phase 3 gate

| Command | Result | Key output |
|---|---|---|
| `npm ci` | EXIT 0 | `added 305 packages`; 17 pre-existing vulnerabilities (7 moderate, 8 high, 2 critical); same deprecation/install-script warnings as baseline |
| `npm run typecheck` | EXIT 0 | 0 errors |
| `npm run lint` | EXIT 0 | `0 errors, 9 warnings` — same pre-existing set (no new warnings) |
| `npm run check:i18n` | EXIT 0 | `238 keys`, `en -> cs, it` consistent |
| `npm run test` | EXIT 0 | `3 files, 33 passed` (17 scanner + 4 controls + 12 primitives) |
| `npm run build` | EXIT 0 | `built in ~2.1s`, CSS `20.50 kB`; chunk-size warning only |
| `npx playwright install chromium` | EXIT 0 | Chromium installed/available |
| `npm run test:e2e` | EXIT 0 | `38 passed` — chromium + mobile-chrome |

> Phase 2's table above records the gate as it was executed on the Phase 2 tree
> state (18 E2E executions, `21` unit). The Phase 3 table is the authoritative
> gate for the final tree; both include `npm ci` and the Chromium install.

New unit coverage: `web/src/components/ui/primitives.test.tsx` (12 static-markup
tests) for Field association/ids, Notice roles/dismiss, LoadingState status,
IconButton name/size/outline, native Table + `aria-sort`, and ConfirmDialog
closed/open/busy markup.

New E2E coverage (`web/e2e/scan-bulk.spec.ts`, `phase 3 consolidation`, 11 tests
× 2 projects): light shell + neutral LanguageSwitcher + skip-link focus + locale
`<html lang>` sync + light metadata + 44px icon hit area; active sort toggling
and inline edit/cancel/save; new-entry buffer row field associations; contained
inventory scroll with no page overflow at 360/768/1440px in the display, buffer
**and** inline-editor states (after awaiting readiness); archive dialog
labelled/Cancel-focused/Tab-wrapped/programmatic focus escape pulled
back/Escape-restored; deferred archive response keeps the dialog open, contains
focus on the dialog while pending, disables Cancel/Confirm, ignores Escape and
issues exactly one request; successful archive with a delayed empty refetch
rescues focus to the Refresh fallback; a subsequent user focus move to Search is
not stolen when the refetch later removes the row; deferred bulk success closes
the dialog, focuses Undo and completes the undo; deferred bulk failure keeps the
dialog open, disables actions, guards against a duplicate request, then closes
and restores focus to Apply with the error notice. All use controlled
mocked-response release, not sleeps.

### Focus management (post-review fixes)

The first two Phase 3 revisions failed an independent focus probe. Three defects
were fixed:

1. **Pending-state focus escape.** When `busy` disabled the focused action, focus
   fell to `<body>`. A separate busy-rescue effect and a document `focusin`
   containment listener now pull focus back into the dialog (first enabled
   control, else the dialog itself) whenever it leaves the dialog.
2. **Lost restoration after delayed row removal.** The opener was captured after
   the busy-rescue effect had already moved focus, so it resolved to the dialog's
   Cancel button; declaring the open/restore effect first fixes the capture. On
   close, if the restored opener is later removed (an invalidated list refetch
   dropping the archived row), a `MutationObserver` moves focus to
   `fallbackFocusRef` (Refresh) instead of letting it fall to `<body>`.
3. **Restoration must not steal deliberate focus.** The observer now rescues only
   when the removed opener actually held focus (focus fell to `<body>`); a
   `focusin` watch cancels it as soon as the user moves focus elsewhere. Its
   lifetime is tied to the dialog lifecycle (cancelled on a new open, on external
   focus, and on unmount) rather than an arbitrary timer.

A fourth, separate defect was caught by the strengthened overflow coverage: the
inline-editor/buffer `sr-only` `Field` labels are absolutely positioned and
escaped the scroll container (which had no positioned ancestor), widening the
document. `TableScroll` is now `relative`, so those labels are clipped and the
page no longer overflows in the editor/buffer states.

Reproduced with the reviewer's controlled-release probe after the fixes:
`FOCUS_CONTAINMENT true`, `BUSY_FOCUS { tag: 'DIV', inside: true }`,
`ARCHIVED_ROW_REMOVED_FOCUS { tag: 'BUTTON', refresh: true }`,
`METADATA light #f8fafc`, `LOCALE_SYNC cs PASS`, and the "refetch must not steal
focus from Search" assertion passes (`PASS: refetch did not steal focus`).

## Outstanding manual checks (not performed here)

Per the plan's §8 matrix, these were **not** executed and remain for a manual
pass: 200% zoom; Czech/Italian long-label and long-item-name layouts; a
real-backend per-role walkthrough with real data; and visual confirmation of all
four label presets (6/9/12/18 mm) through a real print preview. The automated
gate covers structure, focus, canvas/metadata, contained overflow and both
dialog lifecycles.

---

# UI Overhaul — Phase 4 (remaining screens & validation)

**Executed:** 2026-10-13 (UTC) · **Repository revision:** `058b67b8` (working
tree; Phases 0–3 retained). All commands were run individually from `web/` so a
failure could not hide a later result.

Phase 4 finished the residual screen polish, made API-supplied colours
contrast-safe, removed the now-dead legacy tokens after a reference check,
completed the automatable part of the plan's §8 matrix, and closed the two
regression gaps an independent verifier had flagged. No route, API, payload,
scanner, print-geometry or exported-helper contract was changed.

## 1. What changed

- **Shared primitives.** `Button` primary/danger/admin variants and `Input`/
  `Select` moved to semantic tokens (`bg-action`, `bg-danger-fg`, `bg-panel`,
  `text-content`, `placeholder:text-content-muted`). `Alert` variants map to the
  `info/success/warning/danger` `fg`/`bg` pairs with `border-stroke`.
  `ConfirmDialog`'s backdrop is `bg-content/40`; its focus/submission behaviour
  is unchanged. `Badge` picks a guaranteed-contrast foreground via the new
  helper and wraps its children in a `min-w-0 break-words` span (with
  `max-w-full`) so long API status labels cannot escape the pill. `Notice` and
  `Alert` bound and wrap their text (`min-w-0`/`break-words`) so a 64-char asset
  tag in a dismissible notice cannot widen the page.
- **`web/src/lib/utils/colorContrast.ts` (new).** `colorContrastTextClass()`
  parses 6-digit and 3-digit hex, computes WCAG 2.1 relative luminance, and
  returns `text-content-inverse` (white) or `text-content-onColor` (black),
  whichever has the higher ratio; unsupported values fall back to `text-content`
  without throwing and without touching API payloads.
- **`web/src/index.css`.** Native date/time inputs are styled through a
  `:where(input[type='date'], input[type='datetime-local'])` base rule so the
  zero-specificity selector cannot mask `invalid`/focus utility classes.
- **Remaining screens.** `Login` and `ChangePasswordPage` now use `Field` and
  `Alert` (with a stable form-error id wired through `aria-describedby`);
  `ChangePasswordPage` uses `PageHeader`. `About` uses `PageHeader`, semantic
  info/`border-stroke` presentation, wrapping/`break-words` rows, and renders
  `LoadingState`/error `Alert` for the version query. `Placeholder` and
  `NotFoundPage` use `PageHeader`/a single styled `Link` (the nested
  Link/Button was removed). `LabelView` gained `min-w-0`/wrapping, an
  `apiErrorMessage` error `Alert` for the search query, an empty-search
  `EmptyState`, and full-opacity semantic error text. `InventoryTable`,
  `ItemEditor`, `OptionsManager` (swatches gain `aria-pressed` + wrapping),
  `SettingsView`, `UsersView` and `ScanView` moved their last ad-hoc colours to
  semantic tokens. `PageHeader` and `AppShell` gained bounded/`min-w-0`/
  `break-words`/truncation layout so long titles and account names cannot push
  the page wide. `OptionsManager`, `SettingsView` and `UsersView` now render
  `apiErrorMessage` error `Alert`s instead of an indefinite “Loading…” or a
  silent empty/default state when their GET fails.
- **`web/features/labels/LabelView.tsx` PRINT_STYLES, `#label-print-area`, the
  inline preview width, `TAPE_PRESETS` (6/9/12/18 mm), `labelTemplates.ts` and
  the rendered SVG generation were not touched.**

## 2. Token reference check and removal decisions

Before deleting any Tailwind key, the runtime source, CSS, config and tests were
searched for `ink`, `surface`, the legacy `status-*` palette, `shadow-card`/
`shadow-soft`, `rounded-card`/`borderRadius.card` and `fade-in`.

| Token | Decision | Reason (post-migration reference check) |
|---|---|---|
| `ink` | **Removed** | 0 remaining utility references (`text-ink`/`bg-ink`/`border-ink` migrated). |
| `surface` | **Removed** | 0 remaining utility references (only prose comments elsewhere). |
| `status` (feas/devel/sales/customer/loaned) | **Removed** | 0 references; the same hex values live in `OptionsManager.PRESET_COLORS` as literals. |
| `boxShadow.card`, `boxShadow.soft` | **Removed** | 0 references; `Card` uses borders. |
| `borderRadius.card` | **Removed** (emptied `borderRadius` container removed) | 0 references. |
| `brand` (50–900) | **Kept** (config) | Retained per the plan even though the `accent-brand-500` checkboxes moved to `accent-action`; API/preset colours remain data. |
| `boxShadow.overlay` | **Kept** | `ConfirmDialog` and the skip link. |
| `keyframes.fade-in` / `animation.fade-in` | **Kept** | `.animate-fade-in` is used by Login/About/ChangePassword/Placeholder and asserted under reduced motion. |
| `content.inverse` / `content.onColor` | **Added** | Guaranteed-contrast foregrounds for arbitrary API colours. |
| API status colours (`item.status_color`, options `statuses[].color`) | **Kept as data** | Not mapped onto intent semantics. |

Compiled-stylesheet verification (`dist/assets/index-*.css`): the new tokens
`content-inverse`, `content-onColor`, `bg-action`, `bg-danger-fg`,
`bg-panel-subtle`, `bg-info-bg`, `bg-success-bg`, `bg-warning-bg`,
`text-danger-fg`, `accent-action` each appear; `text-ink`, `bg-ink`,
`border-ink`, `bg-surface`, `shadow-card`, `shadow-soft`, `rounded-card` and
`status-feas` all appear **0** times.

## 3. Phase 4 gate

| Command | Exit | Key output |
|---|---|---|
| `npm ci` | 0 | `added 305 packages`; 17 pre-existing vulnerabilities (7 moderate, 8 high, 2 critical); same install-script/deprecation warnings as baseline |
| `npm run typecheck` | 0 | 0 errors |
| `npm run lint` | 0 | `0 errors, 9 warnings` — identical pre-existing set (6 `react-refresh`, 3 `react-hooks`) |
| `npm run check:i18n` | 0 | `238 keys`, `en -> cs, it` consistent (no new strings) |
| `npm run test` | 0 | `4 files, 46 passed` (17 scanner + 6 controls + 17 primitives + 6 colorContrast) |
| `npm run build` | 0 | `362 modules transformed`, CSS `21.23 kB`; chunk-size >500 kB warning only |
| `npx playwright install chromium` | 0 | Chromium available/installed |
| `npm run test:e2e` | 0 | `70 passed (24.1s)` — chromium **and** mobile-chrome (35 spec tests × 2 projects) |

Tooling: `node v26.10.0`, `npm 11.19.1`, Playwright `1.63.0`. The suite was run
without `--project`, so both configured projects executed.

New unit coverage: `web/src/lib/utils/colorContrast.test.ts` (6 tests: black/
white selection, the legacy status palette, upper-case/shorthand hex, invalid
inputs, and a 256-step grayscale sweep proving the selected foreground is always
≥ 4.5:1). `controls.test.ts` and `primitives.test.tsx` were extended (semantic
panel/content tokens, invalid date-control markup, neutral/coloured `Badge`,
preserved `Alert` roles/intent tokens).

### 3.1 Verifier follow-ups — two revisions were rejected

Two independent verifier passes rejected earlier Phase 4 revisions and
reproduced failures that the records of the time wrongly claimed were passing.

| Defect | Root cause | Fix | Regression |
|---|---|---|---|
| `/scan` populated buffer overflowed **703 px @360 / 295 px @768** | The unbroken item name was an unconstrained flex child (`min-width:auto` never shrinks below min-content) | `min-w-0` + `break-words` on the name and `break-all` on the code; `min-w-0` on the row | `populated long-content states do not overflow…` fills the buffer with a 128-char unbroken name |
| `/about` overflowed at **all** widths | `overflow-wrap:break-word` does **not** reduce min-content, so the `grid` auto track expanded past `max-w-2xl`; `max-width` cannot clamp a min-content track | `min-w-0` on the definition/list **grid items** | same test returns a 120-char version and 140-char build time |
| `/` overflowed **41 px @360** (Italian, `total:1000`) | The pagination info was a non-wrapping flex group with `min-width:auto` | Pagination stacks on mobile (`flex-col` → `sm:flex-row`), inner group `min-w-0 flex-wrap`, text `break-words`, page-number row `flex-wrap` | same test selects `it` and returns `total:1000` / `total_pages:50` |
| `accent-brand-500` on four checkboxes | Direct product-UI palette reference contrary to §7.2 | `accent-action` (Inventory ×2, BulkActionPanel ×2; Settings already migrated) | grep of `src/` shows no `brand-*` utility; the semantic-color criterion now holds |

**Yes — the second revision also failed.** A further verifier pass reproduced two
more `/scan` overflow paths at 360 px; both are fixed and now covered:

| Defect (2nd revision) | Root cause | Fix | Regression |
|---|---|---|---|
| Dismissible notice overflowed **301 px @360** for a 64-char asset tag | `Notice`'s inner flex text span had `min-width:auto`, so an unbroken tag could not shrink | `min-w-0` on the flex row, `min-w-0 break-words` on the text, `shrink-0` on the dismiss button; `Alert` content/title also `break-words` | `bounds long asset tags and long status labels…` scans `'P'.repeat(64)` and asserts no overflow at 360/768/1440 |
| Buffered status `Badge` overflowed **315 px @360** for an 80-char status label | `inline-flex` `Badge`'s anonymous text flex item could not shrink; the badge box stayed 278 px while its content was 634 px | `Badge` renders `max-w-full` and wraps its children in a `min-w-0 break-words` span (API colour/value unchanged) | same test resolves `PV-001` with a `'S'.repeat(80)` label |
| `OptionsManager` showed “Loading…” forever on a failed GET; `SettingsView`/`UsersView` silently rendered defaults/empty on a failed GET | `!draft` masked the options error, and settings/users had no error branch | each now returns an `apiErrorMessage` error `Alert` before the loading/empty branch | `admin screens surface GET errors instead of loading forever or empty content` mocks 500 for all three |

An **independent probe** (spawned Vite + Playwright, not the committed suite)
measured `scrollWidth − clientWidth` across 11 routes × {360, 768, 1440} px with
long item/account names, a populated scan buffer, long About metadata, long
settings/users content and the Italian locale: **0 px in all 33 combinations**.
A second probe reproduced the two 2nd-revision defects (64-char tag, 80-char
status label) and now reports **0 px** for both at 360 px.

The committed `all authenticated routes…`, `populated long-content states…`,
`bounds long asset tags and long status labels…` and `long item and account
names…` tests cover the empty, populated and long-content states in English and
Italian at all three widths, including 64-char tags and 80-char status labels.

## 4. §8 matrix — automated vs. genuinely performed vs. unperformed

### 4.1 Automated, run in this environment

New `phase 4` Playwright group (16 tests × 2 projects) plus the extended
Phase 1/3 tests:

| §8 area | Automated evidence |
|---|---|
| Responsiveness | Every authenticated route at 360/768/1440 px, after real readiness, reports `scrollWidth − clientWidth ≤ 1`; two further tests load a populated scan buffer, a 64-char asset tag, an 80-char status label, 120/140-char About metadata, a 128-char inventory name, a 60-char account name, and Italian pagination with `total:1000`/`total_pages:50` at all three widths. |
| Contained table scroll | Retained Phase 3 inventory display/buffer/editor overflow assertions. |
| Locales | `en`/`cs`/`it` catalog rendering, `<html lang>` sync, 7 nav destinations for admin, no overflow at 768 px. |
| Roles | Editor and viewer each see 4 nav destinations (admin sees 7); **all six** non-admin × `/options` `/settings` `/users` direct visits show the forbidden alert; anonymous `/` redirects to `/login`. |
| Keyboard | Real Tab reachability with a computed visible focus indicator (outline or 2 px ring) for Search input, sort button, icon Edit, checkbox, scan input/button, settings checkbox/button. |
| Accessible names | Icon actions located by `getByRole('button', { name: 'Edit'/'Delete' })`; 44 px hit area retained from Phase 3. |
| Forms | Login and ChangePassword labels associated; Login error associated to the controls via `aria-describedby` and announced `role="alert"`; ChangePassword mismatch announced; invalid `Input`/`Select`/**date** rendered in-browser with danger border + focused danger ring (proving the `:where()` specificity fix). |
| States | About loading→error, label-search empty and error, admin `/options`/`/settings`/`/users` GET errors (no indefinite “Loading…” or silent empty), plus retained inventory/scan loading/empty/error/success and bulk success/failure. |
| Dialogs | Retained Phase 3 focus-trap/Escape/restoration tests, plus the new independent synchronous double-activation guard test. |
| Reduced motion | Retained Phase 1 reduced-motion test (`.animate-fade-in` → `animation: none`; transitions `0s`). |
| Contrast (normal text) | Login form, primary/danger buttons, muted text, `::placeholder`, an API-coloured badge, and **all four** intent notices (success, info, error, warning) measured in-browser ≥ 4.5:1. |
| Labels | Existing SVG/PNG downloads retained; each tape preset (6/9/12/18 mm) asserts its **exact** frozen `viewBox` for `PV-001` (`0 0 363 64`, `0 0 363 78`, `0 0 363 93`, `0 0 484 163`), a positive rendered box, and an `mm` print-area width. |
| Duplicate submit | `confirm-guard.html` mounts the real `ConfirmDialog` open and enabled; two synchronous `.click()` calls on the same button in one `evaluate` leave it enabled and invoke `onConfirm` exactly once — exercising `submittedRef`, not disabled/`busyRef` suppression. |
| Save payload | `PATCH /items/1` captured with `waitForRequest`; asserts the full unchanged body (`version:1`, edited `name`, `sn`, `type`, `status`, `sent_date:''`, `return_date:''`, `note:''`), the saved cell, the persisted value after an explicitly-awaited `GET /items` refetch, and reopening the editor with the saved value. |

### 4.2 Computed contrast spot-checks (WCAG 2.1 relative-luminance formula)

| Pair | Ratio | Requirement |
|---|---|---|
| white on `action` `#0b6287` (primary) | 6.75:1 | ≥ 4.5:1 normal |
| white on `danger-fg` `#b91c1c` (danger button) | 6.47:1 | ≥ 4.5:1 normal |
| `content` `#0f172a` on `panel` | 17.85:1 | ≥ 4.5:1 normal |
| `content-muted` `#475569` on `panel` (also `::placeholder`) | 7.58:1 | ≥ 4.5:1 normal |
| `success-fg` on `success-bg` | 6.81:1 | ≥ 4.5:1 |
| `warning-fg` on `warning-bg` | 6.84:1 | ≥ 4.5:1 |
| `danger-fg` on `danger-bg` | 5.91:1 | ≥ 4.5:1 |
| `info-fg` on `info-bg` | 6.18:1 | ≥ 4.5:1 |
| black on mid-luminance API `#808080` | 5.32:1 | ≥ 4.5:1 |
| black on legacy `#93c47d` / `#ffd966` / `#e06666` | 10.43 / 15.37 / 6.26:1 | ≥ 4.5:1 |

The 256-step grayscale sweep in `colorContrast.test.ts` proves the selected
foreground is ≥ 4.5:1 for every luminance, so arbitrary valid API colours cannot
produce illegible badge text.

### 4.3 Genuine visual inspection

**None performed in this environment.** All Phase 4 checks above are automated
computed-style / DOM assertions; no screenshots were captured or human-reviewed
for this phase, so no visual claim is made. The Phase 1 §6 screenshot procedure
remains the way to obtain that evidence.

### 4.4 Unperformed (documented, not claimed)

These §8 items were **not** executed and remain for a manual pass:

- Real-backend per-role walkthrough with real data (the suite mocks
  `**/api/v1/**`; persisted mock state proves frontend refetch behaviour, not
  database persistence).
- Human print preview of all four tape presets (6/9/12/18 mm); Phase 4 verified
  geometry programmatically only.
- 200% browser zoom reflow.
- Human visual review of the migrated screens against the design system.

Phases 0–3 remain applied and were not reverted. After the §3.1 fixes, every
**automatable** §7.2 criterion passes — semantic colours only (no `brand-*`
utility references remain), no page-level overflow at 360/768/1440 px in empty,
populated, long-name, long-asset-tag, long-status-label and Italian-pagination
states, all-four-intent contrast, keyboard focus, form association, admin GET
error states, and both regression gaps — and the full prescribed gate is green on
both Playwright projects (`70 passed`). The unperformed human/backend checks in
§4.3–§4.4 are the only outstanding items. Two earlier exit claims were incorrect
and are superseded by this record.
