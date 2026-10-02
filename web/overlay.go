package web

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/a-h/templ"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/TecharoHQ/anubis"
	"github.com/TecharoHQ/anubis/internal/overlay"
	"github.com/TecharoHQ/anubis/lib/config"
	"github.com/TecharoHQ/anubis/lib/localization"
)

//go:embed templates
var Templates embed.FS

var (
	ErrUnexpectedLayout   = errors.New("web: page has no head, body, main or footer element")
	ErrIncompleteTemplate = errors.New("web: template must output part of {{ .Body }} or {{ .Content }}; a challenge template must also output the scripts of {{ .Head }}, elements with the ids title, status and progress, and everything in {{ .Content }} that it does not place with Select, for example with Without")
)

const (
	customFooter     = "footer.html"
	customStylesheet = "css/custom.css"
	doctype          = "<!DOCTYPE html>"
	byteOrderMark    = "\xef\xbb\xbf"
)

var leadingDoctype = regexp.MustCompile(`(?i)^\s*<!doctype[^>]*>`)

type page struct {
	Lang, Title                 string
	Head, Body, Content, Footer template.HTML
}

type pageKindKey struct{}

func pageKind(kind string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, _ io.Writer) error {
		if current, ok := ctx.Value(pageKindKey{}).(*string); ok {
			*current = kind
		}
		return nil
	})
}

func customized(title string, body templ.Component, localizer *localization.SimpleLocalizer) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		upstream := templ.GetChildren(ctx)
		ctx = templ.ClearChildren(ctx)

		c := loadedCustomization()
		if c == nil {
			return upstream.Render(ctx, w)
		}

		kind := "challenge"
		if _, ok := body.(config.ImpressumPage); ok {
			kind = "impressum"
		}
		return c.apply(kind, page{Lang: localizer.GetLang(), Title: title}, upstream).Render(ctx, w)
	})
}

type customization struct {
	templates  map[string]*template.Template
	footer     *template.HTML
	stylesheet bool
}

var loadedCustomization = sync.OnceValue(func() *customization { return loadCustomization(overlay.Dir()) })

func loadCustomization(dir fs.FS) *customization {
	c := &customization{templates: map[string]*template.Template{}}
	footer, hasFooter := overlay.Lookup(dir, customFooter)
	if hasFooter {
		c.footer = new(template.HTML(footer))
	}
	_, c.stylesheet = overlay.Lookup(dir, "static/"+customStylesheet)
	customizes := hasFooter || c.stylesheet

	builtin, _ := fs.ReadDir(Templates, "templates")
	for _, entry := range builtin {
		name := "templates/" + entry.Name()
		kind := strings.TrimSuffix(entry.Name(), ".tmpl")
		if source, ok := overlay.Lookup(dir, name); ok {
			custom, err := parseTemplate(kind, source)
			if err == nil {
				c.templates[kind] = custom
				customizes = true
				continue
			}
			slog.Error("can't use overlay template, using the built-in one", "name", name, "err", err)
		}
		source, _ := Templates.ReadFile(name)
		c.templates[kind] = template.Must(parseTemplate(kind, source))
	}

	if !customizes {
		return nil
	}
	return c
}

func parseTemplate(kind string, source []byte) (*template.Template, error) {
	source = bytes.TrimPrefix(source, []byte(byteOrderMark))
	t, err := template.New(kind).Funcs(template.FuncMap{"Asset": asset, "Select": selectElements, "Without": without}).Parse(string(source))
	if err != nil {
		return nil, err
	}

	var trial bytes.Buffer
	if err := t.Execute(&trial, samplePage); err != nil {
		return nil, err
	}
	for _, required := range requiredOutput[kind] {
		if !bytes.Contains(trial.Bytes(), []byte(required)) {
			return nil, ErrIncompleteTemplate
		}
	}
	return t, nil
}

const (
	sampleElement = "data-sample"
	sampleHead    = `<title>title</title><script id="anubis_challenge" type="application/json">{}</script>` +
		`<script data-rest="head" type="application/json">{}</script>`
	sampleContent = `<script type="ignore" data-rest="script"></script><h1 id="title" data-sample>title</h1>` +
		`<style data-rest="style"></style><div class="centered-div" data-sample>` +
		`<img id="image" data-sample/><p id="status" data-sample>status</p><script id="anubis-main" data-sample></script>` +
		`<div id="progress" data-sample></div><details data-sample></details><noscript data-sample></noscript>` +
		`<div id="testarea" data-sample></div><code data-sample><pre data-sample>code</pre></code>` +
		`<p data-sample><a href="/" data-sample>link</a></p><div data-rest="element"></div><meta data-rest="meta"/></div>`
)

var samplePage = page{
	Lang:    "en",
	Title:   "title",
	Head:    sampleHead,
	Body:    `<main data-sample>` + sampleContent + `<footer data-sample>footer</footer></main>`,
	Content: sampleContent,
	Footer:  "footer",
}

var requiredOutput = map[string][]string{
	"challenge": {
		sampleElement,
		`id="anubis_challenge"`, `data-rest="head"`,
		`id="anubis-main"`, `id="title"`, `id="status"`, `id="progress"`,
		`data-rest="script"`, `data-rest="element"`, `data-rest="style"`, `data-rest="meta"`,
	},
	"error":     {sampleElement},
	"impressum": {sampleElement},
}

