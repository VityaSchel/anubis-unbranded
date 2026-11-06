package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

func TestStaticFiles(t *testing.T) {
	overlay := fstest.MapFS{
		"static/img/happy.webp":  {Data: []byte("custom happy")},
		"locales/en.json":        {Data: []byte(`{"js_calculating": "Working", "js_speed": "", "loading": "Wait"}`)},
		"footer.html":            {Data: []byte("<p>footer</p>")},
		"locales/de.json":        {Data: []byte(`{"js_calculating": "Rechnet"}`)},
		"static/locales/de.json": {Data: []byte(`{"js_calculating": "whole file"}`)},
	}
	files := staticFiles(overlay)

	for name, tt := range map[string]struct {
		path        string
		status      int
		revalidated bool
		body        string
	}{
		"overlay file":      {"/static/img/happy.webp", http.StatusOK, true, "custom happy"},
		"embedded file":     {"/static/img/pensive.webp", http.StatusOK, false, ""},
		"merged locale":     {"/static/locales/en.json", http.StatusOK, true, ""},
		"whole locale wins": {"/static/locales/de.json", http.StatusOK, true, `{"js_calculating": "whole file"}`},
		"outside static":    {"/footer.html", http.StatusNotFound, false, ""},
		"outside by parent": {"/static/../locales/en.json", http.StatusNotFound, false, ""},
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			files.ServeHTTP(response, httptest.NewRequest("GET", tt.path, nil))

			if response.Code != tt.status {
				t.Fatalf("got status %d, want %d", response.Code, tt.status)
			}
			if got := response.Header().Get("Cache-Control") == revalidate; got != tt.revalidated {
				t.Errorf("revalidated: got %v, want %v", got, tt.revalidated)
			}
			if tt.body != "" && response.Body.String() != tt.body {
				t.Errorf("got body %q, want %q", response.Body, tt.body)
			}
		})
	}

	response := httptest.NewRecorder()
	files.ServeHTTP(response, httptest.NewRequest("GET", "/static/locales/en.json", nil))
	var messages map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &messages); err != nil {
		t.Fatal(err)
	}
	if messages["js_calculating"] != "Working" {
		t.Errorf("override not applied: %q", messages["js_calculating"])
	}
	if _, ok := messages["loading"]; ok {
		t.Error("a string the challenge script does not use is served to it")
	}
	if response.Header().Get("ETag") == "" {
		t.Error("merged locale has no ETag to revalidate against")
	}
	if messages["js_speed"] == "" || len(messages) < 10 {
		t.Errorf("built-in strings were lost: %d messages, js_speed=%q", len(messages), messages["js_speed"])
	}
}

func TestClientLocalesWithoutOverlay(t *testing.T) {
	files := staticFiles(nil)
	fetch := func(lang string) map[string]any {
		t.Helper()
		response := httptest.NewRecorder()
		files.ServeHTTP(response, httptest.NewRequest("GET", "/static/locales/"+lang+".json", nil))
		var messages map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &messages); err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		return messages
	}

	english := fetch("en")
	for _, id := range []string{"image_alt_pensive", "image_alt_reject"} {
		if english[id] == nil {
			t.Errorf("en has no %s", id)
		}
	}
	german := fetch("de")
	if german["image_alt_reject"] == english["image_alt_reject"] || german["js_calculating"] == english["js_calculating"] {
		t.Error("de is not translated")
	}
	for id, text := range english {
		if german[id] == nil {
			t.Errorf("de has no %s; it should fall back to %q", id, text)
		}
	}
}

func TestClientLocalesHoldOnlyScriptStrings(t *testing.T) {
	response := httptest.NewRecorder()
	staticFiles(nil).ServeHTTP(response, httptest.NewRequest("GET", "/static/locales/en.json", nil))
	var messages map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &messages); err != nil {
		t.Fatal(err)
	}
	for id := range messages {
		if !strings.HasPrefix(id, "js_") && !strings.HasPrefix(id, "image_alt_") {
			t.Errorf("served %s, which the challenge script never uses", id)
		}
	}

	script, err := os.ReadFile("js/main.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range regexp.MustCompile(`\bt\("([a-z_]+)"`).FindAllStringSubmatch(string(script), -1) {
		if messages["js_"+match[1]] == "" && messages[match[1]] == "" {
			t.Errorf("main.ts asks for %s, which the served strings do not have", match[1])
		}
	}
}
