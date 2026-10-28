// Package docsite serves the Elvish website from a copy embedded in the
// binary, for reading the documentation offline.
//
// The website is built by a separate toolchain under website/ and is not
// rendered here. "go generate" in this package runs that build and copies
// its output into the site directory, which is embedded. A binary built
// without that step serves a page saying so instead.
package docsite

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:generate make -C ../../website embed

// The built website. Only a .gitkeep is checked in; see the package doc.
//
//go:embed all:site
var siteFS embed.FS

//go:embed placeholder.html
var placeholder []byte

// Site returns the embedded website as a file system rooted at the site's
// root, and whether it has actually been built into this binary.
func Site() (fs.FS, bool) {
	site, err := fs.Sub(siteFS, "site")
	if err != nil {
		return nil, false
	}
	_, err = fs.Stat(site, "index.html")
	return site, err == nil
}

// Handler returns an HTTP handler serving the embedded website, or the
// placeholder page for every path if the website is not built in.
func Handler() http.Handler {
	site, ok := Site()
	if !ok {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			w.Write(placeholder)
		})
	}
	return http.FileServer(http.FS(site))
}
