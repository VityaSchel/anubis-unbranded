package web

import (
	"bytes"
	"io/fs"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/a-h/templ"
	"golang.org/x/net/html"

	"github.com/TecharoHQ/anubis/lib/config"
	"github.com/TecharoHQ/anubis/lib/localization"
)

func testLocalizer() *localization.SimpleLocalizer {
	return &localization.SimpleLocalizer{
		Localizer: localization.NewLocalizationService().GetLocalizerFromRequest(httptest.NewRequest("GET", "/", nil)),
	}
}

func withOverlay(t *testing.T, dir fs.FS) {
	t.Helper()
	builtin := loadedCustomization
	t.Cleanup(func() { loadedCustomization = builtin })
	c := loadCustomization(dir)
	loadedCustomization = func() *customization { return c }
}

func renderPage(t *testing.T, body templ.Component, honeypot *config.Honeypot) string {
	t.Helper()
	impressum := &config.Impressum{Footer: "<p>imprint footer</p>"}
	var out bytes.Buffer
	if err := base("Page title", body, impressum, honeypot, nil, map[string]string{"og:title": "Title"}, testLocalizer()).Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func pageBodies() map[string]templ.Component {
	return map[string]templ.Component{
		"challenge": Bench(testLocalizer()),
		"error":     ErrorPage("message", "admin@example.com", "code", testLocalizer()),
		"impressum": config.ImpressumPage{Title: "Imprint", Body: "<p>imprint</p>"},
	}
}

func normalized(t *testing.T, page string) string {
	t.Helper()
	document, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}

	var blank []*html.Node
	for node := range document.Descendants() {
		if node.Type != html.TextNode {
			continue
		}
		if node.Data = strings.TrimSpace(node.Data); node.Data == "" {
			blank = append(blank, node)
		}
	}
	for _, node := range blank {
		node.Parent.RemoveChild(node)
	}

	var out strings.Builder
	if err := html.Render(&out, document); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestBuiltinTemplatesReproduceUpstreamPage(t *testing.T) {
	for kind, body := range pageBodies() {
		upstream := renderPage(t, body, nil)
		withOverlay(t, Templates)
		custom := renderPage(t, body, nil)

		if want, got := normalized(t, upstream), normalized(t, custom); got != want {
			t.Errorf("%s: update web/templates/%s.tmpl to match the upstream layout\nupstream: %s\nbuilt-in: %s", kind, kind, want, got)
		}
	}
}

func TestCustomTemplateReceivesUpstreamParts(t *testing.T) {
	withOverlay(t, fstest.MapFS{
		"templates/challenge.tmpl": {Data: []byte(`{{ .Lang }}|{{ .Title }}|{{ .Head }}|{{ .Body }}|{{ .Content }}|{{ .Footer }}|{{ Asset "css/site.css" }}`)},
	})

	parts := strings.Split(renderPage(t, templ.Raw(`<p id="challenge">work</p>`), &config.Honeypot{Enabled: true}), "|")
	if len(parts) != 7 {
		t.Fatalf("got %d parts, want 7: %q", len(parts), parts)
	}
	lang, title, head, body, content, footer, asset := parts[0], parts[1], parts[2], parts[3], parts[4], parts[5], parts[6]

	for name, tt := range map[string]struct{ got, want string }{
		"doctype and lang":      {lang, "<!doctype html>en"},
		"title":                 {title, "Page title"},
		"head has scripts":      {head, `id="anubis_version"`},
		"body keeps main":       {body, "<main>"},
		"body keeps footer":     {body, "https://techaro.lol"},
		"content has title":     {content, `id="title"`},
		"content has challenge": {content, `<p id="challenge">work</p>`},
		"content has honeypot":  {content, `<script type="ignore">`},
		"footer has upstream":   {footer, "https://techaro.lol"},
		"footer has imprint":    {footer, "<p>imprint footer</p>"},
		"asset path":            {asset, "/.within.website/x/cmd/anubis/static/css/site.css"},
	} {
		if !strings.Contains(strings.ToLower(tt.got), strings.ToLower(tt.want)) {
			t.Errorf("%s: %q does not contain %q", name, tt.got, tt.want)
		}
	}

	for name, part := range map[string]string{"head": head, "content": content} {
		if strings.Contains(part, "<footer") || strings.Contains(part, "techaro.lol") || strings.Contains(part, "<main") {
			t.Errorf("%s leaks the upstream layout: %q", name, part)
		}
	}
}

func TestPageKindSelectsTemplate(t *testing.T) {
	withOverlay(t, fstest.MapFS{
		"templates/challenge.tmpl": {Data: []byte(`challenge template {{ .Head }}{{ .Body }}`)},
		"templates/error.tmpl":     {Data: []byte(`error template {{ .Body }}`)},
		"templates/impressum.tmpl": {Data: []byte(`impressum template {{ .Body }}`)},
	})

	for kind, body := range pageBodies() {
		if got := renderPage(t, body, nil); !strings.Contains(got, kind+" template ") {
			t.Errorf("%s page did not use %s.tmpl: %q", kind, kind, got)
		}
	}
}

func TestFooterAndStylesheet(t *testing.T) {
	for name, tt := range map[string]struct {
		overlay       fstest.MapFS
		body          templ.Component
		want, missing []string
	}{
		"footer.html and custom.css": {
			overlay: fstest.MapFS{"footer.html": {Data: []byte(`<p id="mine">Example Corp</p>`)}, "static/css/custom.css": {}},
			body:    templ.Raw("<p>body</p>"),
			want:    []string{`<p id="mine">Example Corp</p>`, `/static/css/custom.css?cacheBuster=`, "<main>", `id="title"`},
			missing: []string{"techaro.lol", "imprint footer"},
		},
		"empty footer.html": {
			overlay: fstest.MapFS{"footer.html": {}},
			body:    templ.Raw("<p>body</p>"),
			want:    []string{"<footer>", "<p>body</p>"},
			missing: []string{"techaro.lol"},
		},
		"stray main end tag in the page body": {
			overlay: fstest.MapFS{"footer.html": {Data: []byte(`<p id="mine">Example Corp</p>`)}},
			body:    config.ImpressumPage{Body: `<p>Address</p></main><p>after main</p>`},
			want:    []string{"<p>Address</p>", "<p>after main</p>", `<p id="mine">Example Corp</p>`},
			missing: []string{"techaro.lol"},
		},
		"footer element in the page body": {
			overlay: fstest.MapFS{"footer.html": {Data: []byte(`<p id="mine">Example Corp</p>`)}},
			body:    config.ImpressumPage{Body: `<article><p>Address</p><footer>Last updated 2026-01-01</footer></article>`},
			want:    []string{"Last updated 2026-01-01", `<p id="mine">Example Corp</p>`},
			missing: []string{"techaro.lol"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			withOverlay(t, tt.overlay)
			got := renderPage(t, tt.body, nil)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in %q", want, got)
				}
			}
			for _, missing := range tt.missing {
				if strings.Contains(got, missing) {
					t.Errorf("unexpected %q in %q", missing, got)
				}
			}
		})
	}
}

