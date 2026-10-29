package jsonrelease

import (
	"encoding/json"
	"errors"
	"slices"

	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/releaseconfig"
)

// Snapshot owns an immutable, verified copy. The catalog must publish its
// channel binding only after the snapshot/resource transaction commits.
// This type is not a persistence implementation or a scheduler.
type Snapshot struct {
	id        string
	release   releaseconfig.Release
	canonical []byte
}

func NewSnapshot(r releaseconfig.Release) (*Snapshot, error) {
	if err := r.Validate(true); err != nil {
		return nil, err
	}
	r = copyRelease(r)
	slices.SortFunc(r.Artifacts, func(a, b releaseconfig.Artifact) int {
		if a.Key < b.Key {
			return -1
		}
		if a.Key > b.Key {
			return 1
		}
		return 0
	})
	body, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	// The version is the identity: readable paths and natural ordering are
	// worth more than telling apart two builds of one version, which are
	// refused as an immutability conflict (CheckImmutable).
	return &Snapshot{id: r.Version, release: r, canonical: body}, nil
}
func (s *Snapshot) ID() string                     { return s.id }
func (s *Snapshot) Release() releaseconfig.Release { return copyRelease(s.release) }
func copyArtifact(a releaseconfig.Artifact) releaseconfig.Artifact {
	if a.Size != nil {
		n := *a.Size
		a.Size = &n
	}
	return a
}
func copyRelease(r releaseconfig.Release) releaseconfig.Release {
	r.Artifacts = slices.Clone(r.Artifacts)
	for i := range r.Artifacts {
		r.Artifacts[i] = copyArtifact(r.Artifacts[i])
	}
	return r
}

// CheckImmutable prevents a channel refresh from changing a previously known
// version/build's bytes, source binding or normalized download choices.
func (s *Snapshot) CheckImmutable(next *Snapshot) error {
	if s.id == next.id && string(s.canonical) != string(next.canonical) {
		return errors.New("release identity has conflicting immutable metadata")
	}
	return nil
}

// Select resolves exact stable keys, never filenames or an architecture
// fallback. It preserves the administrator's selection order.
func (s *Snapshot) Select(keys []string) ([]releaseconfig.Artifact, error) {
	if len(keys) == 0 || len(keys) > releaseconfig.MaxArtifacts {
		return nil, errors.New("invalid artifact selection")
	}
	selected := make([]releaseconfig.Artifact, 0, len(keys))
	seen := map[string]bool{}
	for _, key := range keys {
		if seen[key] {
			return nil, errors.New("duplicate artifact selection")
		}
		seen[key] = true
		found := false
		for _, a := range s.release.Artifacts {
			if a.Key == key {
				selected = append(selected, copyArtifact(a))
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("selected artifact is unavailable in this release")
		}
	}
	return selected, nil
}

// DownloadPath is relative to this RedApp origin. A channel handler must capture
// one Snapshot and call this once, then emit a non-cacheable temporary redirect.
// It never exposes the upstream URL or silently switches to an older snapshot.
func (s *Snapshot) DownloadPath(app, key string) (string, error) {
	if !identity.ValidKey(app) {
		return "", errors.New("invalid application identity")
	}
	if _, err := s.Select([]string{key}); err != nil {
		return "", err
	}
	return "/" + app + "/releases/" + s.id + "/" + key, nil
}
