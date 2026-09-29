package overlay

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"sync"
)

var Dir = sync.OnceValue(func() fs.FS {
	path := os.Getenv("OVERLAY_FOLDER")
	if path == "" {
		return nil
	}

	root, err := os.OpenRoot(path)
	if err != nil {
		slog.Error("can't open overlay folder", "path", path, "err", err)
		return nil
	}

	return root.FS()
})

func FS(embedded fs.FS) fs.FS {
	return layer(Dir(), embedded)
}

func LookupFile(name string) ([]byte, bool) {
	dir := Dir()
	if dir == nil {
		return nil, false
	}

	data, err := fs.ReadFile(dir, name)
	logUnexpected(slog.LevelError, name, err)
	return data, err == nil
}

func layer(overlay, embedded fs.FS) fs.FS {
	if overlay == nil {
		return embedded
	}

	return staticOverlay{overlay, embedded}
}

type staticOverlay struct{ overlay, embedded fs.FS }

func (s staticOverlay) Open(name string) (fs.File, error) {
	if strings.HasPrefix(name, "static/") {
		f, err := s.overlay.Open(name)
		if err == nil {
			return f, nil
		}
		logUnexpected(slog.LevelDebug, name, err)
	}

	return s.embedded.Open(name)
}

func logUnexpected(level slog.Level, name string, err error) {
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Log(context.Background(), level, "can't read overlay file", "name", name, "err", err)
	}
}
