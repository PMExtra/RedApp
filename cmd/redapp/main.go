package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/PMExtra/RedApp/internal/apps/builtin"
	"github.com/PMExtra/RedApp/internal/auth"
	"github.com/PMExtra/RedApp/internal/catalog"
	"github.com/PMExtra/RedApp/internal/config"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/history"
	"github.com/PMExtra/RedApp/internal/httpserver"
	"github.com/PMExtra/RedApp/internal/instance"
	"github.com/PMExtra/RedApp/internal/store"
)

var version = "dev"
var revision = "unknown"

func main() {
	if err := command(os.Args[1:]); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func command(args []string) error {
	if len(args) == 1 && args[0] == "version" {
		fmt.Printf("RedApp %s (commit %s)\n", version, revision)
		return nil
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "help") {
		fmt.Println("Usage: redapp serve --config FILE | config validate --config FILE | healthcheck --config FILE | version")
		return nil
	}
	if len(args) == 0 {
		return errors.New("command required: redapp serve --config FILE; use a fresh data directory for this architecture")
	}
	mode, rest := args[0], args[1:]
	if mode == "config" {
		if len(rest) == 0 || rest[0] != "validate" {
			return errors.New("usage: redapp config validate --config FILE")
		}
		mode, rest = "validate", rest[1:]
	}
	if mode != "serve" && mode != "validate" && mode != "healthcheck" {
		return errors.New("unknown command; use serve, config validate, healthcheck, or version")
	}
	flags := flag.NewFlagSet(mode, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("config", "", "deployment JSON configuration file")
	if err := flags.Parse(rest); err != nil {
		return fmt.Errorf("invalid arguments for %s: %w", mode, err)
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	c, err := config.Load(*path)
	if err != nil {
		return err
	}
	switch mode {
	case "validate":
		fmt.Println("Deployment configuration is valid (schema 1); data directory was not opened.")
		return nil
	case "healthcheck":
		return healthcheck(c)
	default:
		return serve(c)
	}
}

func healthcheck(c config.Deployment) error {
	host, port, _ := net.SplitHostPort(c.Listen)
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	} else if host == "::" {
		host = "::1"
	}
	r, err := http.NewRequest("GET", "http://"+net.JoinHostPort(host, port)+"/health/ready", nil)
	if err != nil {
		return err
	}
	// PUBLIC_URL affects published links, never the authority used for health checks.
	r.Host = c.AllowedHosts[0]
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	resp, err := client.Do(r)
	if err != nil {
		return errors.New("healthcheck could not reach the configured listener")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func serve(c config.Deployment) error {
	// Validate a prior directory before Acquire creates an instance lock. No old
	// schema is upgraded, copied, cleared, or otherwise modified by this command.
	if err := store.Preflight(c.DataDir); err != nil {
		return err
	}
	proxy, err := httpserver.NewProxy(strings.Join(c.TrustedProxies, ","))
	if err != nil {
		return err
	}
	registry, err := builtin.New()
	if err != nil {
		return err
	}
	guard, err := instance.Acquire(c.DataDir)
	if err != nil {
		return err
	}
	defer guard.Close()
	db, err := store.Open(guard.Directory)
	if err != nil {
		return err
	}
	defer db.DB.Close()
	upstream := registry.Entries()[0].Upstream
	if err := upstream.LoadProxy(db); err != nil {
		return err
	}
	public, err := config.LoadPublicSettings(db, c.EnvironmentPublicURL)
	if err != nil {
		return err
	}
	manager, err := download.NewApplications(guard.Directory, db, registry.Upstreams())
	if err != nil {
		return err
	}
	defer manager.Close()
	if err := manager.ConfigureLimits(c.DownloadLimits.MaxActiveWriters, c.DownloadLimits.MaxReaders, c.DownloadLimits.MaxArtifactBytes); err != nil {
		return err
	}
	a, err := auth.New(db, false, func(password string) {
		log.Printf("Initial admin password: %s; change it after signing in and protect these logs.", password)
	})
	if err != nil {
		return err
	}
	metricHistory, err := history.Open(db)
	if err != nil {
		return err
	}
	handler := &httpserver.Server{Version: version, DB: db, Registry: registry, Catalog: catalog.New(db, registry), Downloads: manager, Auth: a, Proxy: proxy, Upstream: upstream, History: metricHistory, PublicConfig: public, AllowedHosts: c.AllowedHosts, Dir: guard.Directory, Started: time.Now().UTC()}
	server := &http.Server{Addr: c.Listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 10 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	metricCtx, cancelMetrics := context.WithCancel(ctx)
	metricDone := make(chan struct{})
	go func() {
		defer close(metricDone)
		handler.SampleHistory(metricCtx, func(err error) { log.Printf("Metric history sampling failed: %v", err) })
	}()
	defer func() { cancelMetrics(); <-metricDone }()
	result := make(chan error, 1)
	go func() { result <- server.ListenAndServe() }()
	log.Printf("RedApp started: listener %s, data directory %s", c.Listen, guard.Directory)
	select {
	case err := <-result:
		if err != http.ErrServerClosed {
			return err
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			server.Close()
		}
		manager.Close()
	}
	fmt.Println("RedApp stopped")
	return nil
}
