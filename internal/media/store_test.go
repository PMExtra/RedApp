package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store, dir
}

func raster(t *testing.T, format string) []byte {
	t.Helper()
	pixels := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	pixels.Set(1, 1, color.NRGBA{R: 225, G: 80, A: 192})
	var output bytes.Buffer
	var err error
	if format == "jpeg" {
		err = jpeg.Encode(&output, pixels, nil)
	} else {
		err = png.Encode(&output, pixels)
	}
	if err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestRasterNormalizationAndContentAddress(t *testing.T) {
	store, _ := testStore(t)
	for _, format := range []string{"png", "jpeg"} {
		t.Run(format, func(t *testing.T) {
			input := append(raster(t, format), []byte("discarded upload metadata")...)
			path, err := store.Put(bytes.NewReader(input))
			if err != nil {
				t.Fatal(err)
			}
			f, mime, err := store.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			content, err := io.ReadAll(f)
			if err != nil {
				t.Fatal(err)
			}
			if mime != "image/"+format || bytes.Contains(content, []byte("discarded upload metadata")) {
				t.Fatalf("unexpected MIME or non-normalized content: %s", mime)
			}
			decoded, actual, err := image.Decode(bytes.NewReader(content))
			if err != nil || actual != format || decoded.Bounds().Dx() != 3 || decoded.Bounds().Dy() != 2 {
				t.Fatalf("invalid normalized image: %s %v", actual, err)
			}
			digest := sha256.Sum256(content)
			if !strings.HasPrefix(path, PublicPrefix+hex.EncodeToString(digest[:])) {
				t.Fatalf("path does not name normalized content: %s", path)
			}
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				t.Fatalf("file is not seekable: %v", err)
			}
			again, err := store.Put(bytes.NewReader(input))
			if err != nil || again != path {
				t.Fatalf("identical upload did not reuse immutable path: %s %v", again, err)
			}
		})
	}
}

func TestRejectMalformedLargeAndDecodeBombImages(t *testing.T) {
	store, dir := testStore(t)
	pngData := raster(t, "png")
	oversizedHeader := func(width, height uint32) []byte {
		data := bytes.Clone(pngData)
		binary.BigEndian.PutUint32(data[16:20], width)
		binary.BigEndian.PutUint32(data[20:24], height)
		binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
		return data
	}
	for _, test := range []struct {
		name string
		data []byte
		want error
	}{
		{"unsupported", []byte("GIF89a"), ErrInvalidIcon},
		{"empty", nil, ErrInvalidIcon},
		{"truncated", pngData[:40], ErrInvalidIcon},
		{"upload limit", bytes.Repeat([]byte("x"), MaxBytes+1), ErrTooLarge},
		{"dimension bomb", oversizedHeader(1<<30, 1), ErrTooLarge},
		{"pixel bomb", oversizedHeader(4096, 4096), ErrTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.Put(bytes.NewReader(test.data)); !errors.Is(err, test.want) {
				t.Fatalf("got %v, want %v", err, test.want)
			}
		})
	}
	entries, err := os.ReadDir(filepath.Join(dir, "objects", "icons"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid uploads left files behind: %v %v", entries, err)
	}
}

func TestSVGStaticSubset(t *testing.T) {
	store, _ := testStore(t)
	input := `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 64 64"><!-- discarded --><title>A &amp; B</title><defs><linearGradient id="paint"><stop offset="0%" stop-color="#ff0"/><stop offset="100%" stop-color="rgb(0, 0, 255)"/></linearGradient><clipPath id="clip"><circle cx="32" cy="32" r="30"/></clipPath></defs><g clip-path="url(#clip)" transform="translate(0 1)"><path fill="url(#paint)" d="M0 0 L64 0 L64 64 Z"/></g></svg>`
	path, err := store.Put(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	f, mime, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	content, err := io.ReadAll(f)
	if err != nil || mime != "image/svg+xml" || !strings.HasSuffix(path, ".svg") {
		t.Fatalf("invalid served SVG: %s %s %v", path, mime, err)
	}
	if bytes.Contains(content, []byte("discarded")) || bytes.Contains(content, []byte("<?xml")) || !bytes.Contains(content, []byte("A &amp; B")) {
		t.Fatalf("SVG was not normalized: %s", content)
	}
	// Saved XML must be stable and satisfy exactly the same parser on reread.
	again, err := store.Put(bytes.NewReader(content))
	if err != nil || again != path {
		t.Fatalf("normalized SVG is not stable: %s %v", again, err)
	}
}

func TestRejectActiveOrUnsupportedSVG(t *testing.T) {
	store, _ := testStore(t)
	for name, input := range map[string]string{
		"script":            `<svg><script>alert(1)</script></svg>`,
		"event":             `<svg onload="alert(1)"></svg>`,
		"namespace event":   `<svg xmlns:x="http://www.w3.org/2000/svg" x:onload="alert(1)"></svg>`,
		"foreign object":    `<svg><foreignObject><p>html</p></foreignObject></svg>`,
		"style element":     `<svg><style>path{fill:url(https://example.test/a)}</style></svg>`,
		"style attribute":   `<svg style="background:url(https://example.test/a)"></svg>`,
		"external paint":    `<svg><path fill="url(https://example.test/a)" d="M0 0"/></svg>`,
		"escaped paint":     `<svg><path fill="url(&#104;ttps://example.test/a)" d="M0 0"/></svg>`,
		"data url":          `<svg><image href="data:image/svg+xml;base64,PHN2Zz4="/></svg>`,
		"javascript href":   `<svg><a href="javascript:alert(1)"><path d="M0 0"/></a></svg>`,
		"use external":      `<svg><use href="https://example.test/a.svg#x"/></svg>`,
		"animation":         `<svg><animate attributeName="href"/></svg>`,
		"doctype":           `<!DOCTYPE svg><svg/>`,
		"entity definition": `<!DOCTYPE svg [<!ENTITY attack "x">]><svg><title>&attack;</title></svg>`,
		"unknown entity":    `<svg><title>&attack;</title></svg>`,
		"xml stylesheet":    `<?xml-stylesheet href="https://example.test/a"?><svg/>`,
		"html namespace":    `<svg xmlns="http://www.w3.org/1999/xhtml"/>`,
		"namespace reset":   `<svg xmlns="http://www.w3.org/2000/svg"><g xmlns=""/></svg>`,
		"unknown local id":  `<svg><path fill="url(#missing)"/></svg>`,
		"duplicate id":      `<svg><path id="a"/><path id="a"/></svg>`,
		"duplicate attr":    `<svg width="10" width="20"/>`,
		"multiple roots":    `<svg/><svg/>`,
		"nested svg":        `<svg><svg/></svg>`,
		"mismatched tags":   `<svg><g></svg>`,
		"trailing text":     `<svg/>payload`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.Put(strings.NewReader(input)); !errors.Is(err, ErrInvalidIcon) {
				t.Fatalf("accepted active or unsupported SVG: %v", err)
			}
		})
	}
}

