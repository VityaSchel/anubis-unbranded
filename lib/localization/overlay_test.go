package localization

import (
	"encoding/json"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

func TestApplyOverrides(t *testing.T) {
	t.Setenv("CHALLENGE_TITLE", "Checking {{ your browser")
	t.Setenv("ERROR_TITLE", "")
	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)
	for _, name := range []string{"en.json", "de.json"} {
		if _, err := bundle.LoadMessageFileFS(localeFS, "locales/"+name); err != nil {
			t.Fatal(err)
		}
	}
	localize := func(lang, id string) (string, error) {
		return i18n.NewLocalizer(bundle, lang).Localize(&i18n.LocalizeConfig{MessageID: id})
	}

	for id, env := range titleEnv {
		if _, err := localize("en", id); err != nil {
			t.Fatalf("upstream no longer has %s, so %s would silently stop working: %v", id, env, err)
		}
	}
	loading, _ := localize("en", "loading")
	errorTitle, _ := localize("en", "oh_noes")

	applyOverrides(bundle, fstest.MapFS{
		"locales/en.json":       {Data: []byte(`{"protected_by": "Guarded by {{.Name}}", "mascot_design": "", "making_sure_not_bot": "from file"}`)},
		"locales/en-GB.json":    {Data: []byte(`{"protected_by": "Guarded by"}`)},
		"locales/de.json":       {Data: []byte(`{"protected_by": {"leftDelim": "<<", "rightDelim": ">>", "other": "a << b"}}`)},
		"locales/manifest.json": {Data: []byte(`{"supportedLanguages": []}`)},
	})

	for _, tt := range []struct {
		lang, id, want string
	}{
		{lang: "en", id: "protected_by", want: "Guarded by {{.Name}}"},
		{lang: "en", id: "mascot_design", want: ""},
		{lang: "de", id: "protected_by", want: "a << b"},
		{lang: "en", id: "loading", want: loading},
		{lang: "en-GB", id: "loading", want: loading},
		{lang: "en", id: "making_sure_not_bot", want: "Checking {{ your browser"},
		{lang: "de", id: "making_sure_not_bot", want: "Checking {{ your browser"},
		{lang: "en", id: "oh_noes", want: errorTitle},
	} {
		got, err := localize(tt.lang, tt.id)
		if err != nil {
			t.Errorf("%s/%s: %v", tt.lang, tt.id, err)
		}
		if got != tt.want {
			t.Errorf("%s/%s: got %q, want %q", tt.lang, tt.id, got, tt.want)
		}
	}
}

func TestFallbackToEnglish(t *testing.T) {
	bundle := i18n.NewBundle(language.English)
	if err := bundle.AddMessages(language.English, &i18n.Message{ID: "only_en", Other: "English"}); err != nil {
		t.Fatal(err)
	}
	if err := bundle.AddMessages(language.German, &i18n.Message{ID: "both", Other: "Deutsch"}); err != nil {
		t.Fatal(err)
	}
	sl := SimpleLocalizer{Localizer: i18n.NewLocalizer(bundle, "de", "en")}

	if got := sl.T("only_en"); got != "English" {
		t.Errorf("missing in de: got %q, want the English text", got)
	}
	if got := sl.T("both"); got != "Deutsch" {
		t.Errorf("present in de: got %q", got)
	}
	defer func() {
		if recover() == nil {
			t.Error("a message missing in every language must still panic")
		}
	}()
	sl.T("nowhere")
}

func TestUnbrandedStrings(t *testing.T) {
	read := func(fsys fs.FS, name string) map[string]any {
		t.Helper()
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			t.Fatal(err)
		}
		var messages map[string]any
		if err := json.Unmarshal(data, &messages); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return messages
	}
	english := read(unbrandedFS, "unbranded/en.json")
	defaults, err := DefaultLocales()
	if err != nil {
		t.Fatal(err)
	}
	service := NewLocalizationService()

	entries, _ := localeFS.ReadDir("locales")
	for _, entry := range entries {
		name := entry.Name()
		if name == manifestFile {
			continue
		}
		lang := strings.TrimSuffix(name, ".json")
		if _, err := fs.Stat(unbrandedFS, "unbranded/"+name); err != nil {
			t.Logf("unbranded/%s is missing: %s uses the English alt texts", name, lang)
			continue
		}
		messages := read(unbrandedFS, "unbranded/"+name)
		if !slices.Equal(slices.Sorted(maps.Keys(messages)), slices.Sorted(maps.Keys(english))) {
			t.Errorf("unbranded/%s: keys differ from unbranded/en.json", name)
		}

		var merged map[string]any
		if err := json.Unmarshal(defaults[name], &merged); err != nil {
			t.Fatalf("DefaultLocales %s: %v", name, err)
		}
		builtin := read(localeFS, "locales/"+name)
		sl := SimpleLocalizer{Localizer: service.GetLocalizer(lang)}
		for id, text := range messages {
			if merged[id] != text {
				t.Errorf("DefaultLocales %s: %s = %q, want %q", name, id, merged[id], text)
			}
			if got := sl.T(id); got != text {
				t.Errorf("%s: %s = %q, want %q", lang, id, got, text)
			}
		}
		for id := range builtin {
			if _, ok := merged[id]; !ok {
				t.Errorf("DefaultLocales %s lost %s", name, id)
			}
		}
	}
}
