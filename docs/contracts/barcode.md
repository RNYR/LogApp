# Sklad — Barcode Contract (CODE128)

> Status: **FROZEN CONTRACT** (task 1.a).

## Purpose

Every inventory item carries a unique, printable barcode that is scanned with the in-app camera
(laptop / Android / iOS) to resolve the item and add it to the scan buffer for bulk operations.

## Symbology & payload

| Property            | Value                                                        |
|---------------------|--------------------------------------------------------------|
| Symbology           | **Code 128** (ISO/IEC 15417), Code Set **B** (printable ASCII) |
| Payload             | the item's **`asset_tag`**, e.g. `PV-001`                    |
| Human-readable text | `asset_tag` printed directly beneath the bars               |
| Checksum            | Code 128 mandatory modulo-103 check character — always emitted by the encoder, verified by the decoder |
| Case handling       | resolution is **case-insensitive** and trims surrounding whitespace |
| Immutability        | `asset_tag` is immutable after creation (labels are printed against it) |

## Tag scheme

- Format: `PV-` + zero-padded 3+ digits (`PV-001` … `PV-999`, rolling to `PV-1000`+ as needed).
- New tags take the **next free integer**; existing legacy tags (including the `PV-049` gap) are
  preserved verbatim and never resequenced.
- The tag is the single canonical link between the printed label, the DB row, and the scan buffer.

## Encoding (label generation)

- Library: **bwip-js** (`bcid = 'code128'`, `text = asset_tag`) rendered client-side.
- Output: **SVG** (crisp, scalable) and **PNG** (300 DPI) for print/import.
- Print geometry for **Brother P-Touch E300** tapes:

| Tape width | Minimum recommended narrow-bar width | Notes                                   |
|------------|--------------------------------------|------------------------------------------|
| 6 mm       | ≥ 0.20 mm                            | short tags only, tight margins           |
| 9 mm       | ≥ 0.25 mm                            | usable                                   |
| **12 mm**  | **≥ 0.25 mm (recommended)**          | **recommended daily driver**             |
| 18 mm       | ≥ 0.30 mm                            | largest, easiest to scan                 |

- Quiet zone: **≥ 10 ×** the narrow-bar width on both sides (Code 128 spec minimum).
- The label template renders `asset_tag` as text under the bars, uses the default monospace font,
  and is centered.

## Decoding (camera scan)

- Primary: native **`BarcodeDetector`** with `formats: ['code_128']` (Chrome/Android/desktop).
- Fallback: **ZXing** (`@zxing/library` `BrowserMultiFormatReader`, hints restricted to
  `CODE_128`) — required for iOS Safari which lacks `BarcodeDetector`.
- Input pipeline: `getUserMedia` back camera (`facingMode: 'environment'`), `playsinline` on iOS,
  start only from a user gesture, `torch` toggle where supported.
- Normalization before lookup: trim, collapse internal whitespace, uppercase.
- Non-camera fallback: **HID keyboard-wedge** capture (hardware scanners type + `Enter`) and
  **manual entry** field.

## Resolution semantics

1. Normalize scanned value → exact match on `items.asset_tag` (case-insensitive).
2. Match → `matched`, add to buffer (dedupe by `asset_tag`).
3. No match → `unknown`; the UI offers "create draft item" (auto-assign next free tag) or discard.
4. Already in buffer → `duplicate`; keep single entry.
5. Each resolution may be recorded in `scan_events` (`matched` | `unknown` | `duplicate`).

## Acceptance (round-trip)

- Generated label for `PV-001`, decoded by the in-app camera on laptop / Android / iOS, resolves
  to the item with `asset_tag = "PV-001"`.
- Programmatic round-trip: encode → render → decode → equals `asset_tag` for a fixture set
  (including `PV-999`, `PV-1000`, and a value with hyphen `PV-001`).
- Fixture suite: sharp / slight rotation / tilt / low contrast must decode; Code 39, EAN-13 and
  corrupt-checksum samples must be rejected.
