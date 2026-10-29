# Sklad — UI Overhaul Plan

**Status:** proposal (documentation only; no runtime changes in this deliverable)
**Scope:** `web/` SPA (React 18 + TypeScript + Vite + Tailwind CSS 3)
**Audience:** implementers of the follow-up tasks; reviewers of this plan.

This document audits the current web UI, states concrete problems, defines a
minimal design-system foundation, maps the component-level refactor, and phases
the work. Throughout, **Current** means "verified in the repository today" and
**Proposed** means "not yet implemented; a recommendation for a later task".

---

## 1. Scope, constraints and non-goals

### 1.1 How the UI is layered

```
server/cmd/sklad  ──serves──▶  web/dist  (go:embed all:dist, web/embed.go)
                                   ▲
                            `npm run build` (vite build)

web/src/main.tsx
  └─ App.tsx              QueryClientProvider → SessionProvider → RouterProvider
       └─ app/router.tsx  route table (createBrowserRouter)
            └─ RequireAuth (lib/auth/RequireAuth.tsx)
                 └─ AppShell (components/layout/AppShell.tsx)   ← nav, header, footer, <Outlet/>
                      └─ feature screens (features/**/*)
                           └─ shared primitives (components/ui/*)
```

- The Go binary embeds the built SPA (`web/dist`) and serves it; the frontend is
  a client-side SPA with no server-side rendering.
- React Query owns server state; `SessionProvider` owns the session/role.
  Navigation is filtered by role in `AppShell` (`NAV_ITEMS`, `adminOnly`).
- Tailwind utility classes are the **primary** styling mechanism; `src/index.css`
  holds a small `@layer base`. Two exceptions exist: runtime-supplied inline
  styles (API status colors in `Badge`/`InventoryTable`, and the label preview
  width in `LabelView`) and the `LabelView.PRINT_STYLES` `<style>` block used for
  printing. There is no component library, no CSS Modules, and no icon package.

### 1.2 Constraints (must stay true across the whole overhaul)

- **No functional changes.** Routes, role-based navigation, API calls and
  payloads, scanner lifecycle (camera/HID/manual), inventory editing and the
  new-entry buffer, bulk apply/undo grouping, label generation/print and all
  existing tests and selector contracts must keep working.
- **No wholesale rewrite.** Migrate incrementally, one primitive/screen at a
  time, behind the existing exported component APIs.
- **Light application shell**, retain teal branding, system font stack.
- **No new dependencies** (no icon package, no headless UI library, no
  component kit), **no font download**, **no Tailwind major-version migration**.
- **Dark mode is out of scope.** The app becomes a light-only UI; remove the
  misleading `dark` declarations rather than implementing them.
- **Mobile table-card replacements are out of scope.** The inventory table keeps
  its deliberate contained horizontal scroll.
- **Label print geometry is frozen.** `PRINT_STYLES`, `#label-print-area`,
  `TAPE_PRESETS` (6/9/12/18 mm) and rendered SVG dimensions must not change.
- Any **new user-visible or accessible string** must be added to all three
  catalogs (`en.json`, `cs.json`, `it.json`) with key and `{{placeholder}}`
  parity (`npm run check:i18n`).

### 1.3 Preserved contracts (do not break)

- The Playwright selector contract in `web/e2e/scan-bulk.spec.ts` (listed in
  §9.3).
- The scanner unit tests in `web/src/lib/scanner/scanner.test.ts` (17 tests).
- Exported component APIs/types in `web/src/components/ui/index.ts`, including
  `forwardRef` on `Button`/`Input`/`Select`, `Button` variants/sizes/`loading`,
  `Input`/`Select` `invalid` and native prop passthrough, `CardHeader`/
  `CardTitle`, `FullPageSpinner`, and `Alert variant`/`title`.
- Exported pure helpers imported by screens/tests: `buildBulkPatch`,
  `buildUndoGroups`, `emptyEditableItem`/`toEditableItem`/`editableToInput`/
  `editableToPatch`, and the `labelTemplates.ts` exports.

---

## 2. Current UI audit

### 2.1 Screens and routes (`web/src/app/router.tsx`)

