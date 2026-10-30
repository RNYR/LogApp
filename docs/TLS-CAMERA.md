# HTTPS, the local CA and the camera

Sklad scans labels with the device camera. Browsers only expose `getUserMedia`
on a **secure context**, which means a *trusted* HTTPS origin — not a
self-signed certificate you clicked past, and never plain HTTP. An untrusted
certificate therefore does not degrade the app: it disables scanning.

This guide gets `https://sklad.local` to a green padlock on every device, then
lists what to check when the camera still will not open.

> Related: [`DEPLOY-QNAP.md`](DEPLOY-QNAP.md) (deployment) ·
> [`../deploy/certs/README.md`](../deploy/certs/README.md) (certificate modes
> and the `make-certs` helper) · [`LABELS.md`](LABELS.md) (printing labels).

---

## 1. How the certificate is produced

Two modes, both configured in `.env` — see
[`../deploy/certs/README.md`](../deploy/certs/README.md) for the details:

| Mode | Caddyfile | Certificate | Device trust needed |
|---|---|---|---|
| **A. Internal CA** (default) | `deploy/Caddyfile` | issued by *Sklad Local Root CA* (Caddy's internal CA, name set in the Caddyfile) | **yes** — install the root CA on each device |
| **B. BYO certificate** | `deploy/Caddyfile.byo` | your own (real domain + Let's Encrypt, corporate CA, or `scripts/make-certs`) | only if the issuer is not already trusted |

In mode A the certificate covers exactly the names and addresses the edge is
configured for:

```
SKLAD_HOST      = sklad.local     (also the CN)
SKLAD_HOST_ALT  = sklad
SKLAD_LAN_IP    = <nas address>   (an IP SAN — this is what makes https://<ip> work)
```

If an address is missing here, browsers reject it no matter how well trust is
installed. Keep `SKLAD_LAN_IP` accurate and re-create the certificate after a
network change (mode B: re-run `scripts/make-certs`; mode A: remove the
`caddy_data` volume only as a last resort, since that resets the CA).

---

## 2. Get the root certificate

* **Mode A:** `https://sklad.local/ca.crt` (the edge serves it straight from its
  data volume; no login needed). From a shell:
  `curl -k -o sklad-root-ca.crt https://sklad.local/ca.crt`
* **Mode B:** the CA certificate you installed for the edge, e.g.
  `deploy/certs/rootCA.pem` (or nothing at all when your devices already trust
  the issuer).

Sanity check before installing — a certificate, not an HTML error page:

```bash
head -1 sklad-root-ca.crt          # -----BEGIN CERTIFICATE-----
openssl x509 -in sklad-root-ca.crt -noout -subject -dates
# subject=CN = Sklad Local Root CA
```

The fingerprint of the *server* certificate (compare this on a device if you are
unsure what it is talking to):

```bash
openssl s_client -connect sklad.local:443 -servername sklad.local </dev/null 2>/dev/null |
  openssl x509 -noout -subject -issuer -dates -ext subjectAltName
```

---

## 3. Install trust per platform

### iOS / iPadOS (iPhone, iPad) — two steps, both required

1. **Open the CA in Safari**: `https://sklad.local/ca.crt` (Safari, not Chrome —
   other iOS browsers cannot install profiles).
   → *"Profile Downloaded"* → **Settings → General → VPN & Device Management →
   Downloaded Profile → Install** (enter the passcode).
2. **Enable full trust** — this is the step everybody forgets:
   **Settings → General → About → Certificate Trust Settings** → switch on
   *Sklad Local Root CA*.

Camera access then works in Safari and in any iOS browser using WebKit.
iOS asks for camera permission on the first scan; allow it (Settings → Safari →
Camera is the place to re-enable it later).

### Android

1. Download `https://sklad.local/ca.crt` in Chrome (it lands in *Downloads*).
2. **Settings → Security → Encryption & credentials → Install a certificate →
   CA certificate → Install anyway**, then pick the downloaded file
   (`ca.crt`). The exact wording differs per vendor; on Android 11+ the CA
   install lives under *Encryption & credentials*.
3. Chrome and Firefox then treat `https://sklad.local` as trusted.

Notes: Android shows a permanent "network may be monitored" warning for
user-installed CAs (normal). Some apps deliberately ignore user CAs; Chrome and
Firefox do not.

### macOS

```bash
sudo security add-trusted-cert -d -r trustRoot \
  -k /Library/Keychains/System.keychain sklad-root-ca.crt
```

or double-click the file → **Keychain Access** → add it to the **System**
keychain → open it → *Trust* → **When using this certificate: Always Trust**.
Firefox keeps its own store: *Settings → Privacy & Security → Certificates →
View Certificates → Authorities → Import*.

### Windows

```cmd
certutil -addstore -f "ROOT" sklad-root-ca.crt
```

or double-click → **Install Certificate… → Local Machine → Place all
certificates in the following store → Trusted Root Certification Authorities**.
Edge and Chrome use the Windows store; Firefox again has its own.

### Linux workstations

```bash
sudo cp sklad-root-ca.crt /usr/local/share/ca-certificates/sklad-root-ca.crt
sudo update-ca-certificates
```

---

## 4. Verify, then hand it over

```bash
curl --cacert sklad-root-ca.crt https://sklad.local/api/v1/health   # no -k needed
```

In the browser: green padlock, no warning, and **Camera** permission offered
when you open the scanner. Only then distribute the address to colleagues.

| Device / browser | Scan engine used | Notes |
|---|---|---|
| Android — Chrome, Edge | native `BarcodeDetector` | fastest path; torch toggle available |
| Desktop — Chrome, Edge | native `BarcodeDetector` | webcam or USB scanner |
| Desktop — Firefox, Safari | ZXing (WebAssembly/JS) | slightly slower, still continuous |
| iOS / iPadOS — Safari, all browsers | ZXing | iOS has no `BarcodeDetector`; the app loads ZXing automatically |
| Any device with a USB/Bluetooth scanner | keyboard wedge | the scanner types the code and presses Enter |

---

## 5. Troubleshooting the camera

The app names the exact problem on screen — match the message:

| Message | Meaning | Fix |
|---|---|---|
| *Camera requires a secure HTTPS connection.* | you are on HTTP, or the origin is not a secure context | use `https://…` (and `https://…:8443` if you publish a non-standard port) |
| *Camera permission denied…* | permission was refused earlier | browser site settings → Camera → Allow; iOS: Settings → Safari → Camera |
| *The camera is already in use by another app or tab.* | another app/tab holds the camera | close the other tab/app, reload |
| *Camera scanning is not supported by this browser.* | no `BarcodeDetector` and ZXing could not load | use Chrome/Safari/Firefox current version; check that the SPA bundle loaded (a proxy/CDN blocking the WASM asset breaks ZXing) |
| *No camera found on this device.* | no camera exposed to the browser | use a USB/Bluetooth scanner or the manual entry field |
| Nothing happens at all when tapping **Start** | certificate not trusted (no warning may be shown for camera) or the page is not HTTPS | re-check §3, especially the iOS *Certificate Trust Settings* step; then reload |
| Camera works, labels never decode | label geometry/tape (§ [`LABELS.md`](LABELS.md)): too small bars, glare, wrong symbology, damaged label | reprint at 12 mm/18 mm, keep the label flat, increase light, avoid laminate glare |

Extra checks:

* **Untrusted CA cannot be "clicked past" for the camera.** Proceeding through
  the warning gives you the interface but no camera. Install trust.
* **`sklad.local` vs the IP.** Both can work, but only if the certificate covers
  the one you use (`SKLAD_HOST`/`SKLAD_LAN_IP`, §1).
* **Android and `.local`.** Android's mDNS support is patchy — if the name does
  not resolve, use the IP address or a router DNS entry ([`DEPLOY-QNAP.md`](DEPLOY-QNAP.md) §4).
* **HSTS.** While installing trust across a fleet, `SKLAD_HSTS_MAX_AGE=0` in
  `.env` keeps a typo from being pinned by browsers for a year; set it back to
  `31536000` afterwards.
* **Torch / focus.** Phones expose torch through the scan view; autofocus hunts
  on very close labels — hold the label 10–20 cm away and slightly angled.

---

## 6. Renewals and rotation

* **Mode A:** Caddy renews the short-lived server certificates in the background
  and rotates its intermediate automatically; the root CA lasts years. Nothing to
  do, and nothing to re-install, as long as the `caddy_data` volume survives.
* **Mode B with `scripts/make-certs`:** re-run it when the NAS address changes —
  the root CA is reused and only the server certificate is re-issued, so
  devices keep their trust. `--force` rotates the CA as well, which means
  **every device must install trust again**.
* **Renaming the internal CA** (the `pki` block in `deploy/Caddyfile`) creates a
  new root for the same reason: do it before rollout, not after.
