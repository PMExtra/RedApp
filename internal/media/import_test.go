package media

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"testing"
)

func TestStaticExchangeJPEGRoundtripPreservesOriginalBytesAndHash(t *testing.T) {
	stage, _ := testStore(t)
	destination, _ := testStore(t)
	pixels := image.NewRGBA(image.Rect(0, 0, 17, 13))
	for y := 0; y < 13; y++ {
		for x := 0; x < 17; x++ {
			pixels.Set(x, y, color.RGBA{uint8(x * 15), uint8(y * 19), uint8((x + y) * 8), 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, pixels, &jpeg.Options{Quality: 83}); err != nil {
		t.Fatal(err)
	}
	original := encoded.Bytes()
	wantPath, err := StaticImagePath(original, ".jpg")
	if err != nil {
		t.Fatal(err)
	}
	path, err := stage.PutStatic(original, ".jpg")
	if err != nil || path != wantPath {
		t.Fatal("staging changed JPEG identity", path, err)
	}
	if err := destination.ApplyImages(map[string][]byte{path: original}, func(string) bool { return true }, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	f, _, err := destination.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	actual, err := io.ReadAll(f)
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatal("JPEG re-encoded during publication", err)
	}
	actualPath, err := StaticImagePath(actual, ".jpg")
	if err != nil || actualPath != wantPath {
		t.Fatal("published JPEG hash changed", actualPath, err)
	}
}

func TestStaticExchangeIdentityAndRollbackOnlyNewUnreferenced(t *testing.T) {
	s, _ := testStore(t)
	existing := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><path d="M0 0L1 1" /></svg>`)
	old, e := s.PutStatic(existing, ".svg")
	if e != nil {
		t.Fatal(e)
	}
	fresh := []byte("<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 20 20\">\n<path d=\"M0 0L2 2\" />\n</svg>\n")
	newPath, e := StaticImagePath(fresh, ".svg")
	if e != nil {
		t.Fatal(e)
	}
	fail := errors.New("publication failed")
	e = s.ApplyImages(map[string][]byte{old: existing, newPath: fresh}, func(string) bool { return false }, func() error { return fail })
	if !errors.Is(e, fail) {
		t.Fatal(e)
	}
	if f, _, e := s.Open(newPath); !errors.Is(e, os.ErrNotExist) {
		if f != nil {
			f.Close()
		}
		t.Fatal("new image retained", e)
	}
	f, _, e := s.Open(old)
	if e != nil {
		t.Fatal("existing image removed", e)
	}
	f.Close()
	e = s.ApplyImages(map[string][]byte{newPath: fresh}, func(string) bool { return true }, func() error { return fail })
	if !errors.Is(e, fail) {
		t.Fatal(e)
	}
	f, _, e = s.Open(newPath)
	if e != nil {
		t.Fatal("referenced new image removed", e)
	}
	body, _ := io.ReadAll(f)
	f.Close()
	if !bytes.Equal(body, fresh) {
		t.Fatal("static bytes changed")
	}
}
