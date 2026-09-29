package web

import (
	"io/fs"
	"sync"

	"github.com/TecharoHQ/anubis/internal/overlay"
)

func StaticFS() fs.FS {
	return overlay.FS(Static)
}

var (
	customFooter = sync.OnceValues(func() ([]byte, bool) { return overlay.LookupFile("footer.html") })
	customCSS    = sync.OnceValue(func() bool {
		_, ok := overlay.LookupFile("static/css/custom.css")
		return ok
	})
)
