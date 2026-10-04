// Package media stores validated, inert images for application and vendor icons.
package media

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	MaxBytes       = 2 << 20
	MaxStoredBytes = 4 << 20
	MaxDimension   = 4096
	MaxPixels      = 4 << 20
	PublicPrefix   = "/assets/icons/"
)

var (
	ErrInvalidIcon = errors.New("invalid or unsupported icon")
	ErrTooLarge    = errors.New("icon exceeds size or dimension limit")
)

// Store owns a directory handle and must be closed after its users have stopped.
// The caller must have already validated and locked the RedApp data directory.
type Store struct {
	root *os.Root
	mu   sync.Mutex
}

func New(dir string) (*Store, error) {
	info, err := os.Lstat(filepath.Clean(dir))
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("icon data directory must be a real directory")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("icon data directory changed while opening")
	}
	objects, err := childDirectory(root, "objects")
	if err != nil {
		return nil, err
	}
	defer objects.Close()
	icons, err := childDirectory(objects, "icons")
	if err != nil {
		return nil, err
	}
	return &Store{root: icons}, nil
}

func childDirectory(parent *os.Root, name string) (*os.Root, error) {
	err := parent.Mkdir(name, 0700)
	created := err == nil
	if err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("icon storage path must be a real directory")
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		child.Close()
		return nil, errors.New("icon directory changed while opening")
	}
	if created {
		if err := syncDirectory(parent); err != nil {
			child.Close()
			return nil, err
		}
	}
	return child, nil
}

func syncDirectory(root *os.Root) error {
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func (s *Store) Close() error { return s.root.Close() }

// Put accepts image bytes, never a caller-provided filename. The returned path
// is immutable and names the normalized content, not the original upload.
func (s *Store) Put(reader io.Reader) (string, error) {
	input, err := io.ReadAll(io.LimitReader(reader, MaxBytes+1))
	if err != nil {
		return "", err
	}
	if len(input) > MaxBytes {
		return "", ErrTooLarge
	}
	content, extension, err := normalize(input)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(content)
	name := hex.EncodeToString(digest[:]) + extension
	publicPath := PublicPrefix + name
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.root.Lstat(name); err == nil {
		f, _, err := s.Open(publicPath)
		if err != nil {
			return "", err
		}
		existing, readErr := io.ReadAll(io.LimitReader(f, MaxStoredBytes+1))
		closeErr := f.Close()
		if readErr != nil {
			return "", readErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if !bytes.Equal(existing, content) {
			return "", errors.New("stored icon does not match its content address")
		}
		return publicPath, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	temporary := ".upload-" + hex.EncodeToString(nonce[:])
	f, err := s.root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer s.root.Remove(temporary)
	if _, err := f.Write(content); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := s.root.Rename(temporary, name); err != nil {
		return "", err
	}
	// A subsequent database reference must not become durable before the file's
	// directory entry does. Failed uploads never remove a previously stored icon.
	if err := syncDirectory(s.root); err != nil {
		return "", err
	}
	return publicPath, nil
}

// Open accepts only a canonical path returned by Put. Its file supports Seek
// for http.ServeContent. HTTP callers should additionally set nosniff and a
// restrictive CSP; SVG is intended for img elements, never inline insertion.
func (s *Store) Open(publicPath string) (*os.File, string, error) {
	name, contentType, ok := parsePath(publicPath)
	if !ok {
		return nil, "", fmt.Errorf("%w: invalid icon path", ErrInvalidIcon)
	}
	before, err := s.root.Lstat(name)
	if err != nil {
		return nil, "", err
	}
	if !before.Mode().IsRegular() || before.Size() > MaxStoredBytes {
		return nil, "", errors.New("stored icon must be a bounded regular file")
	}
	f, err := s.root.Open(name)
	if err != nil {
		return nil, "", err
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) {
		f.Close()
		return nil, "", errors.New("stored icon changed while opening")
	}
	return f, contentType, nil
}

func parsePath(path string) (name, contentType string, ok bool) {
	if !strings.HasPrefix(path, PublicPrefix) {
		return "", "", false
	}
	name = strings.TrimPrefix(path, PublicPrefix)
	if len(name) != 68 {
		return "", "", false
	}
	for _, c := range name[:64] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return "", "", false
		}
	}
	switch name[64:] {
	case ".png":
		contentType = "image/png"
	case ".jpg":
		contentType = "image/jpeg"
	case ".svg":
		contentType = "image/svg+xml"
	default:
		return "", "", false
	}
	return name, contentType, true
}

func normalize(input []byte) ([]byte, string, error) {
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(input, []byte{0xef, 0xbb, 0xbf}))
	if len(trimmed) > 0 && trimmed[0] == '<' {
		content, err := normalizeSVG(trimmed)
		return content, ".svg", err
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(input))
	if err != nil || (format != "png" && format != "jpeg") {
		return nil, "", fmt.Errorf("%w: expected PNG, JPEG, or static SVG", ErrInvalidIcon)
	}
	if config.Width < 1 || config.Height < 1 {
		return nil, "", ErrInvalidIcon
	}
	if config.Width > MaxDimension || config.Height > MaxDimension || int64(config.Width)*int64(config.Height) > MaxPixels {
		return nil, "", ErrTooLarge
	}
	pixels, _, err := image.Decode(bytes.NewReader(input))
	if err != nil {
		return nil, "", fmt.Errorf("%w: malformed raster image", ErrInvalidIcon)
	}
	output := &boundedBuffer{limit: MaxStoredBytes}
	extension := ".png"
	if format == "jpeg" {
		extension = ".jpg"
		err = jpeg.Encode(output, pixels, &jpeg.Options{Quality: 90})
	} else {
		err = png.Encode(output, pixels)
	}
	if err != nil {
		return nil, "", err
	}
	return output.Bytes(), extension, nil
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, ErrTooLarge
	}
	return b.Buffer.Write(p)
}
