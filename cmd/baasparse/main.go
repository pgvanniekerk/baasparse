// Command baasparse is the single-binary telco mediation engine (alpha): it
// serves the HTMX management GUI, runs the data plane over a pluggable storage
// backend (POSIX/S3), and persists everything in PostgreSQL. See docs/technical-spec
// (esp. TS 16) for the cloud-native design.
//
// Configuration precedence: flags → environment (K8s ConfigMap/Secret) →
// ~/.baasparse/.config → defaults. JSON logs go to stdout (containers) or
// ~/.baasparse/logs (local); OpenTelemetry exports metrics/traces when an OTLP
// endpoint is configured. Subcommands:
//
//	baasparse set-conn      store the PostgreSQL connection string
//	baasparse set-port      store the HTTP port
//	baasparse setupdb       apply the database schema
//	baasparse createadmin   create/reset an administrator
//	baasparse               run the engine
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/pgvanniekerk/baasparse/internal/archiver"
	"github.com/pgvanniekerk/baasparse/internal/config"
	"github.com/pgvanniekerk/baasparse/internal/container"
	"github.com/pgvanniekerk/baasparse/internal/encoder"
	"github.com/pgvanniekerk/baasparse/internal/fetcher"
	"github.com/pgvanniekerk/baasparse/internal/httpserver"
	"github.com/pgvanniekerk/baasparse/internal/pipeline"
	"github.com/pgvanniekerk/baasparse/internal/reconcile"
	"github.com/pgvanniekerk/baasparse/internal/runner"
	"github.com/pgvanniekerk/baasparse/internal/settings"
	"github.com/pgvanniekerk/baasparse/internal/storage"
	"github.com/pgvanniekerk/baasparse/internal/store"
	"github.com/pgvanniekerk/baasparse/internal/telemetry"
	"github.com/pgvanniekerk/baasparse/internal/watcher"
)

const version = "0.1.0-alpha"

func main() {
	ctx := context.Background()

	sub := ""
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		sub = os.Args[1]
	}

	switch sub {
	case "set-conn":
		exit(setConn(ctx, os.Args[2:]))
	case "set-port":
		exit(setPort(ctx, os.Args[2:]))
	case "setupdb":
		exit(setupDB(ctx, os.Args[2:]))
	case "createadmin":
		exit(createAdmin(ctx, os.Args[2:]))
	case "version":
		fmt.Println("baasparse", version)
	case "help":
		usage()
	case "":
		if err := runApp(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", sub)
		usage()
		os.Exit(2)
	}
}

func exit(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `baasparse — telco mediation engine (alpha)

Usage:
  baasparse set-conn       Store the PostgreSQL connection string (~/.baasparse/.config)
  baasparse set-port       Store the HTTP port (default 8080)
  baasparse setupdb        Apply the database schema (all 85 tables)
  baasparse createadmin    Create/reset an administrator
  baasparse version        Print the version
  baasparse [flags]        Run the engine (GUI + data plane)

Config precedence: flags > env > ~/.baasparse/.config > defaults.
Storage: BAASPARSE_STORAGE_BACKEND=posix|s3 (+ BAASPARSE_S3_*). Telemetry: OTEL_EXPORTER_OTLP_ENDPOINT.
Logs: BAASPARSE_LOG_OUTPUT=stdout|file, BAASPARSE_LOG_FORMAT=json|text.
`)
}

func runApp() error {
	s, err := settings.Resolve()
	if err != nil {
		return err
	}
	logger, console, closeLog, err := setupLogger(s)
	if err != nil {
		return err
	}
	defer closeLog()
	if err := run(s, logger, console); err != nil {
		logger.Error("fatal", "component", "startup", "err", err.Error())
		return err
	}
	return nil
}

// setupLogger builds the slog logger (JSON by default) writing to stdout (in
// containers) or ~/.baasparse/logs (local), plus a console printer that emits
// human-friendly milestones only in local (file) mode.
func setupLogger(s settings.Settings) (*slog.Logger, func(string, ...any), func(), error) {
	var (
		sink    io.Writer = os.Stdout
		closeFn           = func() {}
	)
	if s.Log.Output != "stdout" {
		if err := settings.EnsureDirs(); err != nil {
			return nil, nil, nil, err
		}
		path, err := settings.LogFilePath()
		if err != nil {
			return nil, nil, nil, err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("open log file %s: %w", path, err)
		}
		sink = f
		closeFn = func() { _ = f.Close() }
	}
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	var h slog.Handler = slog.NewJSONHandler(sink, opts)
	if s.Log.Format == "text" {
		h = slog.NewTextHandler(sink, opts)
	}
	console := func(format string, a ...any) {
		if s.Log.Output != "stdout" {
			fmt.Printf(format, a...)
		}
	}
	return slog.New(h), console, closeFn, nil
}

