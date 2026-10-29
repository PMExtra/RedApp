package media

import (
	"bytes"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
)

func svgDoc(body string) string {
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">` + body + `</svg>`
}

func storedBytes(t *testing.T, store *Store, path string) []byte {
	t.Helper()
	f, mime, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	content, err := io.ReadAll(f)
	if err != nil || mime != "image/svg+xml" || !strings.HasSuffix(path, ".svg") {
		t.Fatalf("invalid stored SVG: %s %s %v", path, mime, err)
	}
	return content
}

func TestSVGWithoutActiveContentIsStoredByteForByte(t *testing.T) {
	store, _ := testStore(t)
	inputs := map[string]string{
		"exporter metadata": "<?xml version=\"1.0\"?>\n<!-- kept -->\n" + `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:sodipodi="http://sodipodi.sourceforge.net/DTD/sodipodi-0.dtd" id="图层_1" x="0px" xml:space="preserve" style="enable-background:new 0 0 10 10;" viewBox="0 0 10 10">` +
			`<sodipodi:namedview pagecolor="#fff"/><metadata><title>A &amp; B</title></metadata>` +
			`<style type="text/css"><![CDATA[.a{fill:url(#paint)} .b { stroke: url( '#paint' ) }]]></style>` +
			`<defs><linearGradient id="paint"><stop offset="0" stop-color="red"/></linearGradient><path id="p" d="M0 0h1"/></defs>` +
			`<use xlink:href="#p" class="a b"/><a href="#p"><circle r="1" filter="url(#paint)"/></a>` +
			`<image width="1" height="1" href="data:image/png;base64,iVBORw0KGgo="/></svg>`,
	}
	for _, name := range []string{"hillstone-networks-logo-en.svg", "hillstone-networks-logo-cn.svg"} {
		raw, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		inputs[name] = string(raw)
	}
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			path, err := store.Put(strings.NewReader(input), false)
			if err != nil {
				t.Fatal(err)
			}
			if got := storedBytes(t, store, path); string(got) != strings.TrimSpace(input) {
				t.Fatalf("SVG was changed:\n%s", got)
			}
			if _, err := ValidateStaticImage([]byte(input), ".svg"); err != nil {
				t.Fatalf("static validation rejected a clean SVG: %v", err)
			}
		})
	}
}

func TestSVGActiveContentRequiresConsent(t *testing.T) {
	for name, test := range map[string]struct {
		body, want string
		removed    []string
	}{
		"script":              {`<script>alert(1)</script><path d="M0 0"/>`, `<path d="M0 0"/>`, []string{"<script>"}},
		"prefixed script":     {`<s:script xmlns:s="http://www.w3.org/2000/svg">alert(1)</s:script>`, ``, []string{"<s:script>"}},
		"nested in removed":   {`<foreignObject><p xmlns="http://www.w3.org/1999/xhtml" onclick="x"><script>x</script></p></foreignObject>`, ``, []string{"<foreignObject>"}},
		"html element":        {`<h:img xmlns:h="http://www.w3.org/1999/xhtml" src="https://example.test/a"/><g/>`, `<g/>`, []string{"<h:img>"}},
		"animation":           {`<a href="#x"><set attributeName="href" to="javascript:alert(1)"/></a><g id="x"/>`, `<a href="#x"></a><g id="x"/>`, []string{"<set>"}},
		"events":              {`<g onload="a()" ONCLICK="b()" fill="red"><path onmouseover="c()"/></g>`, `<g fill="red"><path/></g>`, []string{"<g onload>", "<g ONCLICK>", "<path onmouseover>"}},
		"external href":       {`<use xmlns:xlink="http://www.w3.org/1999/xlink" xlink:href="https://example.test/a.svg#x"/><a href=" javascript:alert(1)"/>`, `<use xmlns:xlink="http://www.w3.org/1999/xlink"/><a/>`, []string{"<use xlink:href>", "<a href>"}},
		"embedded svg":        {`<image href="data:image/svg+xml;base64,PHN2Zz4="/><feImage href="data:image/png;base64,AA=="/>`, `<image/><feImage/>`, []string{"<image href>", "<feImage href>"}},
		"external paint":      {`<path fill="url(https://example.test/a)" d="M0 0"/><path stroke="url( 'http://x' )"/>`, `<path d="M0 0"/><path/>`, []string{"<path fill>", "<path stroke>"}},
		"escaped paint":       {`<path fill="u\72l(https://example.test/a)"/>`, `<path/>`, []string{"<path fill>"}},
		"entity paint":        {`<path fill="url(&#104;ttps://example.test/a)"/>`, `<path/>`, []string{"<path fill>"}},
		"style attribute":     {`<g style="fill:red;background:image-set('a.png' 1x)"/>`, `<g/>`, []string{"<g style>"}},
		"style import":        {`<style>@import "https://example.test/a.css";</style><g/>`, `<g/>`, []string{"<style>"}},
		"style font":          {`<style>@font-face{src:local(x)}</style>`, ``, []string{"<style>"}},
		"style url":           {`<style>.a{fill:url(https://example.test/a)}</style>`, ``, []string{"<style>"}},
		"style element child": {`<style><g/>.a{}</style>`, ``, []string{"<style>"}},
		"xml base":            {`<g xml:base="https://example.test/"/>`, `<g/>`, []string{"<g xml:base>"}},
		"repeated":            {`<path onclick="a"/><path onclick="b"/>`, `<path/><path/>`, []string{"<path onclick> ×2"}},
	} {
		t.Run(name, func(t *testing.T) {
			store, _ := testStore(t)
			input := svgDoc(test.body)
			_, err := store.Put(strings.NewReader(input), false)
			var required *SanitizeRequiredError
			if !errors.As(err, &required) || !slices.Equal(required.Removed, test.removed) {
				t.Fatalf("got %v, want removal of %q", err, test.removed)
			}
			if _, err := ValidateStaticImage([]byte(input), ".svg"); !errors.Is(err, ErrInvalidIcon) {
				t.Fatalf("static validation accepted active content: %v", err)
			}
			path, err := store.Put(strings.NewReader(input), true)
			if err != nil {
				t.Fatal(err)
			}
			got := storedBytes(t, store, path)
			if string(got) != svgDoc(test.want) {
				t.Fatalf("sanitized to\n%s\nwant\n%s", got, svgDoc(test.want))
			}
			if again, err := store.Put(bytes.NewReader(got), false); err != nil || again != path {
				t.Fatalf("sanitized SVG is not clean: %s %v", again, err)
			}
		})
	}
}

