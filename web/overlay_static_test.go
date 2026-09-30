package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestStaticFiles(t *testing.T) {
	overlay := fstest.MapFS{
		"static/img/happy.webp":  {Data: []byte("custom happy")},
		"locales/en.json":        {Data: []byte(`{"calculating": "Working", "loading": ""}`)},
		"footer.html":            {Data: []byte("<p>footer</p>")},
		"locales/de.json":        {Data: []byte(`{"calculating": "Rechnet"}`)},
		"static/locales/de.json": {Data: []byte(`{"calculating": "whole file"}`)},
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
		"whole locale wins": {"/static/locales/de.json", http.StatusOK, true, `{"calculating": "whole file"}`},
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
	if messages["calculating"] != "Working" {
		t.Errorf("override not applied: %q", messages["calculating"])
	}
	if response.Header().Get("ETag") == "" {
		t.Error("merged locale has no ETag to revalidate against")
	}
	if messages["loading"] == "" || len(messages) < 10 {
		t.Errorf("built-in strings were lost: %d messages, loading=%q", len(messages), messages["loading"])
	}
}
