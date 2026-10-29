// Sklad — equipment inventory & lending.
//
// The Go module is rooted at the repository root. Backend sources live under
// ./server (e.g. server/cmd/sklad, server/internal/...), so packages are
// imported as "sklad/server/...". The legacy ./equipment-log tree is a separate
// nested module and is intentionally excluded from this module's builds.
module sklad

go 1.23.0

require (
	github.com/go-chi/chi/v5 v5.3.2
	golang.org/x/crypto v0.40.0
	modernc.org/sqlite v1.38.2
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/exp v0.0.0-20250620022241-b7579e27df2b // indirect
	golang.org/x/sys v0.34.0 // indirect
	modernc.org/libc v1.66.3 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)
