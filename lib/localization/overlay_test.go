package localization

import (
	"encoding/json"
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
