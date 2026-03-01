//go:build embed_frontend

package server

import (
	"embed"
	"io/fs"
)

//go:embed webdist/*
var embeddedFrontendFiles embed.FS

func embeddedFrontendFS() (fs.FS, bool) {
	sub, err := fs.Sub(embeddedFrontendFiles, "webdist")
	if err != nil {
		return nil, false
	}

	return sub, true
}
