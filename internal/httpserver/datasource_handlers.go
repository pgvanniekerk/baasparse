package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/pgvanniekerk/baasparse/internal/storage"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

// handleDatasourceList shows the reusable storage connections a pipeline can pick.
func (s *Server) handleDatasourceList(w http.ResponseWriter, r *http.Request) {
	dss, err := s.store.ListDatasources(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, "datasources", s.page(r, "datasources", dss))
}

// handleDatasourceNew renders the datasource setup form.
func (s *Server) handleDatasourceNew(w http.ResponseWriter, r *http.Request) {
	s.render(w, "datasource_new", s.page(r, "datasources", nil))
}

// handleDatasourceCreate persists a new datasource (secrets encrypted by the store).
func (s *Server) handleDatasourceCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return
	}
	d := parseDatasource(r)
	if d.Name == "" {
		s.render(w, "datasource_new", s.page(r, "datasources", map[string]any{"Error": "give the datasource a name"}))
		return
	}
	if _, err := s.store.CreateDatasource(r.Context(), d); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			s.render(w, "datasource_new", s.page(r, "datasources", map[string]any{"Error": "a datasource with that name already exists"}))
			return
		}
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/datasources", http.StatusSeeOther)
}

// handleDatasourceDelete soft-deletes a datasource (pipelines that still reference
// it keep resolving).
func (s *Server) handleDatasourceDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteDatasource(r.Context(), id); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/datasources", http.StatusSeeOther)
}

// handleDatasourceTest probes a datasource's connection (read/write round-trip for
// posix + s3-with-bucket, a ListBuckets reachability check for a bucketless s3
// connection) and renders an inline pass/fail fragment — the "Test connection"
// button. It builds the config from the same-named form fields, so it works both
// on the datasource form and (later) inline in the wizard.
func (s *Server) handleDatasourceTest(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderFragment(w, "test_result", map[string]any{"Error": err.Error()})
		return
	}
	cfg := datasourceTestConfig(r)
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	if err := storage.TestConnection(ctx, cfg); err != nil {
		s.renderFragment(w, "test_result", map[string]any{"Error": err.Error()})
		return
	}
	detail := "connection verified"
	if cfg.Backend == storage.BackendS3 && cfg.Bucket == "" {
		detail = "endpoint and credentials verified (bucket is set per-pipeline)"
	}
	s.renderFragment(w, "test_result", map[string]any{"OK": true, "Detail": detail})
}

// provisionDirs pre-creates a filesystem-backed pipeline's lifecycle directories
// so an operator can drop files into the input area immediately (TS 04). It is a
// best-effort convenience run after CreatePipeline: input/in-progress/quarantine
// on the source store, done/output on the output store; object-storage backends
// have virtual prefixes and are skipped. Errors are logged, never fatal.
func (s *Server) provisionDirs(ctx context.Context, p store.Pipeline) {
	src, err := storage.NewForSource(ctx, p.Source)
	if err != nil {
		s.log.Warn("provision dirs: resolve source store failed", "pipeline", p.Name, "err", err.Error())
		return
	}
	dst := src
	if p.CrossBackend() {
		if d, derr := storage.NewForDest(ctx, p.OutputDest); derr == nil {
			dst = d
			defer dst.Close()
		} else {
			s.log.Warn("provision dirs: resolve output store failed", "pipeline", p.Name, "err", derr.Error())
		}
	}
	defer src.Close()

	if src.Backend() == storage.BackendPOSIX {
		if err := storage.EnsureTree(ctx, src, p.Source.InputDir, p.Source.InProgressDir, p.Source.QuarantineDir); err != nil {
			s.log.Warn("provision source dirs failed", "pipeline", p.Name, "err", err.Error())
		}
	}
	if dst.Backend() == storage.BackendPOSIX {
		if err := storage.EnsureTree(ctx, dst, p.Source.DoneDir, p.Source.OutputDir); err != nil {
			s.log.Warn("provision output dirs failed", "pipeline", p.Name, "err", err.Error())
		}
	}
	s.log.Info("provisioned pipeline directories", "pipeline", p.Name,
		"input", fmt.Sprintf("%s:%s", src.Backend(), p.Source.InputDir))
}