| Route | Component file | Access | Purpose / notes |
|---|---|---|---|
| `/login` | `features/auth/Login.tsx` | public | Login form; dark full-screen canvas, single `Card`. |
| `/` (index) | `features/inventory/InventoryTable.tsx` (with `ItemEditor.tsx`) | auth | Main inventory table: filters, sort, pagination, inline editor, new-entry buffer, archive confirm. |
| `/scan` | `features/scan/ScanView.tsx` (with `features/bulk/BulkActionPanel.tsx`) | auth | Camera/HID/manual scanning → scan buffer → transactional bulk actions + undo. |
| `/labels` | `features/labels/LabelView.tsx` (`+ labelTemplates.ts`) | auth | Tape selection, CODE128 preview, print, SVG/PNG download. |
| `/options` | `features/options/OptionsManager.tsx` | auth, admin nav only | Type/status labels + status color editing. |
| `/settings` | `features/settings/SettingsView.tsx` | auth, admin nav only | Discord webhook, scheduler, default locale, org name, delivery log. |
| `/users` | `features/users/UsersView.tsx` | auth, admin nav only | User list, create, role change, deactivate. |
| `/audit` | `app/pages/Placeholder.tsx` | auth | **Intentional placeholder** (audit log is write-only; post-v1.0.0). Not an overhaul target. |
| `/about` | `features/about/About.tsx` | auth | Version/build info, third-party licenses. |
| `/change-password` | `app/pages/ChangePasswordPage.tsx` | auth | Forced when `must_change_password`; also reachable directly. |
| `*` | `app/pages/NotFoundPage.tsx` | auth | `EmptyState` + link home. |

All authenticated routes are nested under `RequireAuth` → `AppShell`.
`RequireAuth` renders `FullPageSpinner` while the session restores, redirects
anonymous users to `/login`, and forces `must_change_password` users to
`/change-password`.

**Admin-only navigation** (`AppShell.NAV_ITEMS`: `/options`, `/settings`,
`/users` with `adminOnly: true`) is the only client-side gating; each screen also
renders a forbidden `Alert` for non-admins and the API enforces RBAC.

### 2.2 Shared UI component inventory (`web/src/components/ui/index.ts`)

| Export | File | Current styling highlights |
|---|---|---|
| `Button` (`ButtonVariant`, `ButtonSize`) | `Button.tsx` | `rounded-lg`, `h-8/h-10/h-12`; `primary` = `bg-brand-500 text-white`; `secondary` = `border-black/10 bg-white`; `ghost`; `danger` = `red-600`; `admin` = `bg-ink text-white`; ring `focus-visible:ring-2 ring-offset-2`. |
| `Card`, `CardHeader`, `CardTitle` | `Card.tsx` | `rounded-card bg-surface p-6 shadow-card`; header `mb-4 flex justify-between`. |
| `Input` | `Input.tsx` | `h-10 w-full rounded-lg border bg-white px-3 text-sm text-ink`; `invalid` → red border/ring; focus ring `brand-300/30`. |
| `Select` | `Select.tsx` | Same base as `Input`; native `<select>`. |
| `Badge` | `Badge.tsx` | Pill; when `color` prop present applies it as inline `backgroundColor` and forces `text-ink`. |
| `Alert` (`AlertVariant`) | `Alert.tsx` | `info/success/warning/error`; `role="alert"` for error else `role="status"`. |
| `EmptyState` | `EmptyState.tsx` | Centered title/description/action, `text-ink`/`text-ink/60`. |
| `Spinner`, `FullPageSpinner` | `Spinner.tsx` | SVG spinner; full-page is `bg-ink` with `text-brand-300`. |

There is **no** shared `PageHeader`, `Field`, `Notice`, `LoadingState`,
`ConfirmDialog`, `Table`, or `IconButton` today; those patterns are duplicated
inline (see §3).

### 2.3 Styling foundations

**`web/tailwind.config.js`**

- `colors.brand` 50–900 (teal): `#eaf7fd, #cfeefb, #a4dff6, #6ccbee, #33b3e0,
  #0a99d1, #0a7ba8, #0b6287, #0d506e, #032e3f`.
- `colors.ink: '#032e3f'` and `colors.surface: '#f7fafd'` — a legacy pair.
- `colors.status`: `feas #93c47d`, `devel #8faadc`, `sales #cc99ff`,
  `customer #ffd966`, `loaned #e06666` (legacy status palette).
- `fontFamily.sans` starts with `Montserrat`, which is **never loaded** (no
  `<link>`/`@font-face` in `index.html` or `index.css`), so the browser silently
  falls back to the rest of the stack.
- `boxShadow.card: '0 20px 40px rgba(0,0,0,0.4)'` and `boxShadow.soft`, plus
  `borderRadius.card: '1rem'` and a `fade-in` keyframe/animation.
- **No semantic token layer** (canvas/surface/text/border/action/intent).

**`web/src/index.css`**

