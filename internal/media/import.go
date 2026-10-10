package media

import (
	"crypto/sha256"
	"encoding/hex"
	pathpkg "path"
)

// ApplyImages publishes images with apply and removes the images it created
// if apply fails. Existing shared content is never removed; neither is a
// created image that is referenced or that another caller wrote meanwhile.
func (s *Store) ApplyImages(images map[string][]byte, referenced func(string) bool, apply func() error) error {
	s.mu.Lock()
	s.imports++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.imports--; s.imports == 0 {
			clear(s.writes)
		}
		s.mu.Unlock()
	}()
	claims := []claim{}
	rollback := func() {
		for _, c := range claims {
			if c.created && !c.shared {
				s.release(c, referenced(PublicPrefix+c.name))
			}
		}
	}
	for path, raw := range images {
		expected, err := StaticImagePath(raw, pathpkg.Ext(path))
		if err != nil || expected != path {
			rollback()
			return ErrInvalidIcon
		}
		actual, c, err := s.putContent(raw, pathpkg.Ext(path))
		claims = append(claims, c)
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
	if _, e := StaticImagePath(raw, extension); e != nil {
		return "", e
	}
	path, _, err := s.putContent(raw, extension)
	return path, err
}
