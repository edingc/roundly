// Package web embeds the built frontend so the server ships as one binary.
//
// The dist directory is produced by `npm run build`. A placeholder index.html is
// committed so the package compiles on a clean checkout before the frontend has
// been built; the real build overwrites it.
package web

import (
	"io/fs"

	"embed"
)

//go:embed all:dist
var distFS embed.FS

// Dist returns the built frontend rooted at dist/, ready to hand to
// server.SPAHandler.
func Dist() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