- `:root { color-scheme: light dark; }` — declares dark support that does not
  exist (only the shell is dark; cards are light).
- `body { @apply bg-ink font-sans text-ink }` — the same `ink` token is used
  for both background and text.
- Date/time inputs get `h-10 rounded-lg border border-black/10 bg-white`.
- `:focus-visible { outline: none; }` — **global focus outline suppression**.

**`web/index.html`**

- `<meta name="color-scheme" content="light dark">`, `theme-color #032e3f`
  (dark teal). Mismatched with a light shell.

**`AppShell.tsx`**

- Root `bg-ink`; header `border-white/10 bg-ink/95 backdrop-blur`; nav links
  `text-white/75` with active `bg-brand-500 text-white`; admin items filtered by
  role; footer `text-white/50`; content `max-w-7xl px-4 py-6`.

**`LanguageSwitcher.tsx`**

- Native `<select>` hardcoded for the dark header: `border-white/20 bg-white/10
  text-white`, `h-9`, its own focus ring, `<option class="text-ink">`.

**Baseline (run 2026-09-30, this working copy)**

- `npm run typecheck` → pass (0 errors).
- `npm run lint` → 0 errors, 9 warnings (`react-refresh/only-export-components`,
  `react-hooks/exhaustive-deps`).
- `npm run check:i18n` → pass, 237 keys, en→cs/it consistent.
- `npm run test` → pass, 1 file, 17 tests (`scanner.test.ts`).

---

## 3. Problems and priorities

Priority: **P0** = fix early (blocks clarity/consistency), **P1** = high user
impact, **P2** = polish.

