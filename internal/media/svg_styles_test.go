package media

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestExporterSVGUpload(t *testing.T) {
	input, err := os.ReadFile("testdata/hillstone-networks-logo-en.svg")
	if err != nil {
		t.Fatal(err)
	}
	got, err := normalizeSVG(input)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(got, []byte(`fill="#003285"`)) != 17 || bytes.Contains(got, []byte("style")) || bytes.Contains(got, []byte("class=")) || bytes.Contains(got, []byte("data-name")) {
		t.Fatalf("lost paint or retained CSS/metadata: %s", got)
	}
	again, err := normalizeSVG(got)
	if err != nil || !bytes.Equal(got, again) {
		t.Fatalf("normalization unstable: %v", err)
	}
}

func TestSVGClassCascadeAndLocalPaint(t *testing.T) {
	input := `<svg><path class="later first" fill="red" d="M0 0"/><style>.first{fill:blue;stroke:#fff}.later{fill:green}.first{fill:url(#paint)}</style><defs><linearGradient id="paint"><stop offset="0" stop-color="red"/></linearGradient></defs></svg>`
	got, err := normalizeSVG([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte(`fill="url(#paint)"`)) || !bytes.Contains(got, []byte(`stroke="#fff"`)) {
		t.Fatalf("incorrect cascade: %s", got)
	}
}

func TestRejectUnsafeSVGStyles(t *testing.T) {
	cases := map[string]string{
		"external paint":     `.a{fill:url(https://example.test/a)}`,
		"escaped URL":        `.a{fill:u\72l(https://example.test/a)}`,
		"import":             `@import "https://example.test/a";`,
		"expression":         `.a{fill:expression(alert(1))}`,
		"unknown property":   `.a{background:red}`,
		"multiword property": `.a{stroke stroke-width:1}`,
		"empty property":     `.a{:1}`,
		"custom property":    `.a{--paint:red}`,
		"important":          `.a{fill:red!important}`,
		"complex selector":   `svg .a{fill:red}`,
		"compound selector":  `.a.b{fill:red}`,
		"selector list":      `.a,.b{fill:red}`,
		"pseudo selector":    `.a:hover{fill:red}`,
		"at rule":            `@media all{.a{fill:red}}`,
		"unclosed":           `.a{fill:red`,
		"unknown reference":  `.a{fill:url(#missing)}`,
	}
	for name, css := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeSVG([]byte(`<svg><style>` + css + `</style><path class="a"/></svg>`)); err == nil {
				t.Fatal("accepted unsafe/unsupported CSS")
			}
		})
	}
	for _, input := range []string{
		`<svg><style onload="alert(1)">.a{fill:red}</style></svg>`,
		`<svg><style><script/></style></svg>`,
		`<svg><g><style>.a{fill:red}</style></g></svg>`,
		`<svg><path class="unknown"/></svg>`,
		`<svg><path class="a" class="b"/></svg>`,
		`<svg xmlns:x="urn:evil"><path x:data-name="x"/></svg>`,
		`<svg data-name="x" onload="alert(1)"/>`,
		`<svg><style>.a{fill:red}</style><path class="a" onclick="alert(1)"/></svg>`,
	} {
		if _, err := normalizeSVG([]byte(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestSVGStyleBounds(t *testing.T) {
	for name, input := range map[string]string{
		"rules":                 `<svg><style>` + strings.Repeat(`.a{fill:red}`, 257) + `</style></svg>`,
		"rules across elements": `<svg>` + strings.Repeat(`<style>.a{fill:red}</style>`, 257) + `</svg>`,
		"declarations":          `<svg><style>.a{` + strings.Repeat(`fill:red;`, 65) + `}</style></svg>`,
		"expanded bytes":        `<svg><style>.a{stroke-dasharray:` + strings.Repeat(`1 `, 1024) + `}</style>` + strings.Repeat(`<path class="a"/>`, 2200) + `</svg>`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeSVG([]byte(input)); !errors.Is(err, ErrTooLarge) {
				t.Fatalf("expected bounded rejection, got %v", err)
			}
		})
	}
}
