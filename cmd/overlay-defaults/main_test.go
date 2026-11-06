package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteDefaults(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "overlay")
	if err := writeDefaults(folder); err != nil {
		t.Fatalf("upstream moved a folder this tool copies: %v", err)
	}

	for _, name := range []string{
		"templates/challenge.tmpl",
		"templates/error.tmpl",
		"templates/impressum.tmpl",
		"locales/en.json",
		"static/img/happy.webp",
	} {
		info, err := os.Stat(filepath.Join(folder, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}

	var de map[string]string
	data, err := os.ReadFile(filepath.Join(folder, "locales", "de.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &de); err != nil {
		t.Fatalf("locales/de.json: %v", err)
	}
	for _, id := range []string{"loading", "image_alt_reject"} {
		if de[id] == "" {
			t.Errorf("locales/de.json has no %s", id)
		}
	}

	if err := writeDefaults(folder); !errors.Is(err, fs.ErrExist) {
		t.Errorf("existing folder: got %v, want fs.ErrExist", err)
	}
}
