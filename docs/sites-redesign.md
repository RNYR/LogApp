# Sklad — original application redesign

The October 2026 Sites revision replaces the simplified standalone demo with a
build of the original `web/` React application. The original route hierarchy,
API client, scanner, and administrative tools remain. The rental workflow and role gates follow the current requirements in `roles-and-functions.md`.

## Design

**PASS — approved by the user on 2026-10-14.** White PEKAT sidebar and content cards, dark backdrop, Work Sans, and refined native dropdowns are the accepted baseline.

Work Sans (bundled locally under SIL OFL), PEKAT-inspired green `#78be5a`,
blue-gray panels and a dark navy `#101820` canvas. Responsive sidebar navigation
preserves Inventory, Scan, Labels, Manage options, Settings, Accounts, and About.
All new application strings are in the English, Czech and Italian catalogs.
Original status colors remain editable application data.

## Original defaults in the preview

Types: Camera, Lens, Adapter, Light, PC, Router, Power Supply, Monitor, Keyboard,
Mice, Cable, Module/Block.

Statuses: Feasibility, Devel, Sales, Customer (label: Customer On-Site), Loaned.
Values match `equipment-log/main.go`; labels can be localized without changing
item references. Existing real-backend options are not replaced or reseeded.

## Real app vs Sites preview

- `cd web && npm run dev` and `npm run build` use the existing Go API.
- `npm run dev -- --mode preview` and `npm run build -- --mode preview` explicitly
  inject `public/preview-mock.js`. Normal builds do not execute the mock API.
- Sites hosts that explicit preview build. Equipment, option, user and settings
  edits persist only in that browser under `sklad-original-preview-v1`.
- Preview username `admin` accepts any test password. Editor is anonymous. This is
  simulated authentication. Passwords and webhook secrets are
  not persisted. Discord actions are simulated and never send messages.
- Camera scanning uses the real frontend implementation. Labels provides manual PT-E300 instructions and exact asset IDs; unsupported artwork, exports and printing controls have been removed. Physical labels require scan validation.
- `/audit` remains the original post-v1 placeholder; no fabricated audit backend
  has been substituted. The original server audit writes remain untouched.
- The public-access revision updates Go route authentication, account restrictions,
  and OpenAPI security declarations. Existing database records and migrations are preserved.

## Small correctness fix

Options usage now checks both active and archived records, following pagination,
so an option referenced by active equipment cannot appear safe to delete. Delete
controls remain disabled while usage is loading or fails. The server continues
its independent reference enforcement.

## Current access and rental workflow — 2026-10-16

Editor is the anonymous default. Public navigation contains Inventory, Scan and About. Admin alone manages equipment identity, labels, archives, exports, personnel, options and settings. The shared buffer supports checkbox and scan selection, individual updates and atomic bulk updates. Feasibility is Admin-confirmed return; rental fields lock in that status. Other statuses require a renter; Customer requires a note naming the customer.

See `roles-and-functions.md` for the permission matrix and `RENTAL-WORKFLOW.md` for repeatable tests and NAS rollout. Legacy non-Admin accounts are disabled, with historical records retained. Sites sharing remains owner-private; anonymous Editor describes application behavior, not hosting audience.

## Verification

Frontend unit, locale parity, desktop/mobile browser, real Go API integration and backend tests cover the rental revision. Camera decoding is simulated in browser tests. Physical scanners, cameras and printers require target-device acceptance testing. The Sites mock is browser-local; the normal build uses the real backend.

## PT-E300-only labels (2026-10-18)

Labels now supports manual device entry only. Barcode image generation, tape sizing, SVG/PNG downloads, browser printing and bwip-js are removed. Inventory data exports remain available to Admin.
