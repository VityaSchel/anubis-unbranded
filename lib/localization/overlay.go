package localization

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/TecharoHQ/anubis/internal/overlay"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

var titleEnv = map[string]string{
	"making_sure_not_bot": "CHALLENGE_TITLE",
	"oh_noes":             "ERROR_TITLE",
}

//go:embed unbranded/*.json
var unbrandedFS embed.FS

// DefaultLocales returns every built-in locale file with this fork's own
// strings appended, so an overlay can start from all customizable strings.
func DefaultLocales() (map[string][]byte, error) {
	entries, err := localeFS.ReadDir("locales")
	if err != nil {
		return nil, err
	}
	locales := map[string][]byte{}
	for _, entry := range entries {
		name := entry.Name()
		builtin, err := localeFS.ReadFile("locales/" + name)
		if err != nil {
			return nil, err
		}
		extra, err := unbrandedFS.ReadFile("unbranded/" + name)
		if err != nil {
			locales[name] = builtin
			continue
		}
		var messages map[string]string
		if err := json.Unmarshal(extra, &messages); err != nil {
			return nil, err
		}

		locale := bytes.TrimRight(builtin, " \t\r\n")
		locale = bytes.TrimRight(bytes.TrimSuffix(locale, []byte("}")), " \t\r\n")
		for _, id := range slices.Sorted(maps.Keys(messages)) {
			key, _ := json.Marshal(id)
			text, _ := json.Marshal(messages[id])
			locale = fmt.Appendf(locale, ",\n  %s: %s", key, text)
		}
		locale = append(locale, "\n}\n"...)
		if !json.Valid(locale) {
			return nil, fmt.Errorf("can't append the unbranded strings to %s", name)
		}
		locales[name] = locale
	}
	return locales, nil
}

// ClientLocales returns the strings the challenge script fetches, one file per
// built-in language: English first, then the language's own strings, then the
// overlay's non-empty ones, so a string missing in a language falls back to English.
// Only the script's own strings are included; the server renders the rest.
func ClientLocales(dir fs.FS) map[string][]byte {
	english := clientMessages(dir, "en.json")
	merged := map[string][]byte{}
	entries, _ := localeFS.ReadDir("locales")
	for _, entry := range entries {
		name := entry.Name()
		if name == manifestFile {
			continue
		}
		messages := maps.Clone(english)
		maps.Copy(messages, clientMessages(dir, name))
		maps.DeleteFunc(messages, func(id string, _ any) bool { return !clientString(id) })
		if locale, err := json.Marshal(messages); err == nil {
			merged[name] = locale
		}
	}
	return merged
}

func clientString(id string) bool {
	return strings.HasPrefix(id, "js_") || strings.HasPrefix(id, "image_alt_")
}

func clientMessages(dir fs.FS, name string) map[string]any {
	messages := map[string]any{}
	for _, file := range []struct {
		fsys fs.FS
		path string
	}{{localeFS, "locales/" + name}, {unbrandedFS, "unbranded/" + name}} {
		if data, err := fs.ReadFile(file.fsys, file.path); err == nil {
			_ = json.Unmarshal(data, &messages)
		}
	}
	if dir == nil {
		return messages
	}

	var custom map[string]any
	if data, err := fs.ReadFile(dir, "locales/"+name); err == nil && json.Unmarshal(data, &custom) == nil {
		for id, text := range custom {
			if text, ok := text.(string); ok && text != "" {
				messages[id] = text
			}
		}
	}
	return messages
}

func loadOverlay(bundle *i18n.Bundle) {
	loadUnbranded(bundle)
	applyOverrides(bundle, overlay.Dir())
}

func loadUnbranded(bundle *i18n.Bundle) {
	entries, _ := unbrandedFS.ReadDir("unbranded")
	for _, entry := range entries {
		name := entry.Name()
		if _, err := fs.Stat(localeFS, "locales/"+name); err != nil {
			continue
		}
		if _, err := bundle.LoadMessageFileFS(unbrandedFS, "unbranded/"+name); err != nil {
			slog.Error("can't load unbranded strings", "file", name, "err", err)
		}
	}
}

func applyOverrides(bundle *i18n.Bundle, fsys fs.FS) {
	builtin := bundle.LanguageTags()

	if fsys != nil {
		entries, err := fs.ReadDir(fsys, "locales")
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Error("can't read locale overrides", "err", err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".json") || name == manifestFile {
				continue
			}
			if _, err := fs.Stat(localeFS, "locales/"+name); err != nil {
				slog.Error("locale override must be named after a built-in language", "file", name)
				continue
			}
			data, err := fs.ReadFile(fsys, "locales/"+name)
			if err != nil {
				slog.Error("can't read locale override", "file", name, "err", err)
				continue
			}
			file, err := i18n.ParseMessageFileBytes(data, name, map[string]i18n.UnmarshalFunc{"json": json.Unmarshal})
			if err != nil {
				slog.Error("can't parse locale override", "file", name, "err", err)
				continue
			}
			addMessages(bundle, file.Tag, file.Messages...)
		}
	}

	for id, env := range titleEnv {
		if title := os.Getenv(env); title != "" {
			for _, tag := range builtin {
				addMessages(bundle, tag, &i18n.Message{ID: id, Other: title})
			}
		}
	}
}

func addMessages(bundle *i18n.Bundle, tag language.Tag, messages ...*i18n.Message) {
	for _, m := range messages {
		m.Other = literalTemplate(m.Other)
		m.LeftDelim, m.RightDelim = "", ""
	}
	if err := bundle.AddMessages(tag, messages...); err != nil {
		slog.Error("can't apply locale override", "lang", tag, "err", err)
	}
}

func literalTemplate(text string) string {
	return "{{" + strconv.Quote(text) + "}}"
}
