package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// GetFS returns the embedded sub-filesystem rooted at dist.
func GetFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