| # | Problem | Evidence (file) | User impact | Proposed remedy | Priority |
|---|---|---|---|---|---|
| 1 | **`ink` is overloaded as both background and text.** Body is `bg-ink` (dark) while cards are light `surface`; `text-ink` on the dark body is near-invisible, so every screen hand-writes `text-white` headings. `admin` button is `bg-ink text-white`. | `index.css` (`bg-ink text-ink`), `tailwind.config.js` (`ink`), `InventoryTable.tsx`/`ScanView.tsx`/`LabelView.tsx`/`SettingsView.tsx`/`UsersView.tsx`/`OptionsManager.tsx` (`text-white` h1), `Button.tsx` (`admin`) | Text becomes unreadable the moment the shell changes; inconsistent heading colors. | Introduce separate semantic roles (`canvas`, `panel`, `content`) and migrate `ink` to a deprecated alias; coordinate the shell + heading migration together (Phase 2). | P0 |
| 2 | **Global focus outline suppression.** `:focus-visible { outline: none }` removes the default focus indicator globally. | `index.css` | Keyboard users lose focus visibility on controls that don't supply a ring (sortable table headers, glyph action buttons, color swatches, option-color buttons). | Replace with a visible global `:focus-visible` (2px ring, 2px offset) or scope the reset to elements that guarantee their own ring; verify every interactive element. | P0 |
| 3 | **Primary button contrast.** `bg-brand-500 text-white` = **≈3.24:1** (computed, WCAG 2.1 relative-luminance formula) — below the 4.5:1 normal-text threshold at 14px. | `Button.tsx` (`primary`) | Low-vision users struggle to read the main action labels. | Move primary to `brand-700` (`#0b6287`, ≈6.75:1) or `brand-600` (`#0a7ba8`, ≈4.76:1); update hover (`brand-800` ≈8.79:1). | P0 |
| 4 | **Excessive elevation and inconsistent secondary text.** `shadow-card 0 20px 40px rgba(0,0,0,.4)` is heavy for flat app panels; secondary text uses ad-hoc `text-ink/40 /50 /60 /70 /80`. | `tailwind.config.js`, `Card.tsx`; `text-ink/60` etc. across all screens | Visually noisy, "web 2010" feel; muted text has no consistent hierarchy. | Panels get borders, **no shadow**; overlays get the single defined shadow. Standardize on `content.DEFAULT`/`content.muted`. | P1 |
| 5 | **Wrapped navigation + hardcoded dark `LanguageSwitcher`.** Nav uses `order-3 w-full` and `flex-wrap`; the switcher duplicates select styling for the dark header. | `AppShell.tsx`, `LanguageSwitcher.tsx` | On narrow screens destinations can crowd/wrap unpredictably; two select styles. | Wrap nav intentionally in a scrollable/stacked row with **no hidden destinations**; render the switcher with the shared `Select` tokens. | P1 |
| 6 | **Duplicated page scaffolding.** Heading row repeated; dismissible notice (Alert + underline close) repeated; centered loading block repeated; field label wrapper repeated; table styles repeated; **two** confirmation dialogs. | Headings: `InventoryTable.tsx`, `ScanView.tsx`, `LabelView.tsx`, `SettingsView.tsx`, `UsersView.tsx`, `OptionsManager.tsx`. Dismissible notices: `InventoryTable.tsx`, `ScanView.tsx`, `BulkActionPanel.tsx`, `SettingsView.tsx`, `UsersView.tsx`, `OptionsManager.tsx` (`LabelView.tsx` has only a non-dismissible error `Alert`). Loading: `SettingsView.tsx`, `UsersView.tsx`, `OptionsManager.tsx`. Fields: all forms. Tables: `InventoryTable.tsx`, `SettingsView.tsx`, `UsersView.tsx`. Dialogs: `InventoryTable.tsx` (`ConfirmDialog`) and `BulkActionPanel.tsx` (inline modal). | Inconsistent spacing/labels/a11y; dialog behavior differs (BulkActionPanel closes before the async mutation resolves). | Add shared `PageHeader`, `Notice`, `LoadingState`, `Field`, `Table`, `ConfirmDialog` and adopt per screen. | P0/P1 |
| 7 | **Dense inventory tables and inline editors.** 9 columns at `min-w-[900px]` with deliberate scroll; sort indicator is a `▲/▼` glyph that is `aria-hidden` and the `<th>` carries no `aria-sort`. | `InventoryTable.tsx` (`CELL`, `min-w-[900px]`, `SortHeader`) | Horizontal scrolling is intended; assistive tech gets no sort state. | Keep native table + contained scroll; add `aria-sort` and a non-glyph sort affordance; standardize cell padding and header styles via `Table`. | P1 |
| 8 | **Glyph-only actions with small hit areas.** Raw `p-1` (~24px) glyph buttons fall below the 44px touch target; larger removals reuse a shared `Button size="sm"` (`h-8` = 32px) with a `×` child. Labels come from `aria-label`/`title`. | Raw `p-1` glyphs: `InventoryTable.tsx` (`ItemRow`, buffer remove), `ScanView.tsx` (buffer remove). Shared `Button size="sm"` glyph child: `OptionsManager.tsx` (remove) | Touch users miss targets; accessible-name consistency depends on each author. | Add `IconButton` (composes `Button`) that **requires** an accessible name and enforces a larger hit area; keep icons decorative. | P1 |
| 9 | **`Badge` accepts API colors without contrast guarantees.** With `color`, foreground is forced to `text-ink` on an arbitrary `backgroundColor`; the same raw colors are also used as inline backgrounds in `ItemRow` and swatches in `OptionsManager`. | `Badge.tsx`; `InventoryTable.tsx` (`ItemRow` status span); `OptionsManager.tsx` (`ColorField`) | A dark custom status color yields unreadable text. | Keep API colors as **data**, not as notification semantics; compute/limit foreground (e.g., pick dark/light text from luminance) or add a neutral badge variant; validate in the options editor. | P1 |
| 10 | **No consistent empty/loading/error convention.** Errors are sometimes `Alert`, sometimes raw `<p role="alert">`; loading is a full-page spinner, an inline spinner, or a bare table-row spinner with no text. | `Login.tsx`/`ChangePasswordPage.tsx` (`<p role="alert">`), `InventoryTable.tsx` (row spinner, `EmptyState`, `Alert`), `SettingsView.tsx`/`UsersView.tsx`/`OptionsManager.tsx` (bare `Spinner`) | Screen readers announce little or nothing while loading; error styling varies. | Adopt `LoadingState` (with caller-supplied translated text), `Alert` for errors, and `EmptyState` for empty; document the convention. | P1 |
| 11 | **Dark-canvas leftovers and misleading metadata.** `FullPageSpinner`, `Login` and `About` footer text assume a dark canvas (`bg-ink`, `text-white/50`). `color-scheme: light dark` and `theme-color #032e3f` claim dark support. | `Spinner.tsx`, `Login.tsx`, `About.tsx`, `index.html`, `index.css` | After a light-shell change these become low-contrast; metadata misleads browsers/OS chrome. | Migrate together in Phase 2; set `color-scheme: light` and a light `theme-color`. | P1 |
| 12 | **Missing skip link / main target.** No skip-to-content link; `<main>` has no `id`. | `AppShell.tsx` | Keyboard/AT users must tab the nav on every page. | Add a skip link targeting `#main-content` (visually hidden until focused). | P2 |
| 13 | **Brand palette partly unused / no semantic layer.** `brand` scale exists but screens reach for raw `white`, `black/10`, `red-600`, and the solid intent backgrounds `emerald-50`/`amber-50` in `Alert`; no semantic tokens. | `tailwind.config.js`, all screens | Hard to keep states consistent; every screen re-decides colors. | Add semantic tokens (§4); keep `brand` and legacy aliases; remove unused legacy status tokens only after a usage check. | P1 |

