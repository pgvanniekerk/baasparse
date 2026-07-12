package httpserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"time"

	"github.com/pgvanniekerk/baasparse/internal/pipeline"
	"github.com/pgvanniekerk/baasparse/internal/storage"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

type pageData struct {
	Active string
	Data   any
	User   *store.User
}

// page builds page data with the authenticated user attached for the nav.
func (s *Server) page(r *http.Request, active string, data any) pageData {
	return pageData{Active: active, Data: data, User: currentUser(r)}
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	pipes, _ := s.store.ListPipelines(r.Context())
	files, _ := s.store.ListProcessedFiles(r.Context(), 6)
	var recIn, recOut int
	for _, f := range files {
		recIn += f.RecordsIn
		recOut += f.RecordsOut
	}
	s.render(w, "dashboard", s.page(r, "dashboard", map[string]any{
		"Pipelines":     pipes,
		"Files":         files,
		"PipelineCount": len(pipes),
		"RecordsIn":     recIn,
		"RecordsOut":    recOut,
	}))
}

func (s *Server) handlePipelineList(w http.ResponseWriter, r *http.Request) {
	pipes, err := s.store.ListPipelines(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, "pipelines", s.page(r, "pipelines", pipes))
}

func (s *Server) handlePipelineNew(w http.ResponseWriter, r *http.Request) {
	dss, _ := s.store.ListDatasources(r.Context())
	s.render(w, "pipeline_new", s.page(r, "pipelines", map[string]any{"Datasources": dss}))
}

func (s *Server) handlePipelineCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return
	}
	src := parseSource(r)
	p := store.Pipeline{
		Name:      r.FormValue("name"),
		Enabled:   r.FormValue("enabled") == "on",
		InputDir:  src.InputDir,
		OutputDir: src.OutputDir,
		Input:     parseFormatSpec(r, "input"),
		Output:    parseFormatSpec(r, "output"),
		Transform: parseTransform(r),
		Source:    src,
		Archive:   parseArchive(r),
		CreatedBy: "operator",
	}
	if p.Name == "" {
		p.Name = "pipeline-" + time.Now().Format("150405")
	}
	// Catch a bad member glob here rather than at 3am: an unparseable pattern is
	// only discovered when files arrive, and it quarantines every one of them.
	if err := validateFormatSpec(p.Input); err != nil {
		s.badRequest(w, err)
		return
	}
	// Destinations (TS 07 §7.2) and consolidation (§7.3.2). Each destination carries
	// its OWN output structure, so the pipeline-level transform is just the fallback
	// the preview and upload paths use.
	outs, oerr := parseOutputs(r)
	if oerr != nil {
		s.badRequest(w, oerr)
		return
	}
	p.Outputs = outs
	p.Batch = parseBatch(r)
	if len(p.Outputs) > 0 {
		p.Output = p.Outputs[0].Format
		if p.Outputs[0].Transform != nil {
			p.Transform = *p.Outputs[0].Transform
		}
		// Input and outputs must AGREE: a destination that draws from an input field
		// which is not declared would emit that column as null forever, silently.
		if err := store.ValidatePipeline(p); err != nil {
			s.badRequest(w, err)
			return
		}
	} else if hasDestinationRows(r) {
		s.badRequest(w, fmt.Errorf("every output destination needs a name"))
		return
	}
	id, err := s.store.CreatePipeline(r.Context(), p)
	if err != nil {
		s.fail(w, err)
		return
	}
	// Pre-create the lifecycle directory tree for a filesystem-backed pipeline so
	// the operator can drop files immediately (re-reads the pipeline so datasource
	// references are materialized into concrete dirs first).
	if created, gerr := s.store.GetPipeline(r.Context(), id); gerr == nil {
		s.provisionDirs(r.Context(), created)
	}
	http.Redirect(w, r, fmt.Sprintf("/pipelines/%d", id), http.StatusSeeOther)
}

func (s *Server) handlePipelineDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p, err := s.store.GetPipeline(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	files, _ := s.store.ListProcessedFiles(r.Context(), 50)
	var mine []store.ProcessedFile
	for _, f := range files {
		if f.PipelineID == id {
			mine = append(mine, f)
		}
	}
	dels, _ := s.store.ListDeliveries(r.Context(), id, 25)
	s.render(w, "pipeline_detail", s.page(r, "pipelines", map[string]any{
		"Pipeline":   p,
		"Files":      mine,
		"Deliveries": dels,
	}))
}

func (s *Server) handlePipelineToggle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p, err := s.store.GetPipeline(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	_ = s.store.SetPipelineEnabled(r.Context(), id, !p.Enabled)
	http.Redirect(w, r, fmt.Sprintf("/pipelines/%d", id), http.StatusSeeOther)
}

