// Package console holds the admin console's built single-page app. The
// sources are in /console at the repository root; `pnpm build` there writes
// dist/, which is committed so `go build` needs no Node.
package console

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Assets is the built SPA, rooted at dist/.
func Assets() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic("console: dist missing from the build: " + err.Error())
	}
	return sub
}
