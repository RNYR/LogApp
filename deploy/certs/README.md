# Certificates for the Sklad edge

Everything in this directory except this README is **generated** and gitignored:
private keys must never be committed.

There are two ways to give `https://sklad.local` a certificate the devices
trust. Pick one — the default needs no files here at all.

| | Mode A — internal CA (default) | Mode B — bring your own certificate |
|---|---|---|
| Caddyfile | `deploy/Caddyfile` | `deploy/Caddyfile.byo` |
| Who issues | Caddy, on first HTTPS request | you (or `scripts/make-certs`) |
| Files here | none | `sklad.crt`, `sklad.key`, `rootCA.pem` |
| Use when | normal NAS deployment | real domain + Let's Encrypt, an existing corporate CA, or a CA shared with other services |

Browsers only expose the camera on a **trusted** HTTPS origin, so the CA must be
installed on every phone and laptop that scans. The step-by-step device
instructions live in [`../../docs/TLS-CAMERA.md`](../../docs/TLS-CAMERA.md).

---

## Mode A — Caddy's internal CA (default)

Nothing to generate. Caddy creates its own root CA on first start and keeps it
in its data volume:

```
/data/caddy/pki/authorities/local/root.crt   root certificate (public)
/data/caddy/pki/authorities/local/root.key   root key (never leaves the volume)
```

* Certificates are issued for the addresses in the `deploy/Caddyfile` site line
  (`SKLAD_HOST`, `SKLAD_HOST_ALT`, `SKLAD_LAN_IP`), IP SANs included.
* Install trust from **`https://sklad.local/ca.crt`** — the route serves
  `root.crt` with `Content-Type: application/x-pem-file`. iOS and macOS open it
  as a profile; Android and Windows import it as a CA certificate.
* The trust dialog shows Caddy's built-in CA name. This deployment names it
  **"Sklad Local Root CA"** through the `pki` block in the Caddyfile, so the
  prompt on a phone says what it is. That block also provisions the CA at
  startup, which is why `/ca.crt` works on the very first boot.
* Lifetimes (Caddy defaults): the **root CA lasts 10 years**, the intermediate
  is rotated every 7 days automatically, and server certificates are renewed in
  the background well before they expire. Devices trust the root, so nothing has
  to be re-installed during normal operation.
* **Resetting**: deleting the `caddy_data` volume throws the CA and its
  certificates away. Every device must then trust the new CA again, so prefer a
  leaf-only renewal (Mode B) if you need that level of control.

Verify what the edge is presenting:

```bash
openssl s_client -connect sklad.local:443 -servername sklad.local </dev/null 2>/dev/null |
  openssl x509 -noout -subject -issuer -ext subjectAltName
```

---

## Mode B — your own certificate

### B1. Generated locally with `scripts/make-certs`

```bash
./scripts/make-certs                                   # auto-detects the LAN IP
./scripts/make-certs --ips 192.168.1.50                # explicit NAS address
./scripts/make-certs --hosts sklad.local,sklad,nas.local --ips 192.168.1.50,10.0.0.5
./scripts/make-certs --ca-only                         # trust first, issue later
./scripts/make-certs --force                           # rotate the CA as well
```

`SKLAD_HOST` and `SKLAD_LAN_IP` from the environment are used as SAN defaults, so
the script can share the same `.env` as docker-compose. Run
`./scripts/make-certs --help` for every option.

Generated files (all gitignored):

| File | Mode | Purpose |
|---|---|---|
| `rootCA.pem` | `644` | root CA certificate — install this on devices, and it is what `/ca.crt` serves in this mode |
| `rootCA.key` | `600` | **secret** — needed to re-issue the server certificate; keep it off shared storage |
| `sklad.crt` | `644` | server certificate, full chain (leaf first, then `rootCA.pem`) |
| `sklad.key` | `600` | **secret** — server private key |

The script is safe to re-run:

* the root CA is **reused** unless you pass `--force`, so devices keep their trust;
* the server certificate is only re-issued when it is missing, inside the last
  30 days of its life, or does not cover one of the requested names — which is
  exactly what happens when the NAS gets a new LAN address.

Enable the mode by pointing the compose mount at the BYO Caddyfile:

```bash
# .env
CADDYFILE=deploy/Caddyfile.byo
SKLAD_LAN_IP=192.168.1.50      # must match the certificate SANs
```

```bash
docker compose up -d caddy
```

### B2. Real domain / Let's Encrypt

Put your files here with exactly these names (the Caddyfile reads them from the
container path `/certs`, which is this directory mounted read-only):

* `sklad.crt` — full chain (your certificate **plus** the issuing CA/intermediate)
* `sklad.key` — the matching private key
* `rootCA.pem` — optional; only needed if devices must install trust for `/ca.crt`

Then set `SKLAD_HOST` to the public name, `CADDYFILE=deploy/Caddyfile.byo`, and
renew with your ACME client (DNS-01 works without exposing port 80). Caddy reads
the files from disk, so renewing them in place needs no restart as long as the
paths stay the same.

---

## Addresses and ports

| Variable | Meaning | Default |
|---|---|---|
| `SKLAD_HOST` | primary DNS name (certificate CN and first SAN) | `sklad.local` |
| `SKLAD_HOST_ALT` | short mDNS name | `sklad` |
| `SKLAD_LAN_IP` | NAS LAN address — **put the address in the certificate** or phones will reject it | `127.0.0.1` |
| `SKLAD_PUBLIC_PORT` | port appended to the HTTPS redirect target, e.g. `:8443` | empty (443) |
| `SKLAD_HSTS_MAX_AGE` | HSTS `max-age`; `0` disables the header while you are still installing trust | `31536000` |

* Add more names by appending them to the site address line in the Caddyfile
  (and, to control the redirect port, to the `http://` blocks).
* **QTS owns 80/443?** Map host `8080 → 80` and `8443 → 443`, and set
  `SKLAD_PUBLIC_PORT=:8443` so plain-HTTP requests land on the right port. See
  [`../../docs/DEPLOY-QNAP.md`](../../docs/DEPLOY-QNAP.md).
* A certificate covering only the DNS name means `https://192.168.1.50` shows a
  warning and the camera stays blocked. Keep `SKLAD_LAN_IP` and the SANs in step.

## Checklist after any change

```bash
openssl x509 -in deploy/certs/sklad.crt -noout -subject -dates -ext subjectAltName   # mode B
openssl verify -CAfile deploy/certs/rootCA.pem deploy/certs/sklad.crt                # mode B
docker logs sklad-caddy | grep -i 'certificate\|error'                               # both modes
```

Common causes of a blocked camera: the CA is not installed (or, on iOS, is
installed but not *enabled* under Certificate Trust Settings), the address used
is not in the certificate SANs, or the page was opened over plain HTTP (the
camera API needs an HTTPS origin).
