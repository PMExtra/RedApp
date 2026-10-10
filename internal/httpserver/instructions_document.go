package httpserver

import (
	"bytes"
	_ "embed"
	"fmt"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	goldrenderer "github.com/yuin/goldmark/renderer"
	renderer "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"

	"net/http"
)

var instructionsMarkdown = goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithRendererOptions(renderer.WithUnsafe(), goldrenderer.WithNodeRenderers(util.Prioritized(copyCodeRenderer{}, 100))))

//go:embed instructions_height.js
var instructionsHeightScript string

//go:embed instructions_document.css
var instructionsDocumentStyle string

// The CSP sandbox gives the document an opaque origin even when opened directly,
// so its scripts cannot use the administrator session. The embedding iframe
// carries the same sandbox flags.
const instructionsDocumentPolicy = "sandbox allow-scripts allow-popups allow-popups-to-escape-sandbox allow-top-navigation-by-user-activation; default-src * data: blob:; script-src * 'unsafe-inline' 'unsafe-eval' data: blob:; style-src * 'unsafe-inline'; frame-ancestors 'self'"

// getInstructionsDocument renders the usage instructions of a published
// application as a separate HTML document, so administrator-authored scripts
// get normal browser parsing and execution, including external resources. It
// carries only public application data.
func (s *Server) getInstructionsDocument(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	if lang != "en" && lang != "zh-CN" {
		s.fail(w, r, codeInvalidQuery, nil, "lang must be en or zh-CN")
		return
	}
	// The opaque-origin document cannot read the page's theme, so the iframe passes it.
	theme := r.URL.Query().Get("theme")
	if theme == "" {
		theme = "light"
	}
	if theme != "light" && theme != "dark" {
		s.fail(w, r, codeInvalidQuery, nil, "theme must be light or dark")
		return
	}
	entry, ok := s.publishedApp(w, r)
	if !ok {
		return
	}
	value, err := s.store.Instructions(entry.UID)
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	content := value.En
	if lang == "zh-CN" {
		content = value.ZhCN
	}
	var body bytes.Buffer
	if err = instructionsMarkdown.Convert([]byte(content), &body); err != nil {
		s.fail(w, r, codeInternalError, err, "Instructions could not be rendered")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Content-Security-Policy", instructionsDocumentPolicy)
	fmt.Fprintf(w, `<!doctype html><html lang="%s" data-theme="%s"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><base target="_top"><style>%s%s</style></head><body>%s<script>%s</script><script>%s</script></body></html>`, lang, theme, instructionsDocumentStyle, instructionsCopyStyle, s.interpolateInstructionVariables(entry, body.String(), s.publicURL(r), lang), instructionsCopyScript, instructionsHeightScript)
}
