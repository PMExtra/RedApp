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

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/apps/builtin"
	"github.com/PMExtra/RedApp/internal/auth"
	"github.com/PMExtra/RedApp/internal/catalog"
	"github.com/PMExtra/RedApp/internal/config"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/history"
	"github.com/PMExtra/RedApp/internal/hosted"
	"github.com/PMExtra/RedApp/internal/httpcache"
	"github.com/PMExtra/RedApp/internal/httpserver"
	"github.com/PMExtra/RedApp/internal/instance"
	"github.com/PMExtra/RedApp/internal/media"
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
		fmt.Println("Usage: redapp [serve] [options] | config validate [options] | healthcheck [options] | version")
		fmt.Println("Config path: --config FILE > REDAPP_CONFIG > optional /etc/redapp/config.yaml (YAML)")
		fmt.Println("Deployment fields: CLI > environment > selected file > defaults")
		fmt.Println("Options: --data, --listen, --trusted-proxies, --max-writers, --max-readers, --max-artifact-bytes")
		return nil
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		args = append([]string{"serve"}, args...)
	}
	mode, rest := args[0], args[1:]
	if mode == "config" {
		if len(rest) == 0 || rest[0] != "validate" {
			return errors.New("usage: redapp config validate [--config FILE] [options]")
		}
		mode, rest = "validate", rest[1:]
	}
	if mode != "serve" && mode != "validate" && mode != "healthcheck" {
		return errors.New("unknown command; use serve, config validate, healthcheck, or version")
	}
	flags := flag.NewFlagSet(mode, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("config", "", "deployment YAML configuration file")
	for _, name := range []string{"data", "listen", "trusted-proxies", "max-writers", "max-readers", "max-artifact-bytes"} {
		flags.String(name, "", "override deployment setting")
	}
	if err := flags.Parse(rest); err != nil {
		return fmt.Errorf("invalid arguments for %s: %w", mode, err)
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	overrides := map[string]string{}
	configSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			configSet = true
		} else {
			overrides[f.Name] = f.Value.String()
		}
	})
	if configSet && *path == "" {
		return errors.New("--config requires a nonempty file path")
	}
	c, err := config.Load(*path, overrides)
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
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return fmt.Errorf("healthcheck cannot parse listen address: %w", err)
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	} else if host == "::" {
		host = "::1"
	}
	// Use the listener's local authority, independently of the published URL.
	r, err := http.NewRequest("GET", "http://"+net.JoinHostPort(host, port)+"/health/ready", nil)
	if err != nil {
		return err
	}
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
	guard, err := instance.Acquire(c.DataDir)
	if err != nil {
		return err
	}
	defer guard.Close()
	db, err := store.Open(guard.Directory)
	if err != nil {
		return err
	}
	defer db.Close() // Runs after all transfer services stop, flushing their final counters.
	defer db.StartCounterFlush(store.CounterFlushInterval, func(err error) { log.Printf("Counter flush failed; increments retained for retry: %v", err) })()
	db.SetDistributionValidation(builtin.ValidateDescriptors)
	// Add missing entity templates and revalidate every authoritative
	// configuration before deletion recovery mutates data.
	if err = db.EnsureEntityTemplates(); err != nil {
		return err
	}
	// Finish application deletions interrupted by a previous process.
	if err = db.RecoverApplicationDeletions(); err != nil {
		return err
	}
	if err = db.ProcessPendingDeletes(guard.Directory); err != nil {
		return err
	}
	upstream := distributor.NewPool()
	if err := upstream.LoadProxy(db); err != nil {
		return err
	}
	snapshot, err := db.DirectoryConfigurationSnapshot()
	if err != nil {
		return err
	}
	entries, err := builtin.EntriesFromConfiguration(snapshot, upstream)
	if err != nil {
		return err
	}
	registry, err := application.NewRegistry(entries)
	if err != nil {
		return err
	}
	sources := snapshot.Sources
	clients := make(map[string]*distributor.Client, len(sources))
	for _, source := range sources {
		client, e := builtin.NewScopedSourceClient(source.Provider, source.BaseURL, snapshot.ProviderDefaults[source.Provider], source.AppUID, snapshot.ProxyScopes[source.AppUID].VendorUID, upstream)
		if e != nil {
			return e
		}
		clients[source.StorageID()] = client
	}
	public, err := config.LoadPublicSettings(db, c.EnvironmentPublicURL)
	if err != nil {
		return err
	}
	manager, err := download.NewApplications(guard.Directory, db, clients)
	if err != nil {
		return err
	}
	defer manager.Close()
	if err := manager.ConfigureLimits(c.DownloadLimits.MaxWriters, c.DownloadLimits.MaxReaders, c.DownloadLimits.MaxArtifactBytes); err != nil {
		return err
	}
	a, err := auth.New(db, func(password string) {
		log.Printf("Initial admin password: %s; change it after signing in and protect these logs.", password)
	})
	if err != nil {
		return err
	}
	metricHistory, err := history.Open(db)
	if err != nil {
		return err
	}
	icons, err := media.New(guard.Directory)
	if err != nil {
		return err
	}
	defer icons.Close()
	httpCache, err := httpcache.New(guard.Directory, db, manager)
	if err != nil {
		return err
	}
	defer httpCache.Close()
	hostedFiles, err := hosted.New(guard.Directory, db, manager)
	if err != nil {
		return err
	}
	defer hostedFiles.Close()
	handler := &httpserver.Server{Version: version, DB: db, Registry: registry, Catalog: catalog.New(db, registry), Downloads: manager, HTTPCache: httpCache, Hosted: hostedFiles, Auth: a, Proxy: proxy, Upstream: upstream, Pool: upstream, Icons: icons, History: metricHistory, PublicConfig: public, Dir: guard.Directory, Started: time.Now().UTC()}
	handler.ConfigurePublication()
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
	prewarmer, err := handler.Prewarmer()
	if err != nil {
		return err
	}
	defer prewarmer.Close()
	handler.ReleaseMaintenance().AutomaticPrewarm = prewarmer.Automatic
	retentionCtx, cancelRetention := context.WithCancel(ctx)
	retentionDone := make(chan struct{})
	go func() { defer close(retentionDone); handler.ReleaseMaintenance().Run(retentionCtx) }()
	defer func() { cancelRetention(); <-retentionDone }()
	cleanupCtx, cancelCleanup := context.WithCancel(ctx)
	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		httpCache.RunCleanup(cleanupCtx, registry, func(err error) { log.Printf("Automatic HTTP cache cleanup failed: %v", err) })
	}()
	defer func() { cancelCleanup(); <-cleanupDone }()
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
		hostedFiles.Close()
		httpCache.Close()
		manager.Close()
	}
	fmt.Println("RedApp stopped")
	return nil
}
