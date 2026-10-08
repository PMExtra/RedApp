package releasemaintenance

import (
	"fmt"
	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/apps/claude"
	"github.com/PMExtra/RedApp/internal/apps/codex"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/store"
	"reflect"
	"testing"
)

func cached(v string) download.View {
	return download.View{Generation: download.Generation{Resource: download.Resource{Application: "scope", Version: v, ID: v}, State: "complete", Bytes: 10}, Current: true}
}
func TestProvidersShareRetentionSelection(t *testing.T) {
	for name, p := range map[string]application.Protocol{"codex": codex.NewProtocol(nil), "claude": claude.NewProtocol(nil)} {
		t.Run(name, func(t *testing.T) {
			views := []download.View{cached("1.0.0"), cached("2.0.0"), cached("10.0.0"), cached("broken")}
			active := cached("2.0.0")
			active.Current = false
			active.Retired = true
			active.Readers = 1
			views = append(views, active)
			versions, ids := Select(p, "scope", 1, views, map[string]store.RetentionChannel{"latest": {Version: "10.0.0"}})
			if !reflect.DeepEqual(ids, map[string]bool{"1.0.0": true}) {
				t.Fatal(versions, ids)
			}
			reason := map[string][]string{}
			for _, v := range versions {
				reason[v.Version] = v.Reasons
			}
			if !reflect.DeepEqual(reason["2.0.0"], []string{"in_use"}) || !reflect.DeepEqual(reason["broken"], []string{"uncomparable"}) {
				t.Fatal(reason)
			}
		})
	}
}
func TestSelectionLimitAndEqualTieAreStable(t *testing.T) {
	p := codex.NewProtocol(nil)
	views := []download.View{}
	for i := range 106 {
		views = append(views, cached(fmt.Sprintf("1.0.%d", i)))
	}
	a, ids := Select(p, "scope", 1, views, nil)
	if len(ids) != 100 {
		t.Fatal(len(ids))
	}
	for i, j := 0, len(views)-1; i < j; i, j = i+1, j-1 {
		views[i], views[j] = views[j], views[i]
	}
	b, _ := Select(p, "scope", 1, views, nil)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("unstable sorting")
	}
}
func TestIncomparablePrereleasesAreProtected(t *testing.T) {
	p := claude.NewProtocol(nil)
	versions, ids := Select(p, "scope", 1, []download.View{cached("1.0.0-alpha"), cached("1.0.0-beta"), cached("2.0.0")}, nil)
	if len(ids) != 0 {
		t.Fatal(versions, ids)
	}
}

type equalProtocol struct{ application.Protocol }

func (equalProtocol) CompareVersions(a, b string) (int, error) { return 0, nil }
func TestRetentionEqualOrderTieAndHistoricalActivity(t *testing.T) {
	p := equalProtocol{codex.NewProtocol(nil)}
	a, ids := Select(p, "scope", 1, []download.View{cached("2.0.0"), cached("1.0.0")}, nil)
	if a[0].Version != "1.0.0" || !ids["2.0.0"] {
		t.Fatal(a, ids)
	}
	uid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	current := cached("1.0.0")
	current.Resource.Application = "app/" + uid + "-e2"
	historical := current
	historical.Resource.Application = "app/" + uid + "-e1"
	historical.Current = false
	historical.Retired = true
	historical.ActiveWriter = true
	latest := current
	latest.Resource.Version = "2.0.0"
	latest.Resource.ID = "latest"
	versions, ids := Select(codex.NewProtocol(nil), current.Resource.Application, 1, []download.View{current, historical, latest}, nil)
	if len(ids) != 0 {
		t.Fatal(versions, ids)
	}
}
