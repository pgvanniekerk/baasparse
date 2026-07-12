// Package store is the persistence boundary for the management and data planes.
// Domain types here are the alpha's view of the temporal configuration model and
// the operational Processed File record; the Postgres implementation (pg.go)
// maps them onto the TS 03 schema (PL/PLV/SRC/FD/TR/DS and PF/RS).
package store

import (
	"context"
	"errors"
	"time"

	"github.com/pgvanniekerk/baasparse/internal/pipeline"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

// ErrNotFound is returned when a lookup by id misses.
var ErrNotFound = errors.New("not found")

// ErrDuplicate is returned when recording a Processed File would create a second
// DONE record for the same source+name (the exactly-once DB backstop,
// UX_PF_SRC_NAME_DONE) — a re-arrival or a claim-race loser.
var ErrDuplicate = errors.New("file already processed")

// Pipeline is a fully-configured streaming pipeline as edited in the GUI. In the
// schema it spans PL_PIPELINE + PLV_PIPELINE_VERSION (+ SRC/FD/TR/DS rows); the
// alpha keeps the composed spec together for editing convenience.
type Pipeline struct {
	ID     int64
	SrcUID int64 // published SRC_SOURCE row for this pipeline (for file claims)
	Name   string
	// Description is the operator's own note about what this pipeline is for. It is
	// the only free-text column on the list page, because a pipeline's shape (its
	// formats, its transform) is no longer summarisable in a column once it can have
	// several destinations, each with a different one.
	Description string
	Enabled     bool
	InputDir  string
	OutputDir string
	Input     spec.FormatSpec
	Transform spec.TransformSpec
	// Output is the FIRST destination's format, kept so the single-output paths
	// (preview, upload) and legacy pipelines keep working unchanged. Outputs is
	// authoritative for the data plane.
	Output spec.FormatSpec
	// Outputs are the delivery destinations (TS 07 §7.2) — one or more. Each is
	// written independently, so a destination outage cannot stall the others.
	// Always non-empty after a load: a legacy single-output pipeline materializes
	// as one destination named "default".
	Outputs []Output
	// Batch is the output consolidation policy (many input files -> one output
	// object per destination). Disabled means one output per input file.
	Batch BatchSpec
	// Source carries the per-pipeline storage backend, lifecycle areas, done
	// disposition, SFTP fetch transport and declared input fields (decrypted).
	Source Source
	// OutputDest is the materialized output/done destination when the pipeline
	// references a distinct output datasource (cross-backend, e.g. read S3 → write
	// FS). A zero value means output/done use the same store as the source.
	OutputDest Dest
	// Archive is the optional per-pipeline archival policy (nil when unset).
	Archive   *Archive
	CreatedBy string
	CreatedOn time.Time
}

// CrossBackend reports whether the pipeline writes output/done to a different
// store than it reads input from.
func (p Pipeline) CrossBackend() bool {
	return p.OutputDest.Backend != "" || p.OutputDest.Root != "" || p.OutputDest.S3.Bucket != ""
}

// Runner is the executable form of a pipeline's config, for the SINGLE-output
// paths (preview, upload, and pipelines with one destination). It takes the first
// destination's own output structure when it has one — shaping lives on the
// destination now, and the pipeline-level transform is only the legacy fallback.
func (p Pipeline) Runner() pipeline.Spec {
	tr := p.Transform
	out := p.Output
	if len(p.Outputs) > 0 {
		if p.Outputs[0].Transform != nil {
			tr = *p.Outputs[0].Transform
		}
		out = p.Outputs[0].Format
	}
	return pipeline.Spec{Input: p.Input, Transform: tr, Output: out}
}

// ProcessedFile is the operational record of one processed input file
// (PF_PROCESSED_FILE + RS_RECONCILIATION_SUMMARY), BR-COL-005/BR-REC-002.
type ProcessedFile struct {
	FileUID      int64
	PipelineID   int64
	PipelineName string
	Name         string
	Size         int64
	Status       string // COLLECTED | DONE | SUSPENDED | QUARANTINED
	RecordsIn    int
	RecordsOut   int
	Suspended    int
	OutputName   string
	CollectedOn  time.Time
	// Reason is a short reason code (+ optional detail) explaining a QUARANTINED /
	// errored file; empty for successful files.
	Reason string
}

// ArchivablePF is a DONE (or optionally QUARANTINED) processed file eligible for
// age-out archival — the minimal projection the archiver needs to fetch the
// output object and record the audit trail.
type ArchivablePF struct {
	PFUID       int64     // PF_UID (audit key)
	FileUID     int64     // PF_FILE_UID
	Name        string    // source file name
	OutputName  string    // written output name (PF_COMPLETION_MARKER)
	Size        int64     // PF_SIZE_BYTES
	Status      string    // DONE | QUARANTINED
	CompletedOn time.Time // PF_COMPLETED_ON (falls back to PF_COLLECTED_ON)
}

// ArchiveRun is the archiver's report of one completed archive operation, used to
// write the AR_ARCHIVE_RUN + ARF_ARCHIVE_RUN_FILE audit rows and flip the
// processed files to ARCHIVED.
type ArchiveRun struct {
	Compression string  // "gzip" | "zip" | "tar_gz"
	ArchiveName string  // produced archive object name
	SizeBytes   int64   // archive size
	Checksum    string  // optional archive checksum
	Target      string  // destination description (e.g. backend/root)
	Pruned      bool    // whether local originals were pruned
	PFUIDs      []int64 // PF_UID of every file included in the archive
}

// User is an account in the built-in user system (U_USER + roles).
type User struct {
	UID                int64
	Username           string
	DisplayName        string
	Status             string // ACTIVE | DISABLED
	PasswordHash       string
	MustChangePassword bool
	Roles              []string
}

// Session is a GUI login session (SES_SESSION).
type Session struct {
	UserUID     int64
	IdleExpires time.Time
	AbsExpires  time.Time
	ClientAddr  string
}

// Store is the persistence interface used by the management and data planes.
type Store interface {
	// Configuration (management plane)
	ListPipelines(ctx context.Context) ([]Pipeline, error)
	GetPipeline(ctx context.Context, id int64) (Pipeline, error)
	CreatePipeline(ctx context.Context, p Pipeline) (int64, error)
	UpdatePipeline(ctx context.Context, p Pipeline) error
	SetPipelineEnabled(ctx context.Context, id int64, enabled bool) error

	// Datasources (reusable named storage connections, selected by pipelines)
	ListDatasources(ctx context.Context) ([]Datasource, error)
	GetDatasource(ctx context.Context, id int64) (Datasource, error)
	CreateDatasource(ctx context.Context, d Datasource) (int64, error)
	DeleteDatasource(ctx context.Context, id int64) error

	// Operational (data plane)
	NextFileUID(ctx context.Context) (int64, error)
	RecordProcessedFile(ctx context.Context, pf ProcessedFile) error
	ListProcessedFiles(ctx context.Context, limit int) ([]ProcessedFile, error)

	// Fetch transport (fetcher): pipelines whose Source.Backend=="sftp".
	ListSFTPSourcePipelines(ctx context.Context) ([]Pipeline, error)

	// Archival (archiver)
	ListArchivablePF(ctx context.Context, pipelineID int64, olderThan time.Time, includeQuarantined bool) ([]ArchivablePF, error)
	MarkArchived(ctx context.Context, pipelineID int64, run ArchiveRun) (int64, error)

	// Distributed file claim (multi-instance safety, BR-HA-003/004, BR-COL-006)
	AlreadyProcessed(ctx context.Context, srcUID int64, name string) (bool, error)
	AlreadyQuarantined(ctx context.Context, srcUID int64, name string) (bool, error)
	ClaimFile(ctx context.Context, srcUID int64, name string) (Claim, bool, error)
	HeartbeatClaim(ctx context.Context, c Claim) (bool, error)
	ReleaseClaim(ctx context.Context, c Claim) error
	CompleteClaimed(ctx context.Context, pf ProcessedFile, c Claim) (bool, error)
	QuarantineFile(ctx context.Context, pipelineID, srcUID int64, name string, size int64, reason string, c Claim) (bool, error)

	// Output consolidation (TS 07 §7.3.2): a batch is a frozen set of input files
	// producing one output object per destination, with each destination's delivery
	// tracked (and its sequence reserved) independently so one failing destination
	// neither stalls the others nor causes them to be re-delivered on retry.
	FormBatch(ctx context.Context, pipelineID, srcUID int64, files []BatchFile) (OpenBatch, error)
	ListOpenBatches(ctx context.Context, srcUID int64) ([]OpenBatch, error)
	BatchedFileNames(ctx context.Context, srcUID int64) (map[string]bool, error)
	ReserveDelivery(ctx context.Context, pipelineID int64, o Output, b Batch, nameFor func(seq int64) string) (Delivery, error)
	MarkDelivered(ctx context.Context, d Delivery) error
	MarkDeliveryFailed(ctx context.Context, d Delivery, reason string) error
	CloseBatch(ctx context.Context, b Batch, pfs []ProcessedFile, claims []Claim, dels []Delivery, contribs map[int64][]Contribution) (bool, error)
	RecordBatchContributions(ctx context.Context, btUID int64, cs []Contribution) error
	SettledFileNames(ctx context.Context, srcUID int64, names []string) (map[string]bool, error)
	CountProcessedFiles(ctx context.Context, srcUID int64) (int, error)
	TouchBatchAttempt(ctx context.Context, btUID int64, reason string) (int, error)
	DropBatchFile(ctx context.Context, b Batch, name string) error
	AbandonBatch(ctx context.Context, btUID int64, reason string) error
	ListDeliveries(ctx context.Context, pipelineID int64, limit int) ([]DeliveryView, error)

	// Recovery (startup DB↔storage reconciliation, BR-NFR-017)
	RecoverProcessedFile(ctx context.Context, pf ProcessedFile) (bool, error)
	ReconcileOnce(ctx context.Context, fn func(context.Context) error) (bool, error)

	// Users & authentication (management plane)
	GetUserByUsername(ctx context.Context, username string) (User, error)
	UpsertAdmin(ctx context.Context, username, displayName, email, passwordHash string) error
	CreateSession(ctx context.Context, userUID int64, tokenHash []byte, idleExp, absExp time.Time, clientAddr string) error
	SessionUser(ctx context.Context, tokenHash []byte) (User, error)
	TouchSession(ctx context.Context, tokenHash []byte, idleExp time.Time) error
	RevokeSession(ctx context.Context, tokenHash []byte) error

	// Lifecycle
	Ping(ctx context.Context) error
	Close()
}
