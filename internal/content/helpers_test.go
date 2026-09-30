package content

import (
	"io/fs"
	"testing"
)

func sub(t *testing.T, fsys fs.FS, dir string) fs.FS {
	t.Helper()
	s, err := fs.Sub(fsys, dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
