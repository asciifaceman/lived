//go:build !embed_frontend

package server

import "io/fs"

func embeddedFrontendFS() (fs.FS, bool) {
	return nil, false
}