> Contrast figures above are computed from the hex values with the WCAG 2.1
> relative-luminance formula and should be re-verified with a contrast checker
> during Phase 1. API-provided status colors are **data**, independent of the
> semantic notification palette.

---

## 4. Design-system foundation (Proposed)

Target files: `web/tailwind.config.js` (tokens) and `web/src/index.css` (base
layer only). Consume tokens through Tailwind utilities / `@apply`; do **not**
duplicate hex constants per screen.

### 4.1 Semantic color tokens

| Token | Value | Contrast / role |
|---|---|---|
| `canvas` | `#f8fafc` | App background. |
| `panel.DEFAULT` | `#ffffff` | Cards/panels. |
| `panel.subtle` | `#f1f5f9` | Inset/table header rows. |
| `content.DEFAULT` | `#0f172a` | Primary text (≈17:1 on canvas). |
| `content.muted` | `#475569` | Secondary text (≈7.2:1 on canvas). |
| `stroke.DEFAULT` | `#cbd5e1` | Dividers/borders. |
| `stroke.control` | `#64748b` | Interactive control borders. |
| `action.DEFAULT` | `#0b6287` (brand-700) | Primary action/focus; white text ≈6.75:1. |
| `action.hover` | `#0d506e` (brand-800) | Hover; white ≈8.79:1. |
| `action.selected` | `#eaf7fd` (brand-50) | Selected nav/row background. |
| `success.fg` / `success.bg` | `#166534` / `#f0fdf4` | ≈6.8:1 pair. |
| `warning.fg` / `warning.bg` | `#92400e` / `#fffbeb` | ≈6.8:1 pair. |
| `danger.fg` / `danger.bg` | `#b91c1c` / `#fef2f2` | ≈5.9:1 pair. |
| `info.fg` / `info.bg` | `#0b6287` / `#eaf7fd` | ≈6.2:1 pair. |

Semantic names to add: `canvas`, `panel.DEFAULT`, `panel.subtle`,
`content.DEFAULT`, `content.muted`, `stroke.DEFAULT`, `stroke.control`,
`action.DEFAULT`, `action.hover`, `action.selected`, and intent
`{fg,bg}` pairs for `success`/`warning`/`danger`/`info`.

Migration rules:

- Retain `brand` (scale), `ink`, `surface` and the legacy `status` tokens as
  **aliases during migration**; add a code comment marking `ink`/`surface` as
  deprecated. Remove legacy `status` tokens only after a `grep` usage check
  finds no references (a `grep` for `status-feas|status-devel|status-sales|
  status-customer|status-loaned` currently finds **zero** component usages;
  `OptionsManager.PRESET_COLORS` holds the same hex values as literals).
- Keep API status colors (`item.status_color`, options `statuses[].color`)
  strictly separate from semantic notification colors.
- After migration, `ink` should not be used for page backgrounds; it may remain
  as an alias for `content.DEFAULT` until removed.

### 4.2 Typography

- **System font stack** replaces Montserrat-first:
  `ui-sans-serif, system-ui, -apple-system, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif`.
  No webfont download.
- Scale (size/line-height), matching Tailwind defaults:
  - caption **12/16** (`text-xs`)
  - body/control **14/20** (`text-sm`)
  - supporting body **16/24** (`text-base`)
  - section heading **18/28** (`text-lg`)
  - page heading **24/32** (`text-2xl`)
- Page headings use weight 600, `content.DEFAULT`; section headings weight 600.

### 4.3 Spacing, radii, elevation

- Spacing uses Tailwind's existing scale: **4, 8, 12, 16, 24, 32 px**.
- Controls are normally **40px high** (`h-10`); compact table selects may use
  `h-8`.
- Radii: controls **6px** (`rounded-md`), panels/dialogs **8px** (`rounded-lg`),
  badges **pill** (`rounded-full`). Drop `borderRadius.card` (1rem).
- Panels use **borders, no shadow**. Overlays (dialog/dropdown) use one
  restrained shadow: `0 8px 24px rgba(15,23,42,.12)`. Replace `shadow-card` /
  `shadow-soft`.

