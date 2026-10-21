package main

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"src.elv.sh/pkg/testutil"
)

func TestInclude(t *testing.T) {
	testutil.Set[fs.FS](t, &pkgFS, fstest.MapFS{
		"mods/doc/language.md": {Data: []byte("<!-- toc -->\n\n# Language\n\nReference.\n")},
	})
	var out strings.Builder
	filter(strings.NewReader("@include mods/doc/language.md\n"), &out)
	want := "<!-- toc -->\n\n# Language\n\nReference.\n"
	if got := out.String(); got != want {
		t.Errorf("filter = %q, want %q", got, want)
	}
}