func TestSVGProcessingInstructionsAreRemoved(t *testing.T) {
	input := `<?xml version="1.0"?><?xml-stylesheet href="https://example.test/a.css"?>` + svgDoc(``)
	got, removed, err := sanitizeSVG([]byte(input))
	if err != nil || string(got) != `<?xml version="1.0"?>`+svgDoc(``) || !slices.Equal(removed, []string{"<?xml-stylesheet?>"}) {
		t.Fatalf("got %s %q %v", got, removed, err)
	}
}

func TestRejectMalformedSVG(t *testing.T) {
	store, _ := testStore(t)
	for name, input := range map[string]string{
		"doctype":           `<!DOCTYPE svg>` + svgDoc(``),
		"entity definition": `<!DOCTYPE svg [<!ENTITY attack "x">]>` + svgDoc(`<title>&attack;</title>`),
		"unknown entity":    svgDoc(`<title>&attack;</title>`),
		"no namespace":      `<svg/>`,
		"html root":         `<svg xmlns="http://www.w3.org/1999/xhtml"/>`,
		"undeclared prefix": svgDoc(`<x:g/>`),
		"undeclared attr":   svgDoc(`<g x:href="#a"/>`),
		"duplicate attr":    svgDoc(`<g fill="red" fill="blue"/>`),
		"multiple roots":    svgDoc(``) + svgDoc(``),
		"mismatched tags":   svgDoc(`<g></svg>`),
		"unclosed":          `<svg xmlns="http://www.w3.org/2000/svg"><g>`,
		"trailing text":     svgDoc(``) + `payload`,
		"late declaration":  svgDoc(``) + `<?xml version="1.0"?>`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.Put(strings.NewReader(input), true); !errors.Is(err, ErrInvalidIcon) {
				t.Fatalf("got %v, want rejection", err)
			}
		})
	}
}

func TestSanitizeSummaryIsBounded(t *testing.T) {
	var body strings.Builder
	for i := range 30 {
		body.WriteString(`<g on` + strings.Repeat("x", i+1) + `="a"/>`)
	}
	_, removed, err := sanitizeSVG([]byte(svgDoc(body.String())))
	if err != nil || len(removed) != 21 || removed[20] != "… +10" {
		t.Fatalf("got %d items %v", len(removed), err)
	}
}
