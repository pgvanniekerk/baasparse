// Package httpserver is the management plane: the HTMX-style GUI served by the Go
// application itself (BR-UI-001) plus health/metrics endpoints (BR-OPS-009). It
// lets an operator set up pipelines (BR-UI-003), model transformations
// (BR-UI-005) with a live preview (BR-UI-009), and monitor processed files.
package httpserver

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/pgvanniekerk/baasparse/internal/runner"
	"github.com/pgvanniekerk/baasparse/internal/storage"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Prefixes are the storage locations (paths on posix, key prefixes on s3) the
// management plane stages files through.
type Prefixes struct {
	InProgress string
	Output     string
	Done       string
}

// Server holds the GUI dependencies.
type Server struct {
	store            store.Store
	runner           *runner.Runner
	storage          storage.Store
	log              *slog.Logger
	tmpl             *template.Template
	ready            func() bool
	metrics          http.Handler
	inProgressPrefix string
	outputPrefix     string
	donePrefix       string
}

// New builds the management-plane server. metrics is the /metrics handler (from
// the telemetry package); nil falls back to a minimal hand-rolled exposition.
func New(st store.Store, rn *runner.Runner, sg storage.Store, log *slog.Logger, pfx Prefixes, metrics http.Handler, ready func() bool) (*Server, error) {
	tmpl, err := template.New("").Funcs(funcMap()).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	if ready == nil {
		ready = func() bool { return true }
	}
	return &Server{
		store: st, runner: rn, storage: sg, log: log.With("component", "httpserver"), tmpl: tmpl, ready: ready,
		metrics: metrics, inProgressPrefix: pfx.InProgress, outputPrefix: pfx.Output, donePrefix: pfx.Done,
	}, nil
}

// Handler returns the routed http.Handler for the management plane.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Static assets (embedded — no CDN, BR-UI-001).
	mux.Handle("GET /static/", http.FileServerFS(staticFS))

	// Authentication (public)
	mux.HandleFunc("GET /login", s.handleLoginForm)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("GET /logout", s.handleLogout)
	mux.HandleFunc("POST /logout", s.handleLogout)

	// GUI
	mux.HandleFunc("GET /{$}", s.handleDashboard)
	mux.HandleFunc("GET /pipelines", s.handlePipelineList)
	mux.HandleFunc("GET /pipelines/new", s.handlePipelineNew)
	mux.HandleFunc("POST /pipelines", s.handlePipelineCreate)
	mux.HandleFunc("GET /pipelines/{id}", s.handlePipelineDetail)
	mux.HandleFunc("GET /pipelines/{id}/edit", s.handlePipelineEdit)
	mux.HandleFunc("POST /pipelines/{id}", s.handlePipelineUpdate)
	mux.HandleFunc("POST /pipelines/{id}/run", s.handlePipelineRun)
	mux.HandleFunc("POST /pipelines/{id}/toggle", s.handlePipelineToggle)
	mux.HandleFunc("POST /preview", s.handlePreview)

	// Datasources (reusable storage connections)
	mux.HandleFunc("GET /datasources", s.handleDatasourceList)
	mux.HandleFunc("GET /datasources/new", s.handleDatasourceNew)
	mux.HandleFunc("POST /datasources", s.handleDatasourceCreate)
	mux.HandleFunc("POST /datasources/{id}/delete", s.handleDatasourceDelete)
	mux.HandleFunc("POST /datasources/test", s.handleDatasourceTest)

	mux.HandleFunc("GET /files", s.handleFiles)
	mux.HandleFunc("GET /architecture", s.handleArchitecture)

	// Operations
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	if s.metrics != nil {
		mux.Handle("GET /metrics", s.metrics)
	} else {
		mux.HandleFunc("GET /metrics", s.handleMetrics)
	}

	return logging(s.log, s.authMiddleware(mux))
}

// render executes a named page template, logging any error.
func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var buf strings.Builder
	if err := s.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		s.log.Error("template render failed", "template", name, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	_, _ = io.WriteString(w, buf.String())
}

func funcMap() template.FuncMap {
	return template.FuncMap{
		"upper": func(v any) string { return strings.ToUpper(fmt.Sprint(v)) },
		"lower": func(v any) string { return strings.ToLower(fmt.Sprint(v)) },
	}
}

func logging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		log.Debug("http", "method", r.Method, "path", r.URL.Path)
	})
}
