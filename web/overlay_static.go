package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/TecharoHQ/anubis/internal/overlay"
	"github.com/TecharoHQ/anubis/lib/localization"
)

const revalidate = "no-cache"

func StaticFiles() http.Handler {
	loadedCustomization()
	localization.NewLocalizationService()
	return staticFiles(overlay.Dir())
}

func staticFiles(dir fs.FS) http.Handler {
	if dir == nil {
		return http.FileServerFS(Static)
	}

	files := http.FileServerFS(overlay.Layer(dir, Static))
	locales := localization.ClientLocales(dir)
	etags := map[string]string{}
	for name, locale := range locales {
		sum := sha256.Sum256(locale)
		etags[name] = `"` + hex.EncodeToString(sum[:8]) + `"`
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		info, err := fs.Stat(dir, name)
		inOverlay := err == nil && !info.IsDir() && strings.HasPrefix(name, "static/")

		if locale, ok := locales[path.Base(name)]; ok && !inOverlay && path.Dir(name) == "static/locales" {
			w.Header().Set("Cache-Control", revalidate)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("ETag", etags[path.Base(name)])
			http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(locale))
			return
		}

		if inOverlay {
			w.Header().Set("Cache-Control", revalidate)
		}
		files.ServeHTTP(w, r)
	})
}
