// Package media stores validated, inert images for application and vendor icons.
package media

import (
	"bytes"
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

	"github.com/PMExtra/RedApp/internal/fsutil"
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

// Store owns the icon directory below the caller's already validated and
// locked RedApp data directory.
type Store struct {
	dir      string
	mu       sync.Mutex
	importMu sync.Mutex
}

func New(dir string) (*Store, error) {
	info, err := os.Lstat(filepath.Clean(dir))
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("icon data directory: %w", fsutil.ErrNotDirectory)
	}
	icons := filepath.Join(dir, "objects", "icons")
	for _, path := range []string{filepath.Dir(icons), icons} {
		if err := fsutil.EnsureDir(path); err != nil {
			return nil, err
		}
	}
	return &Store{dir: icons}, nil
}

// Close exists for symmetry with the other storage services; the store keeps
// no open handles between calls.
func (s *Store) Close() error { return nil }

// Put accepts image bytes, never a caller-provided filename. The returned path
// is immutable and names the normalized content, not the original upload.
func (s *Store) Put(reader io.Reader) (string, error) {
	s.importMu.Lock()
	defer s.importMu.Unlock()
	return s.put(reader)
}
func (s *Store) put(reader io.Reader) (string, error) {
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
	return s.putContent(content, extension)
}

// putContent preserves validated static exchange assets; upload normalization stays in put.
func (s *Store) putContent(content []byte, extension string) (string, error) {
	digest := sha256.Sum256(content)
	name := hex.EncodeToString(digest[:]) + extension
	publicPath := PublicPrefix + name
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Lstat(filepath.Join(s.dir, name)); err == nil {
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
	// A subsequent database reference must not become durable before the file's
	// directory entry does; WriteFile syncs the directory after the rename.
	// Failed uploads never remove a previously stored icon.
	if err := fsutil.WriteFile(filepath.Join(s.dir, name), content); err != nil {
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
	f, err := fsutil.OpenRegular(filepath.Join(s.dir, name))
	if err != nil {
		return nil, "", err
	}
	if info, err := f.Stat(); err != nil || info.Size() > MaxStoredBytes {
		f.Close()
		return nil, "", errors.New("stored icon must be a bounded regular file")
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

// ValidateStaticImage validates reviewed image bytes without changing their identity.
// Uploads continue to normalize; embedded assets retain their original bytes.
func ValidateStaticImage(input []byte, extension string) (string, error) {
	if len(input) > MaxBytes {
		return "", ErrTooLarge
	}
	_, detected, err := normalize(input)
	if err != nil {
		return "", err
	}
	if extension == ".jpeg" {
		extension = ".jpg"
	}
	if extension != detected {
		return "", ErrInvalidIcon
	}
	switch detected {
	case ".svg":
		return "image/svg+xml", nil
	case ".png":
		return "image/png", nil
	case ".jpg":
		return "image/jpeg", nil
	}
	return "", ErrInvalidIcon
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
