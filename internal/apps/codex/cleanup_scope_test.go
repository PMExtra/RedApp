package codex

import (
	"reflect"
	"testing"

	"github.com/PMExtra/RedApp/internal/download"
)

func TestCleanupCandidatesStayWithinApplication(t *testing.T) {
	resource := func(id, app, version string) download.View {
		return download.View{Generation: download.Generation{Resource: download.Resource{
			ID: id, Labels: map[string]string{"app": app, "version": version},
		}}}
	}
	views := []download.View{
		resource("codex-old", "codex", "0.149.9"),
		resource("codex-current", "codex", "0.150.0"),
		resource("codex-unknown", "codex", "unrecognized"),
		resource("other-same-version", "claude-code", "0.149.9"),
		resource("other-unknown", "claude-code", "foreign-format"),
		resource("unlabelled", "", "0.149.9"),
		resource("unknown-application", "unregistered", "0.149.9"),
	}
	views = append(views, download.View{Generation: download.Generation{Resource: download.Resource{ID: "missing-labels"}}})
	ids, unknown, err := (&Catalog{}).Candidates("0.150.0", views)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, map[string]bool{"codex-old": true}) {
		t.Fatalf("cleanup selected foreign or unlabelled resources: %v", ids)
	}
	if !reflect.DeepEqual(unknown, []string{"unrecognized"}) {
		t.Fatalf("foreign versions entered application diagnostics: %v", unknown)
	}
	if views[3].Resource.Labels["app"] != "claude-code" {
		t.Fatal("selection mutated the input snapshot")
	}
}