func TestSVGBounds(t *testing.T) {
	store, _ := testStore(t)
	for name, input := range map[string]string{
		"depth":      "<svg>" + strings.Repeat("<g>", 32) + strings.Repeat("</g>", 32) + "</svg>",
		"nodes":      "<svg>" + strings.Repeat("<path/>", 4096) + "</svg>",
		"dimension":  `<svg width="4097" height="1"/>`,
		"pixels":     `<svg width="4096" height="4096"/>`,
		"percentage": `<svg width="10000%"/>`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.Put(strings.NewReader(input)); !errors.Is(err, ErrTooLarge) {
				t.Fatalf("got %v, want size rejection", err)
			}
		})
	}
}

func TestOpenRejectsPathsAndSymlinks(t *testing.T) {
	store, dir := testStore(t)
	path, err := store.Put(strings.NewReader(`<svg><circle r="4"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		filepath.Base(path), path + "?x=1", path + "/extra", strings.ToUpper(path),
		PublicPrefix + "../" + filepath.Base(path), PublicPrefix + "%2e%2e/test.svg", "/tmp/test.svg",
	} {
		if f, _, err := store.Open(bad); err == nil {
			f.Close()
			t.Errorf("opened noncanonical path %q", bad)
		}
	}
	target := filepath.Join(t.TempDir(), "external.svg")
	if err := os.WriteFile(target, []byte("external data"), 0600); err != nil {
		t.Fatal(err)
	}
	stored := filepath.Join(dir, "objects", "icons", filepath.Base(path))
	if err := os.Remove(stored); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, stored); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if f, _, err := store.Open(path); err == nil {
		f.Close()
		t.Fatal("followed icon symlink")
	}
	if _, err := store.Put(strings.NewReader(`<svg><circle r="4"/></svg>`)); err == nil {
		t.Fatal("overwrote or reused icon symlink")
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "external data" {
		t.Fatal("external file was changed")
	}
}

func TestNewRejectsSymlinkDirectories(t *testing.T) {
	for _, component := range []string{"data", "objects", "icons"} {
		t.Run(component, func(t *testing.T) {
			dir := t.TempDir()
			target := t.TempDir()
			link := dir
			if component == "data" {
				link = filepath.Join(dir, "linked-data")
				dir = link
			} else if component == "objects" {
				link = filepath.Join(dir, "objects")
			} else {
				if err := os.Mkdir(filepath.Join(dir, "objects"), 0700); err != nil {
					t.Fatal(err)
				}
				link = filepath.Join(dir, "objects", "icons")
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			if store, err := New(dir); err == nil {
				store.Close()
				t.Fatal("accepted symlink directory")
			}
			entries, err := os.ReadDir(target)
			if err != nil || len(entries) != 0 {
				t.Fatal("wrote through directory symlink")
			}
		})
	}
}

func TestConcurrentUploadsPublishOneCompleteFile(t *testing.T) {
	store, dir := testStore(t)
	input := raster(t, "png")
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			path, err := store.Put(bytes.NewReader(input))
			if err != nil {
				t.Error(err)
				return
			}
			f, _, err := store.Open(path)
			if err != nil {
				t.Error(err)
				return
			}
			defer f.Close()
			if _, err := png.Decode(f); err != nil {
				t.Errorf("published incomplete file: %v", err)
			}
		})
	}
	wg.Wait()
	entries, err := os.ReadDir(filepath.Join(dir, "objects", "icons"))
	if err != nil || len(entries) != 1 || strings.HasPrefix(entries[0].Name(), ".upload-") {
		t.Fatalf("unexpected stored files: %v %v", entries, err)
	}
}
