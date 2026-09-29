package web

import (
	"bytes"
	"html/template"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

// markdownRenderer renders suggestion descriptions. Raw HTML is dropped and
// unsafe link schemes (javascript: and the like) are not rendered: the text
// includes query texts and object names taken from the monitored database.
var markdownRenderer = goldmark.New(
	goldmark.WithExtensions(extension.Table, extension.Strikethrough),
	goldmark.WithRendererOptions(html.WithHardWraps()),
)

// renderMarkdown renders trusted-structure, untrusted-content markdown to HTML.
func renderMarkdown(src string) template.HTML {
	var buf bytes.Buffer
	if err := markdownRenderer.Convert([]byte(src), &buf); err != nil {
		return template.HTML(template.HTMLEscapeString(src))
	}
	return template.HTML(buf.String())
}