func asset(name string) string {
	return anubis.BasePrefix + anubis.StaticPath + "static/" + name
}

func parseFragment(fragment template.HTML) ([]*html.Node, error) {
	body := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	return html.ParseFragment(strings.NewReader(string(fragment)), body)
}

func selectElements(fragment template.HTML, selectors ...string) (template.HTML, error) {
	nodes, err := parseFragment(fragment)
	if err != nil {
		return "", err
	}

	var out strings.Builder
	for _, node := range nodes {
		renderSelected(&out, node, selectors)
	}
	return template.HTML(out.String()), nil
}

func renderSelected(out *strings.Builder, node *html.Node, selectors []string) {
	if selected(node, selectors) {
		_ = html.Render(out, node)
		return
	}
	for child := range node.ChildNodes() {
		renderSelected(out, child, selectors)
	}
}

func without(fragment template.HTML, selectors ...string) (template.HTML, error) {
	nodes, err := parseFragment(fragment)
	if err != nil {
		return "", err
	}

	var out strings.Builder
	for _, node := range nodes {
		if removable(node, selectors) {
			continue
		}
		var unwanted []*html.Node
		for descendant := range node.Descendants() {
			if removable(descendant, selectors) {
				unwanted = append(unwanted, descendant)
			}
		}
		for _, element := range unwanted {
			element.Parent.RemoveChild(element)
		}
		_ = html.Render(&out, node)
	}
	return template.HTML(out.String()), nil
}

func removable(node *html.Node, selectors []string) bool {
	return selected(node, selectors) && !strings.HasPrefix(attribute(node, "href"), asset(customStylesheet))
}

func selected(node *html.Node, selectors []string) bool {
	if node.Type != html.ElementNode {
		return false
	}
	return slices.ContainsFunc(selectors, func(selector string) bool {
		if id, ok := strings.CutPrefix(selector, "#"); ok {
			return id != "" && attribute(node, "id") == id
		}
		if class, ok := strings.CutPrefix(selector, "."); ok {
			return slices.Contains(strings.Fields(attribute(node, "class")), class)
		}
		return strings.EqualFold(node.Data, selector)
	})
}

func attribute(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func (c *customization) apply(kind string, p page, upstream templ.Component) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		var rendered bytes.Buffer
		if err := upstream.Render(context.WithValue(ctx, pageKindKey{}, &kind), &rendered); err != nil {
			return err
		}

		custom, err := c.render(kind, p, rendered.Bytes())
		if err != nil {
			slog.Error("can't customize page, serving the built-in one", "template", kind, "err", err)
			custom = rendered.Bytes()
		}

		_, err = w.Write(custom)
		return err
	})
}

func (c *customization) render(kind string, p page, upstream []byte) ([]byte, error) {
	if err := p.fill(upstream, c.footer); err != nil {
		return nil, err
	}
	if c.stylesheet {
		href := asset(customStylesheet) + "?cacheBuster=" + anubis.Version
		p.Head += template.HTML(`<link rel="stylesheet" href="` + template.HTMLEscapeString(href) + `">`)
	}

	var out bytes.Buffer
	if err := c.templates[kind].Execute(&out, p); err != nil {
		return nil, err
	}
	return leadingDoctype.ReplaceAll(out.Bytes(), nil), nil
}

func (p *page) fill(upstream []byte, footer *template.HTML) error {
	document, err := html.Parse(io.MultiReader(strings.NewReader(doctype), bytes.NewReader(upstream)))
	if err != nil {
		return err
	}

	headNode, bodyNode, mainNode := find(document, atom.Head), find(document, atom.Body), find(document, atom.Main)
	if headNode == nil || bodyNode == nil || mainNode == nil {
		return ErrUnexpectedLayout
	}
	footerNode := lastOutermost(mainNode, atom.Footer)
	if footerNode == nil {
		footerNode = lastOutermost(bodyNode, atom.Footer)
	}
	if footerNode == nil {
		return ErrUnexpectedLayout
	}

	if footer != nil {
		for footerNode.FirstChild != nil {
			footerNode.RemoveChild(footerNode.FirstChild)
		}
		footerNode.AppendChild(&html.Node{Type: html.RawNode, Data: string(*footer)})
	}
	p.Head, p.Body, p.Footer = inner(headNode), inner(bodyNode), inner(footerNode)

	footerNode.Parent.RemoveChild(footerNode)
	var content strings.Builder
	for node := range bodyNode.ChildNodes() {
		if node == mainNode {
			content.WriteString(string(inner(mainNode)))
		} else {
			_ = html.Render(&content, node)
		}
	}
	p.Content = template.HTML(content.String())
	return nil
}

func find(root *html.Node, element atom.Atom) *html.Node {
	for node := range root.Descendants() {
		if node.DataAtom == element {
			return node
		}
	}
	return nil
}

func lastOutermost(root *html.Node, element atom.Atom) *html.Node {
	var last *html.Node
	for node := range root.Descendants() {
		if node.DataAtom == element && !within(node, last) {
			last = node
		}
	}
	return last
}

func within(node, ancestor *html.Node) bool {
	for parent := range node.Ancestors() {
		if parent == ancestor {
			return true
		}
	}
	return false
}

func inner(node *html.Node) template.HTML {
	var out strings.Builder
	for child := range node.ChildNodes() {
		_ = html.Render(&out, child)
	}
	return template.HTML(out.String())
}
