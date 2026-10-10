package httpserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/PMExtra/RedApp/internal/apps/builtin"
	"github.com/PMExtra/RedApp/internal/media"
)

const immutableCache = "public, max-age=31536000, immutable"

// buildAssetName is the {file} parameter of getBuildAsset.
var buildAssetName = regexp.MustCompile(`^[A-Za-z0-9._-]+\.(js|css|woff2|txt)$`)

func (s *Server) getBuildAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !buildAssetName.MatchString(name) {
		s.fail(w, r, codeFileNotFound, nil, "Asset not found")
		return
	}
	body, err := fs.ReadFile(s.frontend, buildAssetsDir+"/"+name)
	if err != nil {
		s.fail(w, r, codeFileNotFound, nil, "Asset not found")
		return
	}
	h := w.Header()
	h.Set("Cache-Control", immutableCache)
	switch path.Ext(name) {
	case ".js":
		h.Set("Content-Type", "text/javascript; charset=utf-8")
	case ".css":
		h.Set("Content-Type", "text/css; charset=utf-8")
	case ".woff2":
		h.Set("Content-Type", "font/woff2")
		// Fonts are CORS fetches; sandboxed instruction documents have an opaque origin.
		h.Set("Access-Control-Allow-Origin", "*")
	default:
		h.Set("Content-Type", "text/plain; charset=utf-8")
	}
	_, _ = w.Write(body)
}

// getUploadedIcon serves a content-addressed uploaded icon; HEAD, conditional
// requests and single ranges are handled by http.ServeContent.
func (s *Server) getUploadedIcon(w http.ResponseWriter, r *http.Request) {
	f, contentType, err := s.icons.Open(r.URL.Path)
	if err != nil {
		if errors.Is(err, media.ErrInvalidIcon) || errors.Is(err, os.ErrNotExist) {
			s.fail(w, r, codeFileNotFound, nil, "Icon not found")
		} else {
			s.writeError(w, r, storageError(err))
		}
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		s.writeError(w, r, storageError(err))
		return
	}
	name := r.PathValue("icon_file")
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	h.Set("Cache-Control", immutableCache)
	h.Set("ETag", `"`+strings.TrimSuffix(name, path.Ext(name))+`"`)
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// getPresetImage serves a reviewed image embedded with the presets.
func (s *Server) getPresetImage(w http.ResponseWriter, r *http.Request) {
	asset, ok := builtin.BrandAsset(r.URL.Path)
	if !ok {
		s.fail(w, r, codeFileNotFound, nil, "Image not found")
		return
	}
	digest := sha256.Sum256(asset.Body)
	h := w.Header()
	h.Set("Content-Type", asset.ContentType)
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	h.Set("ETag", `"sha256-`+hex.EncodeToString(digest[:])+`"`)
	http.ServeContent(w, r, path.Base(r.URL.Path), time.Time{}, bytes.NewReader(asset.Body))
}
