package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/TecharoHQ/anubis/lib/localization"
	"github.com/TecharoHQ/anubis/web"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: anubis-overlay-defaults <new folder>")
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	if err := writeDefaults(flag.Arg(0)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func writeDefaults(folder string) error {
	templates, err := fs.Sub(web.Templates, "templates")
	if err != nil {
		return err
	}
	images, err := fs.Sub(web.Static, "static/img")
	if err != nil {
		return err
	}

	if err := os.Mkdir(folder, 0o755); err != nil {
		return err
	}
	err = errors.Join(
		os.CopyFS(filepath.Join(folder, "templates"), templates),
		os.CopyFS(filepath.Join(folder, "locales"), localization.Locales()),
		os.CopyFS(filepath.Join(folder, "static", "img"), images),
	)
	if err != nil {
		return errors.Join(err, os.RemoveAll(folder))
	}
	return nil
}
