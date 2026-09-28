package web

import (
	"strings"
	"testing"
)

func TestRenderMarkdown(t *testing.T) {
	got := string(renderMarkdown("**Why it matters:**\n- one\n- two\n\n```sql\nCREATE INDEX CONCURRENTLY idx ON t (a);\n```\nSee [query](/queries/42)."))
	for _, want := range []string{"<strong>Why it matters:</strong>", "<li>one</li>", "<pre><code class=\"language-sql\">CREATE INDEX CONCURRENTLY", `<a href="/queries/42">query</a>`} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered markdown missing %q:\n%s", want, got)
		}
	}
}

// Descriptions embed query texts and object names from the monitored
// database, so markup in them must not reach the page.
func TestRenderMarkdownIsSafe(t *testing.T) {
	got := string(renderMarkdown("Query: `SELECT '<script>alert(1)</script>'`\n\n<img src=x onerror=alert(1)>\n\n[click](javascript:alert(1))"))
	for _, bad := range []string{"<script>", "<img", "onerror", "javascript:"} {
		if strings.Contains(got, bad) {
			t.Errorf("unsafe %q rendered:\n%s", bad, got)
		}
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("inline code should be escaped:\n%s", got)
	}
}
