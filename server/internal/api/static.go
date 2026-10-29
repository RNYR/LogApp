package api

// The handlers that serve the embedded single-page application live in this
// file.

import (
	"bytes"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

// placeholderHTML is served while web/dist has no bundle, so a fresh checkout
// and the smoke test still get an intelligible page instead of a blank error.
const placeholderHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Sklad — frontend not built</title>
<style>
body { font-family: system-ui, sans-serif; line-height: 1.5; margin: 3rem auto; max-width: 40rem; padding: 0 1rem; }
code, pre { background: #f2f4f6; border-radius: .25rem; padding: .15rem .35rem; }
</style>
</head>
<body>
<h1>Sklad</h1>
<p>The API is running, but the web frontend has not been built yet.</p>
<p>Build it with:</p>
<pre><code>cd web &amp;&amp; npm install &amp;&amp; npm run build</code></pre>
<p>Then reload this page. API status: <a href="/api/v1/health">/api/v1/health</a></p>
</body>
</html>
`

// serveSPA serves the embedded SPA: real files first, then index.html so that
// client-side routes survive a reload.
func (a *API) serveSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		a.notFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		a.methodNotAllowed(w, r)
		return
	}
	if !a.built || a.assets == nil {
		a.servePlaceholder(w, r)
		return
	}

	name, ok := spaPath(r.URL.Path)
	if !ok {
		a.notFound(w, r)
		return
	}
	data, err := fs.ReadFile(a.assets, name)
	if err != nil && name != "index.html" && path.Ext(name) == "" {
		// An extension-less path is a client-side route: serve the shell.
		name = "index.html"
		data, err = fs.ReadFile(a.assets, name)
	}
	if err != nil {
		a.notFound(w, r)
		return
	}
	writeAsset(w, r, name, data)
}

// servePlaceholder answers with the "frontend not built" page: 200 for the app
// shell and client-side routes, 404 for a missing asset.
func (a *API) servePlaceholder(w http.ResponseWriter, r *http.Request) {
	status := http.StatusOK
	name, ok := spaPath(r.URL.Path)
	if !ok || (name != "index.html" && path.Ext(name) != "") {
		status = http.StatusNotFound
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(w, placeholderHTML)
}

// spaPath maps a URL path onto a clean file name inside the bundle. It reports
// false when the path tries to escape the bundle.
func spaPath(requestPath string) (string, bool) {
	name := strings.TrimPrefix(path.Clean("/"+requestPath), "/")
	if name == "" || name == "." {
		return "index.html", true
	}
	if !fs.ValidPath(name) {
		return "", false
	}
	return name, true
}

// writeAsset writes one bundled file with an explicit content type and cache
// policy. Vite fingerprints names under assets/, so those are immutable.
func writeAsset(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	if contentType := assetContentType(name); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

// assetContentType returns the MIME type for a bundled file. The table is
// explicit because the answer must not depend on the host's /etc/mime.types:
// WebAssembly, for example, only streams when it is served as application/wasm.
func assetContentType(name string) string {
	extension := strings.ToLower(path.Ext(name))
	switch extension {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json", ".map":
		return "application/json; charset=utf-8"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	case ".ico":
		return "image/x-icon"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".otf":
		return "font/otf"
	case ".wasm":
		return "application/wasm"
	case ".webmanifest":
		return "application/manifest+json"
	case ".pdf":
		return "application/pdf"
	}
	return mime.TypeByExtension(extension)
}