// handlePipelineRun processes an uploaded sample file through a saved pipeline.
func (s *Server) handlePipelineRun(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p, err := s.store.GetPipeline(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		s.fail(w, err)
		return
	}
	file, hdr, err := r.FormFile("sample")
	if err != nil {
		s.renderFragment(w, "run_result", map[string]any{"Error": "please choose a file to process"})
		return
	}
	defer file.Close()

	// Stage the upload into a unique in-progress location on the storage backend,
	// keeping the original filename so the Processed File record reads cleanly.
	srcKey := path.Join(s.inProgressPrefix, fmt.Sprintf("upload-%d", time.Now().UnixNano()), path.Base(hdr.Filename))
	if err := s.storage.Put(r.Context(), srcKey, file, storage.Meta{}); err != nil {
		s.fail(w, err)
		return
	}

	outPrefix := p.OutputDir
	if outPrefix == "" {
		outPrefix = s.outputPrefix
	}
	pf, err := s.runner.ProcessFile(r.Context(), p, s.storage, s.storage, srcKey, outPrefix, s.donePrefix, nil)
	if errors.Is(err, store.ErrDuplicate) {
		s.renderFragment(w, "run_result", map[string]any{"Error": "A file with this name was already processed by this pipeline (re-arrival). Rename it or use replay."})
		return
	}
	if err != nil {
		s.renderFragment(w, "run_result", map[string]any{"Error": err.Error()})
		return
	}
	// Read a preview of the produced output for display.
	preview, _ := s.readHead(r.Context(), path.Join(outPrefix, pf.OutputName), 8000)
	s.renderFragment(w, "run_result", map[string]any{
		"File":    pf,
		"Preview": preview,
		"OutDir":  outPrefix,
	})
}

// handlePreview runs an in-progress builder config against a pasted sample, with
// no persistence — the live transform modeller (BR-UI-009).
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return
	}
	// Preview one DESTINATION, because shape now lives on the destination: which
	// fields it carries, in what format. The wizard posts the destination being
	// previewed; with none, fall back to the legacy single-output form fields.
	sp := pipeline.Spec{Input: parseFormatSpec(r, "input")}
	outs, oerr := parseOutputs(r)
	if oerr != nil {
		s.renderFragment(w, "preview_result", map[string]any{"Error": oerr.Error()})
		return
	}
	if len(outs) > 0 {
		o := outs[0]
		sp.Output = o.Format
		if o.Transform != nil {
			sp.Transform = *o.Transform
		}
	} else {
		sp.Transform = parseTransform(r)
		sp.Output = parseFormatSpec(r, "output")
	}
	sample := []byte(r.FormValue("sample"))
	res := s.runner.Preview(r.Context(), sp, sample)
	s.renderFragment(w, "preview_result", map[string]any{
		"Result":  res,
		"InKind":  sp.Input.Kind,
		"OutKind": sp.Output.Kind,
	})
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	files, err := s.store.ListProcessedFiles(r.Context(), 200)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, "files", s.page(r, "files", files))
}

func (s *Server) handleArchitecture(w http.ResponseWriter, r *http.Request) {
	s.render(w, "architecture", s.page(r, "architecture", nil))
}

// --- operations ---

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = io.WriteString(w, "ok\n")
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	if !s.ready() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "not ready\n")
		return
	}
	_, _ = io.WriteString(w, "ready\n")
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	pipes, _ := s.store.ListPipelines(r.Context())
	files, _ := s.store.ListProcessedFiles(r.Context(), 100000)
	var in, out, susp int
	for _, f := range files {
		in += f.RecordsIn
		out += f.RecordsOut
		susp += f.Suspended
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP baasparse_pipelines Configured pipelines.\n# TYPE baasparse_pipelines gauge\nbaasparse_pipelines %d\n", len(pipes))
	fmt.Fprintf(w, "# HELP baasparse_files_processed_total Processed files.\n# TYPE baasparse_files_processed_total counter\nbaasparse_files_processed_total %d\n", len(files))
	fmt.Fprintf(w, "# HELP baasparse_records_in_total Records decoded.\n# TYPE baasparse_records_in_total counter\nbaasparse_records_in_total %d\n", in)
	fmt.Fprintf(w, "# HELP baasparse_records_out_total Records distributed.\n# TYPE baasparse_records_out_total counter\nbaasparse_records_out_total %d\n", out)
	fmt.Fprintf(w, "# HELP baasparse_records_suspended_total Records suspended.\n# TYPE baasparse_records_suspended_total counter\nbaasparse_records_suspended_total %d\n", susp)
}

// --- helpers ---

func (s *Server) fail(w http.ResponseWriter, err error) {
	s.log.Error("handler error", "err", err)
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

// badRequest reports invalid operator input — the fault is in the submitted form,
// not in the server, so it must not read as a 500 to the operator or to alerting.
func (s *Server) badRequest(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func (s *Server) renderFragment(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		s.log.Error("fragment render failed", "template", name, "err", err)
	}
}

func (s *Server) readHead(ctx context.Context, key string, max int) (string, error) {
	rc, err := s.storage.Open(ctx, key)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	buf := make([]byte, max)
	n, _ := io.ReadFull(rc, buf)
	if n == max {
		return string(buf[:n]) + "\n… (truncated)", nil
	}
	return string(buf[:n]), nil
}
