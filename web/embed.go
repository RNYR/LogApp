// Package web embeds the built single-page application into the Go binary.
//
// The //go:embed directive has to live in this directory: embed patterns cannot
// escape their own package, so a directive under server/... could not reach
// web/dist.
//
// web/dist/.gitkeep is committed so the package always has at least one
// embedded file and the backend compiles before the SPA has ever been built.
// Assets then reports built=false and the API serves a placeholder page.
package web

import (
	"embed"
	"errors"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Assets returns the built SPA, rooted at the contents of web/dist.
//
// built is false when there is no index.html yet (a fresh checkout, or a build
// that has not run); callers should then serve a placeholder instead of their
// SPA handler.
func Assets() (fs.FS, bool, error) {
	root, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, false, err
	}
	if _, err := fs.Stat(root, "index.html"); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return root, false, nil
		}
		return root, false, err
	}
	return root, true, nil
}
