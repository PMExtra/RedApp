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
	"path/filepath"
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
	m, e := New(guard.Directory, db, c)
	if e != nil {
		t.Fatal(e)
	}
	r := Resource{Source: c.URL("asset"), Hash: os.Getenv("REDAPP_CLEANUP_HASH")}
	r.ID = Identity(r.Source, r.Hash)
	db.Seen("0.1.0")
	versions, _ := db.Versions()
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
	m.testFault = func(p string, g *Generation) {
		if p != point || (g != nil && g.ID != ev.Old) {
			return
		}
		if ev.Job == "" {
			jobs, _ := db.List("cleanup")
			if len(jobs) > 0 {
				var j Cleanup
				json.Unmarshal(jobs[0], &j)
				ev.Job = j.ID
			}
		}
		json.NewEncoder(os.Stdout).Encode(ev)
		os.Exit(91)
	}
	job, e := m.Preview(map[string]bool{r.ID: true})
	if e != nil {
		t.Fatal(e)
	}
	ev.Job = job.ID
	if e = m.Cleanup(job.ID); e != nil {
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
		// A completed retired writer also retains a .part, not a published blob.
		if point == "delete.after_part_unlink" {
			m.mu.Lock()
			part := filepath.Join(guard.Directory, "objects", r.ID, g.ID+".part")
			if e = os.Rename(g.Path, part); e != nil {
				t.Fatal(e)
			}
			g.Path = part
			if e = m.save(g); e != nil {
				t.Fatal(e)
			}
			m.mu.Unlock()
		}
		rd.Close()
	}
	t.Fatalf("故障点未触发: %s", point)
}
func TestCleanupProcessCrashWindows(t *testing.T) {
	data := []byte("durable cleanup generation")
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	stages := []struct{ point, mode string }{
		{"preview.after_job_save", "idle"}, {"cleanup.after_tombstone", "idle"},
		{"cleanup.after_pointer_delete", "idle"}, {"cleanup.after_detach", "idle"},
		{"delete.before_files", "idle"}, {"delete.after_blob_unlink", "idle"},
		{"delete.after_files", "idle"}, {"delete.after_generation_delete", "idle"},
		{"cleanup.after_job_delete", "lease"},
		{"delete.before_files", "new"}, {"delete.after_blob_unlink", "new"},
		{"delete.after_part_unlink", "new"}, {"delete.after_files", "new"},
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
				t.Fatalf("退出非故障点: %v %s", e, out)
			}
			var ev crashEvidence
			if e = json.Unmarshal(bytes.TrimSpace(out), &ev); e != nil {
				t.Fatalf("证据 %v %s", e, out)
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
				m, e := New(dir, db, c)
				if e != nil {
					t.Fatal(e)
				}
				versions, _ := db.Versions()
				if versions["0.1.0"] != ev.FirstSeen {
					t.Fatal("first_seen 被改写")
				}
				want := ev.New
				if stage.point == "preview.after_job_save" && restart == 0 {
					want = ev.Old
				}
				current := m.current[r.ID]
				if want == "" {
					if current != nil {
						t.Fatal("遗留 current")
					}
				} else {
					if current == nil || current.ID != want || !bytes.Equal(collect(t, m, r), data) {
						t.Fatalf("当前代损坏: %s %+v", want, current)
					}
				}
				if stage.point != "preview.after_job_save" || restart > 0 {
					for _, ext := range []string{".blob", ".part"} {
						if _, e = os.Stat(filepath.Join(dir, "objects", r.ID, ev.Old+ext)); !os.IsNotExist(e) {
							t.Fatalf("旧文件未回收: %s %v", ext, e)
						}
					}
					if m.all[ev.Old] != nil {
						t.Fatal("旧代记录残留")
					}
				}
				jobs, _ := db.List("cleanup")
				for _, raw := range jobs {
					var job Cleanup
					json.Unmarshal(raw, &job)
					if e = m.Cleanup(job.ID); e != nil {
						t.Fatal(e)
					}
				}
				if ev.New != "" && m.current[r.ID].ID != ev.New {
					t.Fatal("旧 job 误删新代")
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
	r := resource(c, data)
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
		t.Fatalf("短读伪装成功: %v", e)
	}
}
func TestCleanupTombstoneFailureDoesNotRetireCurrent(t *testing.T) {
	data := []byte("tombstone rejection")
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	m, db, _ := setup(t, c)
	r := resource(c, data)
	collect(t, m, r)
	job, e := m.Preview(map[string]bool{r.ID: true})
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.DB.Exec(`CREATE TRIGGER reject_retire BEFORE UPDATE ON records WHEN NEW.kind='generation' BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Cleanup(job.ID); e == nil {
		t.Fatal("提交失败未返回")
	}
	if m.current[r.ID].Retired {
		t.Fatal("失败 tombstone 污染内存")
	}
	db.DB.Exec("DROP TRIGGER reject_retire")
	if !bytes.Equal(collect(t, m, r), data) {
		t.Fatal("现有缓存受影响")
	}
	if e = m.Cleanup(job.ID); e != nil {
		t.Fatal(e)
	}
}

func TestCleanupDatabaseDeleteFailureRetainsRetryableGeneration(t *testing.T) {
	data := []byte("delete rejection")
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	m, db, _ := setup(t, c)
	r := resource(c, data)
	collect(t, m, r)
	old := m.current[r.ID]
	job, e := m.Preview(map[string]bool{r.ID: true})
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.DB.Exec(`CREATE TRIGGER reject_delete BEFORE DELETE ON records WHEN OLD.kind='generation' BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Cleanup(job.ID); e == nil {
		t.Fatal("删除失败未返回")
	}
	if m.all[old.ID] != old || !old.Retired {
		t.Fatal("删除失败失去可重试旧代")
	}
	db.DB.Exec("DROP TRIGGER reject_delete")
	if e = m.Cleanup(job.ID); e != nil {
		t.Fatal(e)
	}
	if m.all[old.ID] != nil {
		t.Fatal("重试未删除旧代")
	}
}