### 4.4 Focus, motion, color scheme

- Visible **2px focus indicator with 2px offset** for all interactive elements;
  fix the global `:focus-visible` reset (§3, problem 2).
- `color-scheme: light` only; `theme-color` set to a light value (e.g.
  `#f8fafc`).
- Respect `prefers-reduced-motion: reduce`: disable `fade-in`/transition
  animations.

### 4.5 Explicit non-goals for the foundation

No font download, no icon package, no Tailwind major-version migration, no CSS
preprocessor, no new runtime dependency. Tokens stay in Tailwind config; the
`index.css` base layer only normalizes `body`, `:focus-visible`, form controls
and reduced motion.

---

## 5. Component refactor map (Proposed, file-level)

Each item below is **future work**, not part of this document.

### 5.1 Layout shell

- `web/src/components/layout/AppShell.tsx` — `AppShell`
  - Light shell (`bg-canvas`), bounded content width (`max-w-7xl`), subtle
    footer, clearly selected navigation (`action.selected` + `content.DEFAULT`).
  - Wrap mobile navigation without hiding destinations (wrap or horizontally
    scroll the nav row); keep `NAV_ITEMS` role filtering exactly as-is.
  - Add a skip link and give `<main>` `id="main-content"`.
  - Remove dark-only `border-white/10`, `bg-ink/95`, `text-white/*`.

### 5.2 Internationalization

- `web/src/lib/i18n/LanguageSwitcher.tsx` — `LanguageSwitcher`
  - Use neutral shared `Select` styling; keep locale handling, `<html lang>`
    sync, and the accessible label (`common.language`).
  - Keep `LanguageSwitcherProps.className`.

### 5.3 Existing shared primitives (`web/src/components/ui/*.tsx`)

Preserve exported props, refs, variants, native attributes and loading behavior
while migrating visual tokens:

- `Button.tsx` — primary contrast (§3.3), tokenized variants; keep
  `forwardRef`, `loading`, `aria-busy`, size variants.
- `Card.tsx` — `panel` surface + border, no shadow; keep `Card`/`CardHeader`/
  `CardTitle` exports and element types.
- `Input.tsx` / `Select.tsx` — shared control border/focus/validation tokens;
  keep `invalid`/`aria-invalid` and native passthrough.
- `Badge.tsx` — contrast-safe foreground when a color is supplied (or a neutral
  variant); keep the `color` prop signature.
- `Spinner.tsx` — token-consistent color; clarify `Spinner` vs
  `FullPageSpinner` (full-page falls back to canvas, not `bg-ink`); keep both
  exports.
- `Alert.tsx` — map variants to intent `fg/bg`; keep `role` semantics and
  `variant`/`title`.
- `EmptyState.tsx` — tokenized text; keep props.

### 5.4 New shared primitives (Proposed files, added only when adopted)

- `web/src/components/ui/PageHeader.tsx` — title, optional description, optional
  actions; replaces the repeated heading row without changing action order.
- `web/src/components/ui/Field.tsx` — label/control association plus
  hint/error IDs; owns **no** form state.
- `web/src/components/ui/Notice.tsx` — composes `Alert` with an optional
  accessible dismiss button; notice state stays in the feature.
- `web/src/components/ui/LoadingState.tsx` — composes `Spinner` with
  caller-supplied translated status text.
- `web/src/components/ui/ConfirmDialog.tsx` — controlled open/busy state,
  title/body/actions, `role="dialog"` `aria-modal`, labelled title, focus
  containment + restoration, Escape handling, cancel-focused destructive
  confirmation, no duplicate submit while pending. **Escape policy:** Escape
  dismisses (calls `onCancel`) only when not busy; while a destructive mutation
  is pending, Escape is ignored and Cancel is disabled so the request cannot be
  abandoned mid-flight.
- `web/src/components/ui/Table.tsx` — thin native `<table>`/`<thead>`/`<th>`
  wrappers plus a scroll container; **not** a data-grid abstraction; supports
  refs/native props and `aria-sort` semantics.
- `web/src/components/ui/IconButton.tsx` — composes `Button`; **requires** an
  accessible name; icons decorative and dependency-free.
- `web/src/components/ui/index.ts` — export new primitives only when implemented
  and adopted.

### 5.5 Feature screens

- `features/inventory/InventoryTable.tsx` — `InventoryTable`, `SortHeader`,
  `ItemRow`, local `ConfirmDialog`: standardize toolbar (`PageHeader`), tables
  (`Table`, `aria-sort`), pagination, row actions (`IconButton`), notices
  (`Notice`), and confirmation (`ConfirmDialog`). Preserve filtering, debounce,
  sorting, pagination math, buffer mutations, and `EditableItem` flow.
