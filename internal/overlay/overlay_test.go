package overlay

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestLookupAfterInit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "footer.html"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OVERLAY_FOLDER", dir)

	for name, want := range map[string]bool{"footer.html": true, "missing.html": false} {
		if _, ok := Lookup(Dir(), name); ok != want {
			t.Errorf("Lookup(%q): got %v, want %v", name, ok, want)
		}
	}
}

func TestLayer(t *testing.T) {
	embedded := fstest.MapFS{"static/img/happy.webp": {Data: []byte("embedded happy")}}
	overlay := fstest.MapFS{
		"static/img/happy.webp": {Data: []byte("custom happy")},
		"static/css/custom.css": {Data: []byte("body{}")},
		"footer.html":           {Data: []byte("<p>footer</p>")},
		"anubis.env":            {Data: []byte("ED25519_PRIVATE_KEY_HEX=secret")},
	}

	for name, tt := range map[string]struct {
		overlay    fs.FS
		file, want string
	}{
		"overlay wins":                  {overlay, "static/img/happy.webp", "custom happy"},
		"unreadable overlay falls back": {deniedFS{}, "static/img/happy.webp", "embedded happy"},
		"overlay-only file":             {overlay, "static/css/custom.css", "body{}"},
		"no overlay":                    {nil, "static/img/happy.webp", "embedded happy"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := fs.ReadFile(Layer(tt.overlay, embedded), tt.file)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}

	for _, name := range []string{"static/missing.txt", "footer.html", "anubis.env"} {
		if _, err := fs.ReadFile(Layer(overlay, embedded), name); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: got %v, want fs.ErrNotExist", name, err)
		}
	}
}

type deniedFS struct{}

func (deniedFS) Open(string) (fs.File, error) { return nil, fs.ErrPermission }
