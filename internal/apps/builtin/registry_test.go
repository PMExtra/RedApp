package builtin

import (
	"github.com/PMExtra/RedApp/internal/application"
	"testing"
)

func TestReviewedManifestAndCanonicalIdentity(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range r.Entries() {
		if e.Upstream.Base.String() != e.Descriptor.Upstream {
			t.Fatal("descriptor and fixed reader disagree")
		}
		for _, installer := range e.Descriptor.Installers {
			op, err := e.ParsePath(installer.File)
			if err != nil || op.Kind != application.InstallerOperation {
				t.Fatalf("missing installer operation: %v", err)
			}
		}
	}
	for _, id := range []string{"codex", "/openai/codex", "OpenAI/codex", "openai//codex", "admin/app", "api/app", "assets/app", "health/app", "x/../y", "x/%2f"} {
		if _, err := application.ParseKey(id); err == nil {
			t.Errorf("invalid or reserved key accepted %q", id)
		}
		if _, ok := r.Lookup(id); ok {
			t.Errorf("default/alias application accepted %q", id)
		}
	}
	entry := r.Entries()[0]
	if _, err := application.NewRegistry([]application.Entry{entry, entry}); err == nil {
		t.Fatal("duplicate app silently replaced")
	}
}