- `features/inventory/ItemEditor.tsx` — `ItemEditor`: standardize inline field
  appearance/labels while preserving the `<tr>` structure and all mapper
  functions (`emptyEditableItem`, `toEditableItem`, `editableToInput`,
  `editableToPatch`).
- `features/scan/ScanView.tsx` and `features/bulk/BulkActionPanel.tsx` —
  standardize scan controls, buffer list, notices and confirmation. Leave the
  scanner lifecycle, `buildBulkPatch`, and `buildUndoGroups` untouched.
- `features/settings/SettingsView.tsx`, `features/users/UsersView.tsx`,
  `features/options/OptionsManager.tsx` — shared headings, fields, tables,
  loading/empty/error states; preserve validation and option-color editing.
- `features/labels/LabelView.tsx` — standardize surrounding controls only;
  preserve `PRINT_STYLES`, `#label-print-area`, rendering, download handlers,
  and physical label dimensions.
- `features/auth/Login.tsx`, `features/about/About.tsx`, and
  `app/pages/ChangePasswordPage.tsx`, `app/pages/NotFoundPage.tsx`,
  `app/pages/Placeholder.tsx` — migrate remaining dark-canvas assumptions,
  form styling, and secondary text.
- `web/index.html` — align `color-scheme`/`theme-color` with the light shell.
- `web/src/locales/en.json`, `cs.json`, `it.json` — only if new accessible text
  is necessary; maintain key and placeholder parity.

---

## 6. Phased roadmap

Estimates are rough, not commitments. Every **implementation** phase exit runs
the gate in §9.1.

### Phase 0 — Baseline & documentation (0.5–1 day, low risk)

- Record before screenshots (per route, desktop + mobile), the route/role
  matrix, current test results (see §2.3), and the selector contract (§9.3).
- **Dependencies:** none. **Exit:** baseline artifacts captured; this plan
  reviewed.

### Phase 1 — Additive foundation (1–2 days, low risk)

- Add semantic tokens to `tailwind.config.js` + base-layer fixes in
  `index.css` (focus indicator, `color-scheme: light`, reduced motion,
  system font stack).
- Fix primary button contrast and shared control focus/validation tokens.
- **Do not** switch the global canvas yet.
- **Dependencies:** Phase 0. **Exit:** gate passes; existing screens still look
  unchanged except focus and primary-button color.

### Phase 2 — Coordinated shell migration (2–3 days, medium risk)

- Switch `body`/`AppShell`/`FullPageSpinner` canvas, `index.html` metadata,
  `LanguageSwitcher`, `Card` elevation, and all white-on-dark headings/footer
  text **together**.
- Migrate `Login` and `About` dark-canvas assumptions in the same change.
- **Dependencies:** Phase 1. **Exit:** no light-on-light or dark-on-dark text;
  gate passes; keyboard/reduced-motion checks pass.

### Phase 3 — Component consolidation (3–5 days, medium/high risk)

- Add each primitive with its first consumer: `PageHeader`, `Field`, `Notice`,
  `LoadingState`, `IconButton`, `Table`, then `ConfirmDialog`.
- Migrate inventory (tables/editor/dialogs), then scan/bulk, then admin screens.
- Dialog and inline-table changes each get separate checks.
- **Dependencies:** Phase 2. **Exit:** no duplicated heading/notice/loading
  patterns remain; both dialog sites share one accessible implementation; gate
  passes.

### Phase 4 — Remaining screens & validation (1–2 days, medium risk)

- Auth/about/fallback/labels polish; accessibility and responsiveness pass
  (§8); remove obsolete styling/tokens only after usage checks.
- **Dependencies:** Phase 3. **Exit:** all acceptance criteria in §7.2 met;
  gate passes.

Keep each commit independently reviewable; **do not combine a visual migration
with a business-logic change** in one commit.

---

## 7. Acceptance criteria

### 7.1 This plan deliverable

- `docs/ui-overhaul-plan.md` exists and covers: current-UI audit, problems,
  design-system foundation, component refactor map, and phased roadmap.
- Findings reference real files and separate **Current** evidence from
  **Proposed** changes.
- All routes (`router.tsx`) and all shared UI modules (`ui/index.ts`) are
  inventoried.
- Semantic tokens have explicit values; proposed primitives have defined
  responsibilities and consumers.
