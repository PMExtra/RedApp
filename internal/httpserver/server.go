package httpserver

import (
	"embed"
	"encoding/json"
	"errors"
	"github.com/PMExtra/RedApp/installers/codex"
	app "github.com/PMExtra/RedApp/internal/apps/codex"
	"github.com/PMExtra/RedApp/internal/auth"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/store"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

//go:embed web/*
var web embed.FS

type Server struct {
	DB        *store.Store
	Catalog   *app.Catalog
	Downloads *download.Manager
	Auth      *auth.Auth
	Proxy     Proxy
	Public    string
	Dir       string
	Started   time.Time
}

func PublicURL(s string) (string, error) {
	u, e := url.Parse(s)
	if e != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && u.Scheme != "http") || strings.ContainsAny(s, "'\"`$\\ \t\r\n") {
		return "", errors.New("public base URL 必须是安全的 HTTP(S) origin；首版不支持子路径")
	}
	return strings.TrimRight(s, "/"), nil
}
func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	reply(w, status, map[string]string{"error": msg})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("需要 JSON 请求")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	var tail any
	if e := d.Decode(&tail); e != io.EOF {
		return errors.New("请求尾部异常")
	}
	return nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	if r.Host != strings.TrimPrefix(strings.TrimPrefix(s.Public, "https://"), "http://") {
		fail(w, 400, "Host 与 public base URL 不匹配")
		return
	}
	if r.URL.RawPath != "" || r.URL.RawQuery != "" || strings.Contains(r.URL.Path, "\\") || strings.Contains(r.URL.Path, "//") {
		fail(w, 400, "请求路径不规范")
		return
	}
	if r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" {
		if r.Method != "GET" {
			fail(w, 405, "方法不支持")
			return
		}
		if r.URL.Path == "/health/ready" {
			if e := s.DB.DB.PingContext(r.Context()); e != nil {
				fail(w, 503, "本地存储未就绪")
				return
			}
			f, e := os.CreateTemp(s.Dir, ".health-")
			if e != nil {
				fail(w, 503, "数据目录不可写")
				return
			}
			name := f.Name()
			f.Close()
			os.Remove(name)
		}
		reply(w, 200, map[string]bool{"ok": true})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/admin") {
		s.admin(w, r)
		return
	}
	s.DB.Add("requests", 1)
	if r.Method != "GET" {
		fail(w, 405, "方法不支持")
		return
	}
	if r.URL.Path == "/install.sh" || r.URL.Path == "/install.ps1" {
		body, e := codex.Installer(strings.TrimPrefix(r.URL.Path, "/"), s.Public)
		if e != nil {
			fail(w, 503, "安装器尚未通过生成验证")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(body)
		return
	}
	if r.URL.Path == "/licenses/LICENSE" || r.URL.Path == "/licenses/NOTICE" {
		b, e := codex.License(strings.TrimPrefix(r.URL.Path, "/licenses/"))
		if e != nil {
			fail(w, 404, "许可文件不存在")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(b)
		return
	}
	v := "latest"
	isMetadata := r.URL.Path == "/channels/latest"
	name := ""
	if !isMetadata {
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) != 4 || parts[1] != "releases" {
			fail(w, 404, "路由不存在")
			return
		}
		var e error
		v, e = app.Normalize(parts[2])
		if e != nil || v != parts[2] {
			fail(w, 400, "版本必须使用规范形式")
			return
		}
		name = parts[3]
		isMetadata = name == "release.json"
	}
	if isMetadata {
		m, e := s.Catalog.Get(r.Context(), v)
		if e != nil {
			fail(w, 502, "可信元数据获取失败")
			return
		}
		reply(w, 200, m.Public(s.Public))
		return
	}
	resource, e := s.Catalog.Authorize(r.Context(), v, name)
	if e != nil {
		fail(w, 404, "资源未授权或元数据不可用")
		return
	}
	s.DB.Add("artifact_requests", 1)
	s.DB.Add("version:"+v+":requests", 1)
	rd, hit, e := s.Downloads.Acquire(r.Context(), resource)
	if e != nil {
		fail(w, 503, "下载容量不足或本地存储不可用")
		return
	}
	defer rd.Close()
	s.DB.Add(rd.Kind+"_requests", 1)
	if hit {
		s.DB.Add("reuse_requests", 1)
	} else {
		s.DB.Add("miss_requests", 1)
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Expected-SHA256", resource.Hash)
	buf := make([]byte, 32<<10)
	for {
		n, re := rd.Read(buf)
		if n > 0 {
			written, we := w.Write(buf[:n])
			s.DB.Add("downstream_bytes", int64(written))
			s.DB.Add("version:"+v+":downstream_bytes", int64(written))
			if we != nil {
				s.DB.Add("download_errors", 1)
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if re != nil {
			if re == io.EOF {
				s.DB.Add("download_success", 1)
				return
			}
			s.DB.Add("download_errors", 1)
			panic(http.ErrAbortHandler)
		}
	}
}
func (s *Server) sameOrigin(r *http.Request) bool {
	return r.Header.Get("Origin") == "" || r.Header.Get("Origin") == s.Public
}
func (s *Server) admin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; frame-ancestors 'none'; base-uri 'none'")
	if !s.sameOrigin(r) {
		fail(w, 403, "来源不允许")
		return
	}
	if r.URL.Path == "/admin/api/login" {
		if r.Method != "POST" {
			fail(w, 405, "方法不支持")
			return
		}
		var input struct {
			Password string `json:"password"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "请求无效")
			return
		}
		token, session, e := s.Auth.Login(s.Proxy.ClientIP(r), input.Password)
		if e != nil {
			fail(w, 429, "登录失败或达到限速")
			return
		}
		s.Auth.Cookie(w, token)
		reply(w, 200, map[string]string{"csrf": session.CSRF})
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/admin/api/") {
		if r.Method != "GET" {
			fail(w, 405, "方法不支持")
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/admin/")
		if path == "/admin" || path == "" {
			path = "index.html"
		}
		if path != "index.html" && path != "app.js" && path != "style.css" {
			fail(w, 404, "页面不存在")
			return
		}
		assets, _ := fs.Sub(web, "web")
		b, e := fs.ReadFile(assets, path)
		if e != nil {
			fail(w, 404, "页面不存在")
			return
		}
		contentType := map[string]string{"index.html": "text/html; charset=utf-8", "app.js": "text/javascript; charset=utf-8", "style.css": "text/css; charset=utf-8"}
		w.Header().Set("Content-Type", contentType[path])
		w.Write(b)
		return
	}
	session, ok := s.Auth.Session(r)
	if !ok {
		fail(w, 401, "请先登录")
		return
	}
	if r.Method != "GET" && !s.Auth.CSRF(r, session) {
		fail(w, 403, "CSRF 验证失败")
		return
	}
	if r.Method == "GET" {
		switch r.URL.Path {
		case "/admin/api/session":
			reply(w, 200, map[string]string{"csrf": session.CSRF})
		case "/admin/api/status":
			status, e := s.status()
			if e != nil {
				fail(w, 503, "状态读取失败")
				return
			}
			reply(w, 200, status)
		default:
			fail(w, 404, "接口不存在")
		}
		return
	}
	if r.Method != "POST" {
		fail(w, 405, "方法不支持")
		return
	}
	switch r.URL.Path {
	case "/admin/api/logout":
		s.Auth.Logout(r)
		s.Auth.Cookie(w, "")
		reply(w, 200, map[string]bool{"ok": true})
	case "/admin/api/password":
		var input struct {
			Old string `json:"old"`
			New string `json:"new"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "请求无效")
			return
		}
		if e := s.Auth.Password(input.Old, input.New); e != nil {
			fail(w, 400, "密码修改失败，请检查当前密码及长度")
			return
		}
		reply(w, 200, map[string]bool{"ok": true})
	case "/admin/api/settings":
		var input struct {
			TTL int `json:"latest_ttl_seconds"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "请求无效")
			return
		}
		if e := s.Catalog.SetTTL(input.TTL); e != nil {
			fail(w, 400, "TTL 允许 1–86400 秒")
			return
		}
		reply(w, 200, map[string]bool{"ok": true})
	case "/admin/api/cleanup/preview":
		var input struct {
			Minimum string `json:"minimum_version"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "请求无效")
			return
		}
		views := s.Downloads.Snapshot()
		ids, unknown, e := s.Catalog.Candidates(input.Minimum, views)
		if e != nil {
			fail(w, 400, "最小版本无效")
			return
		}
		job, e := s.Downloads.Preview(ids)
		if e != nil {
			fail(w, 503, "预览持久化失败")
			return
		}
		var size int64
		active := 0
		for _, v := range views {
			for _, selected := range job.Selected {
				if v.ID == selected.Generation {
					size += v.Bytes
					if v.State != "complete" {
						active++
					}
				}
			}
		}
		reply(w, 200, map[string]any{"job": job, "logical_bytes": size, "active": active, "unknown_versions": unknown})
	case "/admin/api/cleanup/execute":
		var input struct {
			ID string `json:"cleanup_id"`
		}
		if decode(w, r, &input) != nil {
			fail(w, 400, "请求无效")
			return
		}
		if e := s.Downloads.Cleanup(input.ID); e != nil {
			fail(w, 409, "清理任务无效或执行失败")
			return
		}
		reply(w, 200, map[string]bool{"ok": true})
	default:
		fail(w, 404, "接口不存在")
	}
}
func (s *Server) status() (map[string]any, error) {
	views := s.Downloads.Snapshot()
	var complete, temp, pending, total, logical, allocatedCache, allocatedTemp, allocatedPending int64
	classes := map[string]string{}
	for _, v := range views {
		if v.Retired {
			classes[v.Path] = "pending"
			pending += v.Bytes
		} else if v.State == "complete" {
			classes[v.Path] = "cache"
			complete += v.Bytes
		} else {
			classes[v.Path] = "temporary"
			temp += v.Bytes
		}
	}
	e := filepath.WalkDir(s.Dir, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		info, e := d.Info()
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		allocated := info.Size()
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			allocated = st.Blocks * 512
		}
		total += allocated
		if !d.IsDir() {
			logical += info.Size()
		}
		switch classes[path] {
		case "cache":
			allocatedCache += allocated
		case "temporary":
			allocatedTemp += allocated
		case "pending":
			allocatedPending += allocated
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	var disk syscall.Statfs_t
	if e = syscall.Statfs(s.Dir, &disk); e != nil {
		return nil, e
	}
	versions, e := s.DB.Versions()
	if e != nil {
		return nil, e
	}
	events, e := s.DB.Events()
	if e != nil {
		return nil, e
	}
	counters, e := s.DB.Counters()
	if e != nil {
		return nil, e
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return map[string]any{"name": "RedApp", "started": s.Started, "os": runtime.GOOS, "arch": runtime.GOARCH, "go": runtime.Version(), "goroutines": runtime.NumGoroutine(), "memory_bytes": mem.Alloc, "sampled_at": time.Now().UTC(), "resources": views, "versions": versions, "events": events, "counters": counters, "disk": map[string]any{"used_bytes": total, "logical_bytes": logical, "allocated_cache_bytes": allocatedCache, "allocated_temporary_bytes": allocatedTemp, "allocated_pending_bytes": allocatedPending, "cache_bytes": complete, "temporary_bytes": temp, "pending_bytes": pending, "other_bytes": total - allocatedCache - allocatedTemp - allocatedPending, "free_bytes": disk.Bavail * uint64(disk.Bsize)}, "public_base_url": s.Public, "rates": s.DB.Rates(), "client_runtime_update_policy": "企业安装器删除自动更新标记；CLI 二进制原样，运行期公网更新检查由企业出口控制。"}, nil
}
