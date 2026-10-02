package localization

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
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

func Locales() fs.FS {
	locales, _ := fs.Sub(localeFS, "locales")
	return locales
}

func ClientLocales(dir fs.FS) map[string][]byte {
	merged := map[string][]byte{}
	entries, _ := fs.ReadDir(dir, "locales")
	for _, entry := range entries {
		name := entry.Name()
		builtin, err := localeFS.ReadFile("locales/" + name)
		if err != nil || name == manifestFile {
			continue
		}
		overrides, err := fs.ReadFile(dir, "locales/"+name)
		if err != nil {
			continue
		}

		var messages, custom map[string]any
		if json.Unmarshal(builtin, &messages) != nil || json.Unmarshal(overrides, &custom) != nil {
			continue
		}
		for id, text := range custom {
			if text, ok := text.(string); ok && text != "" {
				messages[id] = text
			}
		}
		if locale, err := json.Marshal(messages); err == nil {
			merged[name] = locale
		}
	}
	return merged
}

func loadOverlay(bundle *i18n.Bundle) {
	applyOverrides(bundle, overlay.Dir())
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