- Phases include effort, risk, dependencies and test gates.
- Existing functionality, localization, selector contracts, and label-print
  constraints are explicitly protected.
- **No runtime/source changes** beyond this document.

### 7.2 Eventual implementation

- Text contrast **≥ 4.5:1** for normal text; **≥ 3:1** for large text and
  necessary interactive/state indicators.
- Keyboard-visible focus on every interactive element.
- Only semantic tokens used for UI colors (no ad-hoc `white`/`black/10`/`red-600`
  for product UI).
- No page-level horizontal overflow at **360, 768, 1440px**, except contained
  table scrolling.
- Role-based navigation, accessible labels, existing routes, table
  sorting/pagination/editing, scan/buffer/bulk/undo, and label exports/printing
  all behave as before.
- Preserved selector contract (§9.3) and scanner unit tests still pass.

---

## 8. Manual verification matrix

| Area | Check |
|---|---|
| Keyboard | Tab through header/nav/content; skip link works; focus always visible; sort/pagination/row actions reachable. |
| Dialogs | Focus contained on open, restored on close; Escape closes when idle but is ignored while a mutation is pending; destructive confirm focuses Cancel; no duplicate submit while pending. |
| Forms | Labels associated, `invalid` sets border/ring + `aria-invalid`; errors announced; hints present. |
| States | Empty, loading (with text), error (retry), and success notices render consistently on each screen. |
| Roles | Admin vs non-admin navigation and forbidden states. |
| Locales | English, Czech, Italian — including long translated labels and long item names. |
| Responsiveness | 360 / 768 / 1440px; contained table scroll only; nav destinations not hidden. |
| Accessibility | 200% zoom; reduced motion; contrast spot-checks. |
| Labels | Print preview for 6/9/12/18 mm presets; SVG/PNG downloads; dimensions unchanged. |
| Visual | Before/after screenshots per phase. Do not add brittle screenshot tests without a committed baseline. |

---

## 9. Regression testing

### 9.1 Phase-gate commands

```sh
cd web
npm ci
npm run typecheck
npm run lint
npm run check:i18n
npm run test
npm run build
npx playwright install chromium
npm run test:e2e
```

`npm run test:e2e` must run **both** Playwright projects defined in
`web/playwright.config.ts`: `chromium` (Desktop Chrome) and `mobile-chrome`
(Pixel 5). Do not narrow the phase gate with `--project`; both were green in the
baseline (`6 passed`). Playwright starts Vite and mocks `**/api/v1/**`; **no Go
server is required** for the existing E2E suite. Record any pre-existing
failures (9 lint warnings today) rather than attributing them to a UI change.

### 9.2 Baseline to preserve

- `npm run test` → `scanner.test.ts` (17 tests). Do not change scanner behavior.
- `npm run check:i18n` → 237 keys, en→cs/it parity.

### 9.3 E2E selector contract (`web/e2e/scan-bulk.spec.ts`) — must keep passing

- Buttons by accessible name: `Start camera`, `Stop camera`, `Add to buffer`,
  `Apply to 1 item(s)`, `Confirm`, `Undo`, `Download SVG`, `Download PNG`.
- Labels: `Enter code manually`, `Apply status`, `Tape width`.
- Text: `Scanning…`, `Matched: PV-001`, `Test Camera`, `Updated 1 item(s).`,
  `Bulk update undone.`
- Locator: `#label-print-area svg`.

### 9.4 Proposed new test (future, not in this deliverable)

- `web/e2e/ui-accessibility.spec.ts` using mocked API responses to assert:
  navigation permissions, keyboard focus, dialog focus containment/restoration
  and Escape, field associations, and contained table scrolling. Add only after
  the corresponding phase lands; do not change existing selectors to add it.

---

## 10. Risks and watch-outs

- **Canvas-only change is unsafe.** Changing the background without migrating
  white headings/footer text at the same time recreates problem 1 in reverse.
  Coordinated in Phase 2.
- **Keep API status colors distinct** from semantic notification colors; do not
  map `item.status_color` onto `danger`/`success`.
- **Shared primitives must not absorb feature state, API logic, or table data
  processing.**
- **Dialog consolidation** introduces focus-management and async-dismissal risk
  (the current bulk dialog closes before its mutation resolves).
- **Preserve native table structure** for inline editing and contained mobile
  scrolling.
- **Global styles must not alter label print geometry** (`@media print` in
  `LabelView.PRINT_STYLES`).
- **New visible/accessible strings** require all three locale catalogs.
- **Do not implement the proposed future files as part of this documentation
  task.**