func TestNothingToCustomize(t *testing.T) {
	for name, dir := range map[string]fs.FS{
		"no overlay":          nil,
		"empty overlay":       fstest.MapFS{},
		"unrelated files":     fstest.MapFS{"static/img/happy.webp": {}, "locales/en.json": {Data: []byte("{}")}},
		"unparsable template": fstest.MapFS{"templates/challenge.tmpl": {Data: []byte("{{ .Head")}},
		"failing template":    fstest.MapFS{"templates/challenge.tmpl": {Data: []byte("{{ .Head }}{{ .Body }}{{ .Missing }}")}},
		"failing branch":      fstest.MapFS{"templates/challenge.tmpl": {Data: []byte("{{ .Head }}{{ .Body }}{{ if .Footer }}{{ .Missing }}{{ end }}")}},
		"template sans body":  fstest.MapFS{"templates/error.tmpl": {Data: []byte("<p>oops</p>")}},
		"challenge sans head": fstest.MapFS{"templates/challenge.tmpl": {Data: []byte("{{ .Body }}")}},
		"unknown template":    fstest.MapFS{"templates/other.tmpl": {Data: []byte("{{ .Head }}{{ .Body }}")}},
	} {
		if loadCustomization(dir) != nil {
			t.Errorf("%s: got a customization, want none", name)
		}
	}
}

func TestUnusableTemplateFallsBackToBuiltin(t *testing.T) {
	withOverlay(t, fstest.MapFS{
		"templates/challenge.tmpl": {Data: []byte("{{ .Head")},
		"templates/error.tmpl":     {Data: []byte("custom error {{ .Body }}")},
	})
	bodies := pageBodies()

	if got := renderPage(t, bodies["challenge"], nil); !strings.Contains(got, "<main>") {
		t.Errorf("challenge did not use the built-in template: %q", got)
	}
	if got := renderPage(t, bodies["error"], nil); !strings.Contains(got, "custom error ") {
		t.Errorf("error did not use the custom template: %q", got)
	}
}

func TestByteOrderMarkKeepsSingleDoctype(t *testing.T) {
	withOverlay(t, fstest.MapFS{
		"templates/challenge.tmpl": {Data: []byte(byteOrderMark + "<!DOCTYPE html><html><head>{{ .Head }}</head><body>{{ .Body }}</body></html>")},
	})

	got := renderPage(t, templ.Raw("<p>body</p>"), nil)
	if strings.Count(strings.ToLower(got), "<!doctype") != 1 || strings.Contains(got, byteOrderMark) {
		t.Errorf("want one doctype and no byte order mark: %q", got[:80])
	}
}

func TestUnexpectedLayoutServesUpstreamPage(t *testing.T) {
	c := loadCustomization(fstest.MapFS{"footer.html": {}})
	const upstream = "<p>a page without the usual layout</p>"

	var out bytes.Buffer
	if err := c.apply("challenge", page{}, templ.Raw(upstream)).Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != upstream {
		t.Errorf("got %q, want the upstream page", got)
	}
}
