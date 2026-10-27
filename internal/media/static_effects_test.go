package media

import (
	"strings"
	"testing"
)

func TestStaticEffectsRemainUnavailableToUploads(t *testing.T) {
	raw := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><mask id="m" mask-type="alpha" maskUnits="userSpaceOnUse" x="0" y="0" width="100" height="100"><path d="M0 0h100v100z" fill="white"/></mask><g mask="url(#m)" filter="url(#f)" style="mix-blend-mode:overlay"><path d="M0 0h100v100z"/></g><defs><filter id="f" x="-8" y="-8" width="116" height="116" filterUnits="userSpaceOnUse" color-interpolation-filters="sRGB"><feFlood flood-opacity="0" result="BackgroundImageFix"/><feColorMatrix in="SourceAlpha" type="matrix" values="0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 127 0"/><feOffset/><feGaussianBlur stdDeviation="4.16667"/><feBlend mode="overlay" in="SourceGraphic" in2="BackgroundImageFix" result="shape"/></filter></defs></svg>`)
	if mime, err := ValidatePresetImage(raw, ".svg"); err != nil || mime != "image/svg+xml" {
		t.Fatal(mime, err)
	}
	if _, _, err := normalize(raw); err == nil {
		t.Fatal("upload allowlist broadened")
	}
	if _, err := ValidateStaticImage(raw, ".svg"); err == nil {
		t.Fatal("configuration import allowlist broadened")
	}
	for _, tc := range []struct{ old, new string }{
		{`url(#m)`, `url(https://example.com/m)`},
		{`url(#f)`, `url(#missing)`},
		{`mix-blend-mode:overlay`, `mix-blend-mode:overlay;background:url(https://example.com)`},
		{`stdDeviation="4.16667"`, `stdDeviation="1000"`},
		{`width="116"`, `width="10000"`},
		{`<feOffset/>`, `<feImage href="https://example.com/a"/>`},
		{`<feOffset/>`, `<script>alert(1)</script>`},
		{`<feOffset/>`, strings.Repeat(`<feOffset/>`, 40)},
		{`<feOffset/>`, `<g filter="url(#f)"/>`},
	} {
		changed := strings.Replace(string(raw), tc.old, tc.new, 1)
		if _, err := ValidatePresetImage([]byte(changed), ".svg"); err == nil {
			t.Fatalf("accepted unsafe effect %s", tc.new)
		}
	}
}
