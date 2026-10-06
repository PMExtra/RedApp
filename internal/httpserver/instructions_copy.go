package httpserver

import (
	_ "embed"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	htmlrenderer "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// Decorate only Markdown AST code nodes; raw administrator HTML stays untouched.
type copyCodeRenderer struct{}
type codeRenderFuncs map[ast.NodeKind]renderer.NodeRendererFunc

func (f codeRenderFuncs) Register(k ast.NodeKind, fn renderer.NodeRendererFunc) { f[k] = fn }
func (copyCodeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	originals := codeRenderFuncs{}
	htmlrenderer.NewRenderer().RegisterFuncs(originals)
	for _, kind := range []ast.NodeKind{ast.KindCodeBlock, ast.KindFencedCodeBlock, ast.KindCodeSpan} {
		render := originals[kind]
		tag, class := "div", "copy-block"
		if kind == ast.KindCodeSpan {
			tag, class = "span", "copy-inline"
		}
		reg.Register(kind, func(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
			if entering {
				_, _ = w.WriteString("<" + tag + " class=\"" + class + "\">")
			}
			status, err := render(w, source, n, entering)
			if !entering {
				_, _ = w.WriteString(`<button type="button" class="copy-code" aria-label="Copy code">Copy</button></` + tag + ">")
			}
			return status, err
		})
	}
}

const instructionsCopyStyle = `.copy-block{position:relative}.copy-block pre{padding-right:5.5em}.copy-block>.copy-code{position:absolute;top:6px;right:6px}.copy-inline{position:relative}.copy-inline>.copy-code{position:absolute;bottom:100%;right:0;opacity:0;pointer-events:none;white-space:nowrap}.copy-inline:hover>.copy-code,.copy-inline:focus-within>.copy-code{opacity:1;pointer-events:auto}.copy-code:focus-visible{outline:2px solid #185fbc;outline-offset:2px}.copy-status{position:absolute;width:1px;height:1px;overflow:hidden;clip-path:inset(50%)}`

//go:embed instructions_copy.js
var instructionsCopyScript string
