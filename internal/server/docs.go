package server

import (
	"embed"
	"io/fs"
	"net/http"
)

// docsFS holds the API documentation page, rendered by Redoc.
// See docs/README.md for how to update the vendored Redoc files.
//
// The files are listed explicitly so docs/README.md isn't embedded and served.
// The glob covers redoc.standalone.js.LICENSE.txt, which only some versions ship.
//
//go:embed docs/index.html docs/redoc.standalone.js* docs/LICENSE.redoc
var docsFS embed.FS

// docsHandler serves the embedded docs, to be mounted at "/docs/"
func docsHandler() http.Handler {
	sub, err := fs.Sub(docsFS, "docs")
	if err != nil {
		panic(err) // can only fail on a malformed path, which is a programming error
	}
	return http.StripPrefix("/docs/", http.FileServerFS(sub))
}
