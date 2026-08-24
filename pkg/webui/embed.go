// Package webui carries the browser interface as files compiled into the
// binary, so a deployment stays a single executable with no asset directory to
// ship alongside it.
//
// The files under dist are produced by the Vite project in web/ and copied
// here by `make ui`. They are committed, because go:embed reads them at
// compile time and cannot reach outside this package directory.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// Files returns the interface rooted at dist, so a request path maps directly
// onto a file name.
func Files() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		// dist is embedded at compile time, so a failure here means the
		// binary was built without it and there is nothing to serve.
		panic("webui: dist is missing from the binary: " + err.Error())
	}
	return sub
}
