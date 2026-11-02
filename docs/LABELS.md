# Labels with the Brother PT-E300

The PT-E300 is the only supported office printer. It is standalone: the 12 V
connector is for power, and labels cannot be uploaded from Sklad. The Labels tab
is an Admin-only manual preparation guide, not a printer connection.

## Workflow

1. Create or find the equipment record in Inventory.
2. Open Labels and search for the item. Select it to display its exact asset ID.
3. On the E300 press Bar Code, choose CODE128, enable Under # for readable text,
   and prefer Large barcode width where it fits.
4. Confirm, type the exact asset ID (for example `PV-001`), and confirm the barcode.
   Preserve hyphens and leading zeros; do not encode a URL or item name.
5. Print in black on white tape. Use the Labels tab’s Scan link to verify that the
   physical barcode resolves to the intended item before attaching it.

Sklad matches the decoded asset ID. The E300 generates its own barcode artwork.
No image download, browser printing, tape geometry, P-touch Editor integration,
or automatic print-completion tracking is offered. Inventory CSV/JSON exports
remain administrative data exports, not printer inputs. The historical
`label_printed_at` database field is preserved for compatibility but is not shown
as a print confirmation.

If scanning fails, check the exact ID, print quality, barcode width, clear space
around the bars, lighting and focus. Try wider tape when the barcode is clipped.
Physical camera/scanner testing remains necessary. See [camera setup](TLS-CAMERA.md).

### Thin labels on small or round parts

Labels wrapped around lens barrels, cables and similar parts often leave only a
few millimetres of white tape before and after the bars, with the dark part
right next to it. Barcode readers require that blank margin (the *quiet zone*)
and reject such frames. The scanner compensates: when a frame does not decode,
it retries on crops that end inside the white tape margins, which reads labels
the plain decoder misses. To help it:

- Leave as much white tape as practical before and after the bars; the margin
  may wrap around the back of the part.
- On small round parts, a narrower barcode width can read better than Large: the
  shorter code curves less, and the margins stay visible.
- Hold the label across the aiming line and avoid a glare stripe on glossy tape.
- If the live view still struggles, use **Scan from photo** — the native camera
  takes a sharper picture.

## Sources (verified 2026-10-18)

- [Brother PT-E300 product details](https://www.brother-usa.com/p/mobile-label-printers/PTE300)
- [Brother user manual](https://download.brother.com/welcome/docp000765/pte300_vneng_usr_laf904001.pdf), printed pp.14 and 28–29.

Earlier instructions describing USB/P-touch Editor transfers to the E300 were
incorrect and are superseded by this guide.
