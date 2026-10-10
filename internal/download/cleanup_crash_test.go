package download

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/instance"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/testutil"
)

type crashEvidence struct{ Old, New, Job, FirstSeen string }

func TestCleanupCrashHelper(t *testing.T) {
	if os.Getenv("REDAPP_CLEANUP_HELPER") != "1" {
		return
	}
	guard, e := instance.Acquire(os.Getenv("REDAPP_CLEANUP_DIR"))
	if e != nil {
		t.Fatal(e)
	}
	db, e := store.Open(guard.Directory)
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(os.Getenv("REDAPP_CLEANUP_SOURCE"))
	c := &distributor.Client{Base: u, HTTP: &http.Client{Timeout: 10 * time.Second}}
	hook := &faultHook{}
	m, e := newTestManager(guard.Directory, db, c, withTrace(hook.trace))
	if e != nil {
		t.Fatal(e)
	}
	r := Resource{Application: testApp, Version: "0.1.0", Key: "asset", Source: testutil.SourceURL(c, "asset"), Hash: os.Getenv("REDAPP_CLEANUP_HASH")}
	r.ID = LogicalIdentity(r.Application, r.Version, r.Key)
	authorize(t, m, r)
	versions, _ := db.VersionsFor(testApp)
	ev := crashEvidence{FirstSeen: versions["0.1.0"]}
	rd, _, e := m.Acquire(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	g := rd.g
	ev.Old = g.ID
	if _, e = io.ReadAll(rd); e != nil {
		t.Fatal(e)
	}
	mode := os.Getenv("REDAPP_CLEANUP_MODE")
	if mode == "idle" {
		rd.Close()
	}
	point := os.Getenv("REDAPP_CLEANUP_POINT")
	hook.set(func(p string, g *Generation) {
		if p != point || (g != nil && g.ID != ev.Old) {
			return
		}
		if ev.Job == "" {
			db.DB.QueryRow("SELECT id FROM cleanup_previews LIMIT 1").Scan(&ev.Job)
		}
		json.NewEncoder(os.Stdout).Encode(ev)
		os.Exit(91)
	})
	job, e := m.Preview(testApp, map[string]bool{r.ID: true})
	if e != nil {
		t.Fatal(e)
	}
	ev.Job = job.ID
	if e = m.Cleanup(testApp, job.ID); e != nil {
		t.Fatal(e)
	}
	if mode == "new" {
		fresh, _, e := m.Acquire(context.Background(), r)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = io.ReadAll(fresh); e != nil {
			t.Fatal(e)
		}
		fresh.Close()
		ev.New = fresh.g.ID

		rd.Close()
	}
	t.Fatalf("fault point not reached: %s", point)
}
func TestCleanupProcessCrashWindows(t *testing.T) {
	data := []byte("durable cleanup generation")
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	stages := []struct{ point, mode string }{
		{"preview.after_job_save", "idle"}, {"cleanup.after_tombstone", "idle"},
		{"cleanup.after_pointer_delete", "idle"}, {"cleanup.after_detach", "idle"},
		{"delete.before_files", "idle"}, {"delete.after_blob_unlink", "idle"},
		{"delete.after_files", "idle"}, {"delete.after_generation_delete", "idle"},
		{"cleanup.after_receipt", "lease"},
		{"delete.before_files", "new"}, {"delete.after_files", "new"},
		{"delete.after_generation_delete", "new"},
	}
	for _, stage := range stages {
		t.Run(stage.mode+"/"+stage.point, func(t *testing.T) {
			dir := t.TempDir()
			r := resource(c, data)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCleanupCrashHelper$")
			cmd.Env = append(os.Environ(), "REDAPP_CLEANUP_HELPER=1", "REDAPP_CLEANUP_DIR="+dir, "REDAPP_CLEANUP_SOURCE="+c.Base.String(), "REDAPP_CLEANUP_HASH="+r.Hash, "REDAPP_CLEANUP_POINT="+stage.point, "REDAPP_CLEANUP_MODE="+stage.mode)
			out, e := cmd.CombinedOutput()
			exit, ok := e.(*exec.ExitError)
			if !ok || exit.ExitCode() != 91 {
				t.Fatalf("helper exited outside the fault point: %v %s", e, out)
			}
			var ev crashEvidence
			if e = json.Unmarshal(bytes.TrimSpace(out), &ev); e != nil {
				t.Fatalf("unreadable crash evidence: %v %s", e, out)
			}
			for restart := 0; restart < 2; restart++ {
				guard, e := instance.Acquire(dir)
				if e != nil {
					t.Fatal(e)
				}
				db, e := store.Open(dir)
				if e != nil {
					t.Fatal(e)
				}
				m, e := newTestManager(dir, db, c)
				if e != nil {
					t.Fatal(e)
				}
				versions, _ := db.VersionsFor(testApp)
				if versions["0.1.0"] != ev.FirstSeen {
					t.Fatal("first_seen was rewritten")
				}
				want := ev.New
				if stage.point == "preview.after_job_save" && restart == 0 {
					want = ev.Old
				}
				current := m.current[r.ID]
				if want == "" {
					if current != nil {
						t.Fatal("a current generation was left behind")
					}
				} else {
					if current == nil || current.ID != want || !bytes.Equal(collect(t, m, r), data) {
						t.Fatalf("current generation damaged: %s %+v", want, current)
					}
				}
				if stage.point != "preview.after_job_save" || restart > 0 {
					if _, e = os.Stat(m.partPath(ev.Old)); !os.IsNotExist(e) {
						t.Fatalf("old part remains: %v", e)
					}
					if ev.New == "" {
						if _, e = os.Stat(m.blobPath(r)); !os.IsNotExist(e) {
							t.Fatalf("unreferenced blob remains: %v", e)
						}
					}

					if m.all[ev.Old] != nil {
						t.Fatal("old generation record left behind")
					}
				}
				if e = m.Cleanup(testApp, ev.Job); e != nil {
					t.Fatal(e)
				}

				if ev.New != "" && m.current[r.ID].ID != ev.New {
					t.Fatal("old cleanup job deleted the new generation")
				}
				m.Close()
				db.DB.Close()
				guard.Close()
			}
		})
	}
}
func TestPublishedPrefixShortReadFails(t *testing.T) {
	data := []byte("published prefix")
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	m, _, _ := setup(t, c)
	r := authorizedResource(t, m, c, data)
	collect(t, m, r)
	rd, _, e := m.Acquire(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	defer rd.Close()
	if e = os.Truncate(rd.g.Path, 3); e != nil {
		t.Fatal(e)
	}
	_, e = io.ReadAll(rd)
	if e != io.ErrUnexpectedEOF {
		t.Fatalf("short read reported as success: %v", e)
	}
}
func TestCleanupTombstoneFailureDoesNotRetireCurrent(t *testing.T) {
	data := []byte("tombstone rejection")
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	m, db, _ := setup(t, c)
	r := authorizedResource(t, m, c, data)
	collect(t, m, r)
	job, e := m.Preview(testApp, map[string]bool{r.ID: true})
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.DB.Exec(`CREATE TRIGGER reject_retire BEFORE UPDATE ON generations BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Cleanup(testApp, job.ID); e == nil {
		t.Fatal("commit failure not returned")
	}
	if m.current[r.ID].Retired {
		t.Fatal("failed tombstone changed in-memory state")
	}
	db.DB.Exec("DROP TRIGGER reject_retire")
	if !bytes.Equal(collect(t, m, r), data) {
		t.Fatal("existing cache was affected")
	}
	if e = m.Cleanup(testApp, job.ID); e != nil {
		t.Fatal(e)
	}
}

func TestCleanupDatabaseDeleteFailureRetainsRetryableGeneration(t *testing.T) {
	data := []byte("delete rejection")
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	m, db, _ := setup(t, c)
	r := authorizedResource(t, m, c, data)
	collect(t, m, r)
	old := m.current[r.ID]
	job, e := m.Preview(testApp, map[string]bool{r.ID: true})
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.DB.Exec(`CREATE TRIGGER reject_delete BEFORE DELETE ON generations BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Cleanup(testApp, job.ID); e == nil {
		t.Fatal("delete failure not returned")
	}
	if m.all[old.ID] != old || !old.Retired {
		t.Fatal("delete failure lost the retryable old generation")
	}
	db.DB.Exec("DROP TRIGGER reject_delete")
	if e = m.Cleanup(testApp, job.ID); e != nil {
		t.Fatal(e)
	}
	if m.all[old.ID] != nil {
		t.Fatal("retry did not delete the old generation")
	}
}