func run(s settings.Settings, log *slog.Logger, console func(string, ...any)) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		return err
	}
	dbURL := firstNonEmpty(cfg.DatabaseURL, s.DatabaseURL)
	if dbURL == "" {
		return fmt.Errorf("no database configured — run: baasparse set-conn")
	}
	dbSchema := firstNonEmpty(cfg.DBSchema, s.DBSchema, settings.DefaultSchema)
	listen := cfg.ListenAddr
	if listen == "" {
		listen = fmt.Sprintf(":%d", s.Port)
	}

	// Apply hot-path buffer tuning (engine-wide; env-configurable for S3/network).
	if s.Perf.ReadBufferBytes > 0 {
		pipeline.ReadBufferBytes = s.Perf.ReadBufferBytes
	}
	if s.Perf.WriteBufferBytes > 0 {
		encoder.WriteBufferBytes = s.Perf.WriteBufferBytes
	}
	// Archive-container safety limits (decompression-bomb guards, TS 04 §4.4.8).
	if s.Perf.ArchiveMaxMembers > 0 {
		container.MaxMembers = s.Perf.ArchiveMaxMembers
	}
	if s.Perf.ArchiveMaxMemberBytes > 0 {
		container.MaxMemberBytes = s.Perf.ArchiveMaxMemberBytes
	}
	if s.Perf.ArchiveMaxTotalBytes > 0 {
		container.MaxTotalBytes = s.Perf.ArchiveMaxTotalBytes
	}

	console("baasparse %s — starting\n", version)
	log.Info("starting", "component", "startup", "function", "run", "version", version, "listen", listen, "storage", s.Storage.Backend)

	// Telemetry (OTel). Safe with no collector: traces no-op, metrics still at /metrics.
	tel, err := telemetry.Init(ctx, telemetry.Config{
		ServiceName: "baasparse", ServiceVersion: version, InstanceID: cfg.InstanceID,
		OTLPEndpoint: s.Otel.Endpoint, Insecure: s.Otel.Insecure,
	})
	if err != nil {
		return fmt.Errorf("telemetry init: %w", err)
	}
	defer func() {
		// Bound the flush so an unreachable collector can't stall shutdown.
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tel.Shutdown(shutCtx)
	}()

	// Storage backend (POSIX or S3).
	sg, err := storage.New(ctx, storage.Config{
		Backend: s.Storage.Backend, Root: s.Storage.Root,
		Endpoint: s.Storage.S3.Endpoint, Region: s.Storage.S3.Region, Bucket: s.Storage.S3.Bucket,
		AccessKey: s.Storage.S3.AccessKey, SecretKey: s.Storage.S3.SecretKey,
		UseSSL: s.Storage.S3.UseSSL, CreateBucket: s.Storage.S3.CreateBucket,
	})
	if err != nil {
		return fmt.Errorf("storage init: %w", err)
	}
	defer sg.Close()

	// The POSIX backend self-creates parent directories on Put/Move (resolving
	// them under Storage.Root), and List tolerates an absent input directory — so
	// no pre-creation is needed here. (Pre-creating ignored Storage.Root and could
	// abort startup under a read-only working directory.)
	pfx := prefixesFor(sg.Backend(), cfg)

	st, err := store.OpenPG(ctx, dbURL, dbSchema)
	if err != nil {
		return err
	}
	defer st.Close()

	if cfg.AutoMigrate {
		schemaPath := config.ResolveSchemaPath(cfg.SchemaPath)
		log.Info("applying schema", "component", "startup", "path", schemaPath)
		if err := st.ApplySchema(ctx, schemaPath); err != nil {
			return err
		}
	}
	if err := st.Ping(ctx); err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	host, _ := os.Hostname()
	if err := st.RegisterInstance(ctx, cfg.InstanceID, host, version, listen); err != nil {
		return err
	}
	log.Info("connected to database", "component", "startup", "instance", cfg.InstanceID)
	console("  database connected (instance %s)\n", cfg.InstanceID)

	// Startup DB↔storage reconciliation (single-owner): recover any completed
	// files the database is missing from their completion markers (BR-NFR-017).
	if ran, recovered, rerr := reconcile.Run(ctx, st, sg, pfx.Done, log); rerr != nil {
		log.Warn("reconciliation failed", "component", "startup", "err", rerr.Error())
	} else if ran && recovered > 0 {
		console("  recovered %d processed file(s) from completion markers\n", recovered)
	}

	rn := runner.New(st, log)
	srv, err := httpserver.New(st, rn, sg, log, pfx, tel.MetricsHandler(),
		func() bool { return st.Ping(ctx) == nil })
	if err != nil {
		return err
	}

	// Bounded graceful drain: on SIGTERM the watcher stops claiming new files
	// immediately (ctx), but an in-flight file keeps processing on procBase until
	// it finishes or the drain grace elapses (BR-HA-008).
	const drainGrace = 20 * time.Second
	procBase, procCancel := context.WithCancel(context.Background())
	defer procCancel()
	go func() {
		<-ctx.Done()
		t := time.NewTimer(drainGrace)
		defer t.Stop()
		<-t.C
		procCancel() // hard-cancel any file still processing past the grace
	}()

	// The data-plane background loops (watcher, SFTP fetcher, archiver) all share
	// the same lifecycle: they scan on ctx (SIGTERM stops claiming new work
	// immediately) and process in-flight work on procBase (bounded drain grace),
	// and they only run when the watch data plane is enabled. Each signals a done
	// channel so shutdown can wait for its in-flight work before the DB pool closes.
	watcherDone := make(chan struct{})
	fetcherDone := make(chan struct{})
	archiverDone := make(chan struct{})
	if cfg.WatchEnable {
		// The watcher resolves each pipeline's storage from its own Source
		// (per-source backend), uses the per-source done/quarantine/disposition,
		// and falls back to sg/pfx for legacy pipelines.
		w := watcher.New(st, sg, rn, log, pfx.Output, pfx.Done, 2*time.Second, s.Perf.MaxConcurrentFiles)
		go func() { w.Run(ctx, procBase); close(watcherDone) }()

		// SFTP fetch transport (single-owner): for each Source.Backend=="sftp"
		// pipeline, streams new remote files into that pipeline's own input area
		// (posix/s3) where the watcher then claims them. nil resolver defaults to
		// storage.NewForSource.
		f := fetcher.New(st, nil, log)
		go func() { f.Run(ctx, procBase); close(fetcherDone) }()

		// Archiver (single-owner): ages out DONE (and optionally QUARANTINED)
		// files per pipeline.Archive into a compressed object on the destination
		// backend, then prunes originals and records the AR/ARF audit trail.
		a := archiver.New(st, sg, log)
		go func() { a.Run(ctx, procBase); close(archiverDone) }()
	} else {
		close(watcherDone)
		close(fetcherDone)
		close(archiverDone)
	}

	httpSrv := &http.Server{Addr: listen, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		console("\nshutting down…\n")
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
	}()

	console("  storage backend: %s\n", sg.Backend())
	if s.Otel.Endpoint != "" {
		console("  telemetry → %s\n", s.Otel.Endpoint)
	}
	console("  GUI ready at http://localhost%s  (sign in)\n", listen)
	log.Info("listening", "component", "httpserver", "addr", listen)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	// Wait for the data-plane loops (watcher, fetcher, archiver) to finish their
	// in-flight work before the deferred DB pool close, so a completion never
	// races a closing pool. The bound exceeds the drain grace after which any
	// still-running work is hard-cancelled.
	drainDeadline := time.After(drainGrace + 5*time.Second)
	for _, dl := range []struct {
		name string
		done <-chan struct{}
	}{
		{"watcher", watcherDone},
		{"fetcher", fetcherDone},
		{"archiver", archiverDone},
	} {
		select {
		case <-dl.done:
		case <-drainDeadline:
			log.Warn("data-plane drain timed out", "component", "startup", "loop", dl.name)
		}
	}
	log.Info("shutdown complete", "component", "startup")
	console("stopped.\n")
	return nil
}

// prefixesFor computes the storage locations for the lifecycle areas. On POSIX
// these are local directory paths (as before); on S3 they are object-key prefixes.
func prefixesFor(backend string, cfg config.Config) httpserver.Prefixes {
	if backend == storage.BackendS3 {
		return httpserver.Prefixes{InProgress: "in-progress", Output: "output", Done: "done"}
	}
	return httpserver.Prefixes{InProgress: cfg.InProgressDir(), Output: cfg.OutputDir(), Done: cfg.DoneDir()}
}
