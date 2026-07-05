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
	ID        int64
	SrcUID    int64 // published SRC_SOURCE row for this pipeline (for file claims)
	Name      string
	Enabled   bool
	InputDir  string
	OutputDir string
	Input     spec.FormatSpec
	Transform spec.TransformSpec
	Output    spec.FormatSpec
	CreatedBy string
	CreatedOn time.Time
}

// Runner is the executable form of a pipeline's config.
func (p Pipeline) Runner() pipeline.Spec {
	return pipeline.Spec{Input: p.Input, Transform: p.Transform, Output: p.Output}
}

// ProcessedFile is the operational record of one processed input file
// (PF_PROCESSED_FILE + RS_RECONCILIATION_SUMMARY), BR-COL-005/BR-REC-002.
type ProcessedFile struct {
	FileUID      int64
	PipelineID   int64
	PipelineName string
	Name         string
	Size         int64
	Status       string // COLLECTED | DONE | SUSPENDED
	RecordsIn    int
	RecordsOut   int
	Suspended    int
	OutputName   string
	CollectedOn  time.Time
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
	UserUID       int64
	IdleExpires   time.Time
	AbsExpires    time.Time
	ClientAddr    string
}

// Store is the persistence interface used by the management and data planes.
type Store interface {
	// Configuration (management plane)
	ListPipelines(ctx context.Context) ([]Pipeline, error)
	GetPipeline(ctx context.Context, id int64) (Pipeline, error)
	CreatePipeline(ctx context.Context, p Pipeline) (int64, error)
	SetPipelineEnabled(ctx context.Context, id int64, enabled bool) error

	// Operational (data plane)
	NextFileUID(ctx context.Context) (int64, error)
	RecordProcessedFile(ctx context.Context, pf ProcessedFile) error
	ListProcessedFiles(ctx context.Context, limit int) ([]ProcessedFile, error)

	// Distributed file claim (multi-instance safety, BR-HA-003/004, BR-COL-006)
	AlreadyProcessed(ctx context.Context, srcUID int64, name string) (bool, error)
	AlreadyQuarantined(ctx context.Context, srcUID int64, name string) (bool, error)
	ClaimFile(ctx context.Context, srcUID int64, name string) (Claim, bool, error)
	HeartbeatClaim(ctx context.Context, c Claim) (bool, error)
	ReleaseClaim(ctx context.Context, c Claim) error
	CompleteClaimed(ctx context.Context, pf ProcessedFile, c Claim) (bool, error)
	QuarantineFile(ctx context.Context, pipelineID, srcUID int64, name string, size int64, c Claim) (bool, error)

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
