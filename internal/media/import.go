package media

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	pathpkg "path"
)

func PrepareImage(raw []byte) ([]byte, string, error) {
	if len(raw) > MaxBytes {
		return nil, "", ErrTooLarge
	}
	body, ext, e := normalize(raw)
	if e != nil {
		return nil, "", e
	}
	hash := sha256.Sum256(body)
	return body, PublicPrefix + hex.EncodeToString(hash[:]) + ext, nil
}

// ApplyImages serializes image uploads with publication/rollback. Existing shared
// content is never removed; only newly created, still unreferenced images are reclaimed.
func (s *Store) ApplyImages(images map[string][]byte, referenced func(string) bool, apply func() error) error {
	s.importMu.Lock()
	defer s.importMu.Unlock()
	created := []string{}
	rollback := func() {
		for _, path := range created {
			if !referenced(path) {
				name, _, ok := parsePath(path)
				if ok {
					s.mu.Lock()
					s.root.Remove(name)
					syncDirectory(s.root)
					s.mu.Unlock()
				}
			}
		}
	}
	for path, raw := range images {
		expected, err := StaticImagePath(raw, pathpkg.Ext(path))
		if err != nil || expected != path {
			rollback()
			return ErrInvalidIcon
		}
		f, _, err := s.Open(path)
		exists := err == nil
		if exists {
			f.Close()
		} else if !errors.Is(err, os.ErrNotExist) {
			rollback()
			return err
		}
		if !exists {
			created = append(created, path)
		}
		actual, err := s.putContent(raw, pathpkg.Ext(path))
		if err != nil {
			rollback()
			return err
		}
		if actual != path {
			rollback()
			return ErrInvalidIcon
		}
	}
	if err := apply(); err != nil {
		rollback()
		return err
	}
	return nil
}

// Static imports validate the full bytes without repeated lossy JPEG re-encoding.
func StaticImagePath(raw []byte, extension string) (string, error) {
	if _, e := ValidateStaticImage(raw, extension); e != nil {
		return "", e
	}
	hash := sha256.Sum256(raw)
	return PublicPrefix + hex.EncodeToString(hash[:]) + extension, nil
}
func (s *Store) PutStatic(raw []byte, extension string) (string, error) {
	s.importMu.Lock()
	defer s.importMu.Unlock()
	if _, e := StaticImagePath(raw, extension); e != nil {
		return "", e
	}
	return s.putContent(raw, extension)
}
