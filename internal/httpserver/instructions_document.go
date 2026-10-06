package httpserver

import (
	"bytes"
	"fmt"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	goldrenderer "github.com/yuin/goldmark/renderer"
	renderer "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
	"html"
	"net/http"
	"strings"
)

var instructionsMarkdown = goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithRendererOptions(renderer.WithUnsafe(), goldrenderer.WithNodeRenderers(util.Prioritized(copyCodeRenderer{}, 100))))

// A separately served HTML document gives administrator-authored scripts normal
// browser parsing/execution semantics. It carries only public application data.
// This deliberately trusted same-origin document is not an isolation boundary.
func (s *Server) instructionsDocument(w http.ResponseWriter, r *http.Request, origin string) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/apps/") || !strings.HasSuffix(r.URL.Path, "/instructions/document") {
		return false
	}
	if r.Method != http.MethodGet {
		fail(w, 405, "Method not allowed")
		return true
	}
	lang := r.URL.Query().Get("lang")
	if !queryAllowed(r, "lang") || (lang != "en" && lang != "zh-CN") {
		fail(w, 400, "Invalid instructions language")
		return true
	}
	key := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/apps/"), "/instructions/document")
	entry, ok := s.Registry.Lookup(key)
	if !ok {
		fail(w, 404, "Application not found")
		return true
	}
	value, err := s.DB.Instructions(entry.UID)
	if err != nil {
		fail(w, 503, "Instructions unavailable")
		return true
	}
	content := value.En
	if lang == "zh-CN" {
		content = value.ZhCN
	}
	var commands strings.Builder
	for _, item := range entry.Descriptor.Installers {
		label := "Shell"
		command := "curl -fsSL '" + origin + "/" + key + "/" + item.File + "' | " + item.Shell
		if item.Shell == "powershell" {
			label = "PowerShell"
			command = "irm '" + origin + "/" + key + "/" + item.File + "' | iex"
		}
		fmt.Fprintf(&commands, "<section><h3>%s</h3><pre tabindex=\"0\"><code>%s</code></pre><button type=\"button\" onclick=\"navigator.clipboard?.writeText(this.previousElementSibling.textContent).then(()=>this.textContent='✓').catch(()=>{})\">%s</button></section>\n", label, html.EscapeString(command), map[string]string{"en": "Copy", "zh-CN": "复制"}[lang])
	}
	// Preserve the legacy HTML command block only for existing custom documents.
	content = strings.ReplaceAll(content, "{{install_commands}}", commands.String())
	var body bytes.Buffer
	if err = instructionsMarkdown.Convert([]byte(content), &body); err != nil {
		fail(w, 503, "Instructions unavailable")
		return true
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Content-Security-Policy", "default-src * data: blob:; script-src * 'unsafe-inline' 'unsafe-eval' data: blob:; style-src * 'unsafe-inline'; frame-ancestors 'self'")
	fmt.Fprintf(w, `<!doctype html><html lang="%s"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><base target="_top"><style>body{font:15px/1.65 system-ui,sans-serif;margin:0;padding:1px;color:#27364b;background:white}h1,h2,h3{line-height:1.3}pre{overflow:auto;background:#f4f6f9;padding:16px;border-radius:8px}img,video{max-width:100%%}button{border:1px solid #cbd5e1;background:white;border-radius:6px;padding:8px 16px;cursor:pointer}table{border-collapse:collapse}td,th{border:1px solid #ddd;padding:8px}a{color:#185fbc}%s</style></head><body>%s<script>%s</script></body></html>`, lang, instructionsCopyStyle, s.interpolateInstructionVariables(entry, body.String(), origin, lang), instructionsCopyScript)
	return true
}
