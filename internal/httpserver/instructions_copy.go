package httpserver

import (
	_ "embed"
	"html"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	htmlrenderer "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// Decorate only Markdown AST code nodes; raw administrator HTML stays untouched.
type copyCodeRenderer struct{}
type codeRenderFuncs map[ast.NodeKind]renderer.NodeRendererFunc

func (f codeRenderFuncs) Register(k ast.NodeKind, fn renderer.NodeRendererFunc) { f[k] = fn }

const copyCodeButton = `<button type="button" class="copy-code" aria-label="Copy code"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V4H4v12h4"/></svg><span>Copy</span></button>`

func (copyCodeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	originals := codeRenderFuncs{}
	htmlrenderer.NewRenderer().RegisterFuncs(originals)
	for _, kind := range []ast.NodeKind{ast.KindCodeBlock, ast.KindFencedCodeBlock, ast.KindCodeSpan} {
		render := originals[kind]
		inline := kind == ast.KindCodeSpan
		tag, class := "div", "copy-block"
		if inline {
			tag, class = "span", "copy-inline"
		}
		reg.Register(kind, func(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
			if entering {
				_, _ = w.WriteString("<" + tag + " class=\"" + class + "\" data-markdown-copy>")
				if !inline {
					title := ""
					if fenced, ok := n.(*ast.FencedCodeBlock); ok {
						title = string(fenced.Language(source))
					}
					switch strings.ToLower(title) {
					case "sh", "bash", "shell":
						title = "Shell"
					case "powershell", "pwsh":
						title = "PowerShell"
					}
					_, _ = w.WriteString(`<div class="copy-heading"><span class="copy-title" data-code-title="` + html.EscapeString(title) + `">` + html.EscapeString(title) + `</span>` + copyCodeButton + `</div>`)
				}
			}
			status, err := render(w, source, n, entering)
			if !entering {
				if inline {
					_, _ = w.WriteString(copyCodeButton)
				}
				_, _ = w.WriteString(`<span class="copy-status" hidden role="status" aria-live="polite"></span></` + tag + ">")
			}
			return status, err
		})
	}
}

//go:embed instructions_copy.css
var instructionsCopyStyle string

//go:embed instructions_copy.js
var instructionsCopyScript string
