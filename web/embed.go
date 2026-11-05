package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var content embed.FS

func FS() (fs.FS, error) {
	return fs.Sub(content, "dist")
}
