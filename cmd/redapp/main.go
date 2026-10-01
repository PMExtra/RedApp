package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/PMExtra/RedApp/internal/apps/codex"
	"github.com/PMExtra/RedApp/internal/auth"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/httpserver"
	"github.com/PMExtra/RedApp/internal/instance"
	"github.com/PMExtra/RedApp/internal/store"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var version = "dev"
var revision = "unknown"

func env(k, defaultValue string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return defaultValue
}
func main() {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Printf("RedApp %s (commit %s)\n", version, revision)
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		base := env("REDAPP_PUBLIC_URL", "")
		u, err := httpserver.PublicURL(base)
		if err != nil {
			os.Exit(1)
		}
		_, port, err := net.SplitHostPort(env("REDAPP_LISTEN", ":8080"))
		if err != nil {
			os.Exit(1)
		}
		r, err := http.NewRequest("GET", "http://"+net.JoinHostPort("127.0.0.1", port)+"/health/ready", nil)
		if err != nil {
			os.Exit(1)
		}
		if u != "" {
			r.Host = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
		}
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Do(r)
		if err != nil {
			os.Exit(1)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run() error {
	data := flag.String("data", env("REDAPP_DATA", "/var/lib/redapp"), "Local persistent data directory")
	listen := flag.String("listen", env("REDAPP_LISTEN", ":8080"), "Listen address")
	public := flag.String("public-url", env("REDAPP_PUBLIC_URL", ""), "Public service origin (empty: derive from request)")
	upstream := flag.String("base-url", env("REDAPP_BASE_URL", "https://releases.openai.com/codex"), "Fixed upstream base URL")
	proxies := flag.String("trusted-proxies", env("REDAPP_TRUSTED_PROXIES", ""), "Trusted proxy CIDRs, comma-separated")
	flag.Parse()
	base, e := httpserver.PublicURL(*public)
	if e != nil {
		return e
	}
	proxy, e := httpserver.NewProxy(*proxies)
	if e != nil {
		return e
	}
	client, e := distributor.New(*upstream)
	if e != nil {
		return e
	}
	guard, e := instance.Acquire(*data)
	if e != nil {
		return e
	}
	defer guard.Close()
	db, e := store.Open(guard.Directory)
	if e != nil {
		return e
	}
	defer db.DB.Close()
	manager, e := download.New(guard.Directory, db, client)
	if e != nil {
		return e
	}
	defer manager.Close()
	a, e := auth.New(db, strings.HasPrefix(base, "https://"), func(password string) {
		log.Printf("Initial admin password: %s; change it after signing in and protect these logs.", password)
	})
	if e != nil {
		return e
	}
	handler := &httpserver.Server{DB: db, Catalog: codex.New(db, client), Downloads: manager, Auth: a, Proxy: proxy, Public: base, Dir: guard.Directory, Started: time.Now().UTC()}
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 10 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result := make(chan error, 1)
	go func() { result <- server.ListenAndServe() }()
	log.Printf("RedApp started: %s, data directory %s", base, guard.Directory)
	select {
	case e := <-result:
		if e != http.ErrServerClosed {
			return e
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if e := server.Shutdown(shutdown); e != nil {
			server.Close()
		}
		manager.Close()
	}
	fmt.Println("RedApp stopped")
	return nil
}
