package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestUserProvidedOpenAISymbol(t *testing.T) {
	data := OpenAISymbol()
	digest := sha256.Sum256([]byte(data))
	if len(data) != 1894 || hex.EncodeToString(digest[:]) != "ca35a5723163b6a766b8b37de9bedd24c2b3ae81d3caa4b9429ccb91ef873cc7" {
		t.Fatal("user-provided SVG bytes changed")
	}
	decoder := xml.NewDecoder(strings.NewReader(data))
	starts := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch item := token.(type) {
		case xml.StartElement:
			starts++
			if item.Name.Space != "http://www.w3.org/2000/svg" {
				t.Fatal("unexpected namespace")
			}
			if starts == 1 && item.Name.Local == "svg" {
				if len(item.Attr) != 2 {
					t.Fatal("unexpected root attributes")
				}
				for _, attr := range item.Attr {
					if attr.Name.Space != "" || !((attr.Name.Local == "xmlns" && attr.Value == "http://www.w3.org/2000/svg") || (attr.Name.Local == "viewBox" && attr.Value == "1.68 1.75 16.65 16.5")) {
						t.Fatal("unexpected root attribute")
					}
				}
			} else if starts == 2 && item.Name.Local == "path" {
				if len(item.Attr) != 1 || item.Attr[0].Name.Local != "d" || item.Attr[0].Name.Space != "" || !strings.Contains(item.Attr[0].Value, "a2.86 2.86 0 0 0-1.105-1.155") {
					t.Fatal("path or requested correction changed")
				}
			} else {
				t.Fatal("unexpected SVG element")
			}
		case xml.EndElement:
		default:
			t.Fatal("unexpected SVG token")
		}
	}
	if starts != 2 {
		t.Fatal("expected one SVG and one path only")
	}
}
