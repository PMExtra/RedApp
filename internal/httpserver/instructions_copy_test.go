package httpserver

import (
	"bytes"
	"strings"
	"testing"
)

func TestInstructionsCopyOnlyMarkdown(t *testing.T) {
	source := "```sh\n  first\n\nsecond  \n```\n\n`inline`\n\n    indented\n    code\n\n<pre><code>raw block</code></pre>\n\n<code>raw inline</code>"
	var body bytes.Buffer
	if err := instructionsMarkdown.Convert([]byte(source), &body); err != nil {
		t.Fatal(err)
	}
	rendered := body.String()
	if strings.Count(rendered, `class="copy-code"`) != 3 {
		t.Fatal(rendered)
	}
	for _, want := range []string{"  first\n\nsecond  \n", "<pre><code>raw block</code></pre>", "<code>raw inline</code>", `class="copy-inline"`} {
		if !strings.Contains(rendered, want) {
			t.Fatal(want, rendered)
		}
	}
}

func TestMarkdownCodeTitlesAndCustomHeadings(t *testing.T) {
	for language, title := range map[string]string{"bash": "Shell", "powershell": "PowerShell", "python": "python", "": ""} {
		var body bytes.Buffer
		source := "### Custom title\n\n```" + language + "\necho hello\n```\n"
		if err := instructionsMarkdown.Convert([]byte(source), &body); err != nil {
			t.Fatal(err)
		}
		rendered := body.String()
		if !strings.Contains(rendered, "<h3>Custom title</h3>") || !strings.Contains(rendered, `data-code-title="`+title+`"`) || !strings.Contains(rendered, `class="copy-heading"`) {
			t.Fatal(rendered)
		}
	}
}
