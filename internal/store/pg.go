package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

// PG is the PostgreSQL-backed Store. It maps the alpha domain onto the TS 03
// schema: a Pipeline spans FD/TR/DS/PL/PLV/SRC rows (the full config chain), and
// a Processed File spans PF + RS.
type PG struct {
	pool   *pgxpool.Pool
	schema string // SQL schema (namespace) holding engine tables
	insUID *int64 // this instance's INS_UID, once registered
}

// pipelineDoc is the composed pipeline configuration stored as the PLV stage
// graph JSONB — the single source of truth the GUI edits and the runner reads.
type pipelineDoc struct {
	Enabled   bool               `json:"enabled"`
	InputDir  string             `json:"inputDir"`
	OutputDir string             `json:"outputDir"`
	Input     spec.FormatSpec    `json:"input"`
	Transform spec.TransformSpec `json:"transform"`
	Output    spec.FormatSpec    `json:"output"`
}

// OpenPG connects a pool with the given schema on the search_path. Pass "" to
// default to "baasparse" (TS 02 §2.1).
func OpenPG(ctx context.Context, dsn, schema string) (*PG, error) {
	if schema == "" {
		schema = "baasparse"
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect pool: %w", err)
	}
	return &PG{pool: pool, schema: schema}, nil
}

// CountTables returns the number of base tables in the engine schema (a simple
// confirmation after a schema apply).
func (p *PG) CountTables(ctx context.Context) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.tables WHERE table_schema=$1 AND table_type='BASE TABLE'`,
		p.schema).Scan(&n)
	return n, err
}

// ApplySchema applies schema.sql (idempotent) via the simple protocol so the
// multi-statement file runs in one shot (BR-HA-011 baseline, alpha).
func (p *PG) ApplySchema(ctx context.Context, path string) error {
	sqlBytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read schema %s: %w", path, err)
	}
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Conn().PgConn().Exec(ctx, string(sqlBytes)).ReadAll(); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

// RegisterInstance upserts this instance into INS_INSTANCE and records its UID.
func (p *PG) RegisterInstance(ctx context.Context, name, host, version, mgmtAddr string) error {
	var uid int64
	err := p.pool.QueryRow(ctx, `
		INSERT INTO INS_INSTANCE (INS_NAME, INS_HOST, INS_ENGINE_VERSION, INS_STATUS, INS_MGMT_ADDR, INS_CREATED_BY, INS_MODIFIED_BY)
		VALUES ($1,$2,$3,'READY',$4,'engine','engine')
		ON CONFLICT (INS_NAME) DO UPDATE SET INS_STATUS='READY', INS_HEARTBEAT_ON=now(), INS_MODIFIED_ON=now(), INS_MGMT_ADDR=EXCLUDED.INS_MGMT_ADDR
		RETURNING INS_UID`, name, host, version, mgmtAddr).Scan(&uid)
	if err != nil {
		return fmt.Errorf("register instance: %w", err)
	}
	p.insUID = &uid
	return nil
}

func (p *PG) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }
func (p *PG) Close()                          { p.pool.Close() }

// --- users & authentication ---

func (p *PG) GetUserByUsername(ctx context.Context, username string) (User, error) {
	var u User
	var hash *string
	err := p.pool.QueryRow(ctx, `
		SELECT U_UID, U_USERNAME, COALESCE(U_DISPLAY_NAME,''), U_STATUS, U_PASSWORD_HASH, U_MUST_CHANGE_PASSWORD
		FROM U_USER WHERE U_USERNAME=$1`, username).
		Scan(&u.UID, &u.Username, &u.DisplayName, &u.Status, &hash, &u.MustChangePassword)
	if err == pgx.ErrNoRows {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	if hash != nil {
		u.PasswordHash = *hash
	}
	u.Roles, _ = p.userRoles(ctx, u.UID)
	return u, nil
}

func (p *PG) userRoles(ctx context.Context, uid int64) ([]string, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT R.R_NAME FROM UR_USER_ROLE UR JOIN R_ROLE R ON R.R_UID=UR.UR_R_UID
		WHERE UR.UR_U_UID=$1 ORDER BY R.R_NAME`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// UpsertAdmin creates or updates a LOCAL user with the given password hash and
// grants the Administrator role (BR-USR-003/009). An empty email is stored NULL.
func (p *PG) UpsertAdmin(ctx context.Context, username, displayName, email, passwordHash string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var uid int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO U_USER (U_USERNAME, U_DISPLAY_NAME, U_EMAIL, U_AUTH_SOURCE, U_PASSWORD_HASH, U_STATUS, U_MUST_CHANGE_PASSWORD, U_PASSWORD_CHANGED_ON, U_CREATED_BY, U_MODIFIED_BY)
		VALUES ($1,$2,NULLIF($3,''),'LOCAL',$4,'ACTIVE',FALSE,now(),'admin-cli','admin-cli')
		ON CONFLICT (U_USERNAME) DO UPDATE SET
			U_PASSWORD_HASH=EXCLUDED.U_PASSWORD_HASH, U_STATUS='ACTIVE', U_MUST_CHANGE_PASSWORD=FALSE,
			U_PASSWORD_CHANGED_ON=now(), U_DISPLAY_NAME=EXCLUDED.U_DISPLAY_NAME, U_EMAIL=EXCLUDED.U_EMAIL,
			U_MODIFIED_ON=now(), U_MODIFIED_BY='admin-cli'
		RETURNING U_UID`, username, displayName, email, passwordHash).Scan(&uid); err != nil {
		return fmt.Errorf("upsert user: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO UR_USER_ROLE (UR_U_UID, UR_R_UID, UR_CREATED_BY, UR_MODIFIED_BY)
		SELECT $1, R.R_UID, 'admin-cli','admin-cli' FROM R_ROLE R WHERE R.R_NAME='Administrator'
		ON CONFLICT (UR_U_UID, UR_R_UID) DO NOTHING`, uid); err != nil {
		return fmt.Errorf("grant admin role: %w", err)
	}
	return tx.Commit(ctx)
}

func (p *PG) CreateSession(ctx context.Context, userUID int64, tokenHash []byte, idleExp, absExp time.Time, clientAddr string) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO SES_SESSION (SES_U_UID, SES_TOKEN_HASH, SES_ABSOLUTE_EXPIRES_ON, SES_IDLE_EXPIRES_ON, SES_CLIENT_ADDR, SES_STATUS, SES_CREATED_BY, SES_MODIFIED_BY)
		VALUES ($1,$2,$3,$4,$5,'ACTIVE','gui','gui')`,
		userUID, tokenHash, absExp, idleExp, clientAddr)
	return err
}

func (p *PG) SessionUser(ctx context.Context, tokenHash []byte) (User, error) {
	var u User
	var hash *string
	err := p.pool.QueryRow(ctx, `
		SELECT U.U_UID, U.U_USERNAME, COALESCE(U.U_DISPLAY_NAME,''), U.U_STATUS, U.U_PASSWORD_HASH, U.U_MUST_CHANGE_PASSWORD
		FROM SES_SESSION S JOIN U_USER U ON U.U_UID=S.SES_U_UID
		WHERE S.SES_TOKEN_HASH=$1 AND S.SES_STATUS='ACTIVE'
		  AND S.SES_IDLE_EXPIRES_ON > now() AND S.SES_ABSOLUTE_EXPIRES_ON > now()
		  AND U.U_STATUS='ACTIVE'`, tokenHash).
		Scan(&u.UID, &u.Username, &u.DisplayName, &u.Status, &hash, &u.MustChangePassword)
	if err == pgx.ErrNoRows {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	u.Roles, _ = p.userRoles(ctx, u.UID)
	return u, nil
}

func (p *PG) TouchSession(ctx context.Context, tokenHash []byte, idleExp time.Time) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE SES_SESSION SET SES_LAST_SEEN_ON=now(), SES_IDLE_EXPIRES_ON=$2, SES_MODIFIED_ON=now()
		WHERE SES_TOKEN_HASH=$1 AND SES_STATUS='ACTIVE'`, tokenHash, idleExp)
	return err
}

func (p *PG) RevokeSession(ctx context.Context, tokenHash []byte) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE SES_SESSION SET SES_STATUS='REVOKED', SES_REVOKED_ON=now(), SES_MODIFIED_ON=now()
		WHERE SES_TOKEN_HASH=$1`, tokenHash)
	return err
}

// --- configuration ---

func (p *PG) CreatePipeline(ctx context.Context, pl Pipeline) (int64, error) {
	doc := pipelineDoc{
		Enabled: pl.Enabled, InputDir: pl.InputDir, OutputDir: pl.OutputDir,
		Input: pl.Input, Transform: pl.Transform, Output: pl.Output,
	}
	docJSON, _ := json.Marshal(doc)
	inJSON, _ := json.Marshal(pl.Input)
	trJSON, _ := json.Marshal(pl.Transform)
	dsJSON, _ := json.Marshal(map[string]any{"format": pl.Output, "dir": pl.OutputDir})
	inputDirs, _ := json.Marshal([]string{pl.InputDir})
	by := pl.CreatedBy
	if by == "" {
		by = "operator"
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var fdUID, trUID, plUID, plvUID int64
	// Input Format Definition
	if err := tx.QueryRow(ctx, `
		INSERT INTO FD_FORMAT_DEFINITION (FD_VERSION_NO, FD_STATUS, FD_EFFECTIVE_FROM, FD_NAME, FD_KIND, FD_SPEC, FD_CREATED_BY, FD_MODIFIED_BY)
		VALUES (1,'PUBLISHED',now(),$1,$2,$3,$4,$4) RETURNING FD_UID`,
		pl.Name+"-input", strings.ToUpper(string(pl.Input.Kind)), inJSON, by).Scan(&fdUID); err != nil {
		return 0, fmt.Errorf("insert FD: %w", err)
	}
	// Transform Rule Set
	if err := tx.QueryRow(ctx, `
		INSERT INTO TR_TRANSFORM_RULESET (TR_VERSION_NO, TR_STATUS, TR_EFFECTIVE_FROM, TR_NAME, TR_RULES, TR_CREATED_BY, TR_MODIFIED_BY)
		VALUES (1,'PUBLISHED',now(),$1,$2,$3,$3) RETURNING TR_UID`,
		pl.Name+"-transform", trJSON, by).Scan(&trUID); err != nil {
		return 0, fmt.Errorf("insert TR: %w", err)
	}
	// Destination
	if _, err := tx.Exec(ctx, `
		INSERT INTO DS_DESTINATION (DS_VERSION_NO, DS_STATUS, DS_EFFECTIVE_FROM, DS_NAME, DS_KIND, DS_SPEC, DS_SPOOL_POLICY, DS_RETENTION, DS_CREATED_BY, DS_MODIFIED_BY)
		VALUES (1,'PUBLISHED',now(),$1,'FILE',$2,'{}'::jsonb,'{}'::jsonb,$3,$3)`,
		pl.Name+"-output", dsJSON, by); err != nil {
		return 0, fmt.Errorf("insert DS: %w", err)
	}
	// Pipeline identity
	if err := tx.QueryRow(ctx, `
		INSERT INTO PL_PIPELINE (PL_NAME, PL_STATUS, PL_CREATED_BY, PL_MODIFIED_BY)
		VALUES ($1,'ACTIVE',$2,$2) RETURNING PL_UID`, pl.Name, by).Scan(&plUID); err != nil {
		return 0, fmt.Errorf("insert PL: %w", err)
	}
	// Published pipeline version (carries the composed spec)
	if err := tx.QueryRow(ctx, `
		INSERT INTO PLV_PIPELINE_VERSION (PLV_PL_UID, PLV_VERSION_NO, PLV_STATUS, PLV_EFFECTIVE_FROM, PLV_MODE, PLV_STAGE_GRAPH, PLV_PUBLISHED_ON, PLV_CREATED_BY, PLV_MODIFIED_BY)
		VALUES ($1,1,'PUBLISHED',now(),'STREAMING',$2,now(),$3,$3) RETURNING PLV_UID`,
		plUID, docJSON, by).Scan(&plvUID); err != nil {
		return 0, fmt.Errorf("insert PLV: %w", err)
	}
	// Source (references the input FD and the pipeline)
	if _, err := tx.Exec(ctx, `
		INSERT INTO SRC_SOURCE (SRC_VERSION_NO, SRC_STATUS, SRC_EFFECTIVE_FROM, SRC_NAME, SRC_INPUT_DIRS,
			SRC_IN_PROGRESS_DIR, SRC_DONE_DIR, SRC_QUARANTINE_DIR, SRC_REJECTED_DIR, SRC_FILE_PATTERN,
			SRC_COLLECTION_POLICY, SRC_FD_UID, SRC_PL_UID, SRC_CREATED_BY, SRC_MODIFIED_BY)
		VALUES (1,'PUBLISHED',now(),$1,$2,'','','','','*','{}'::jsonb,$3,$4,$5,$5)`,
		pl.Name+"-source", inputDirs, fdUID, plUID, by); err != nil {
		return 0, fmt.Errorf("insert SRC: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return plUID, nil
}

func (p *PG) ListPipelines(ctx context.Context) ([]Pipeline, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT PL.PL_UID, COALESCE(SRC.SRC_UID,0), PL.PL_NAME, PL.PL_CREATED_ON, PLV.PLV_STAGE_GRAPH
		FROM PL_PIPELINE PL
		JOIN PLV_PIPELINE_VERSION PLV ON PLV.PLV_PL_UID = PL.PL_UID AND PLV.PLV_STATUS='PUBLISHED' AND PLV.PLV_END_DATE IS NULL
		LEFT JOIN SRC_SOURCE SRC ON SRC.SRC_PL_UID = PL.PL_UID AND SRC.SRC_STATUS='PUBLISHED' AND SRC.SRC_END_DATE IS NULL
		WHERE PL.PL_STATUS='ACTIVE'
		ORDER BY PL.PL_UID`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Pipeline
	for rows.Next() {
		p, err := scanPipeline(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (p *PG) GetPipeline(ctx context.Context, id int64) (Pipeline, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT PL.PL_UID, COALESCE(SRC.SRC_UID,0), PL.PL_NAME, PL.PL_CREATED_ON, PLV.PLV_STAGE_GRAPH
		FROM PL_PIPELINE PL
		JOIN PLV_PIPELINE_VERSION PLV ON PLV.PLV_PL_UID = PL.PL_UID AND PLV.PLV_STATUS='PUBLISHED' AND PLV.PLV_END_DATE IS NULL
		LEFT JOIN SRC_SOURCE SRC ON SRC.SRC_PL_UID = PL.PL_UID AND SRC.SRC_STATUS='PUBLISHED' AND SRC.SRC_END_DATE IS NULL
		WHERE PL.PL_UID=$1 AND PL.PL_STATUS='ACTIVE'`, id)
	pl, err := scanPipeline(row)
	if err == pgx.ErrNoRows {
		return Pipeline{}, ErrNotFound
	}
	return pl, err
}

func (p *PG) SetPipelineEnabled(ctx context.Context, id int64, enabled bool) error {
	pl, err := p.GetPipeline(ctx, id)
	if err != nil {
		return err
	}
	doc := pipelineDoc{Enabled: enabled, InputDir: pl.InputDir, OutputDir: pl.OutputDir, Input: pl.Input, Transform: pl.Transform, Output: pl.Output}
	docJSON, _ := json.Marshal(doc)
	_, err = p.pool.Exec(ctx, `
		UPDATE PLV_PIPELINE_VERSION SET PLV_STAGE_GRAPH=$1, PLV_MODIFIED_ON=now()
		WHERE PLV_PL_UID=$2 AND PLV_STATUS='PUBLISHED' AND PLV_END_DATE IS NULL`, docJSON, id)
	return err
}

type scannable interface {
	Scan(dest ...any) error
}

func scanPipeline(row scannable) (Pipeline, error) {
	var (
		p   Pipeline
		doc []byte
	)
	if err := row.Scan(&p.ID, &p.SrcUID, &p.Name, &p.CreatedOn, &doc); err != nil {
		return Pipeline{}, err
	}
	var d pipelineDoc
	if err := json.Unmarshal(doc, &d); err != nil {
		return Pipeline{}, fmt.Errorf("decode stage graph for pipeline %d: %w", p.ID, err)
	}
	p.Enabled = d.Enabled
	p.InputDir = d.InputDir
	p.OutputDir = d.OutputDir
	p.Input = d.Input
	p.Transform = d.Transform
	p.Output = d.Output
	return p, nil
}

// --- operational ---

func (p *PG) NextFileUID(ctx context.Context) (int64, error) {
	var v int64
	err := p.pool.QueryRow(ctx, `
		UPDATE SQ_SEQUENCE_ALLOCATOR SET SQ_LAST_VALUE = SQ_LAST_VALUE + 1, SQ_MODIFIED_ON=now()
		WHERE SQ_NAME='FILE_UID' RETURNING SQ_LAST_VALUE`).Scan(&v)
	return v, err
}

func (p *PG) RecordProcessedFile(ctx context.Context, pf ProcessedFile) error {
	srcUID, plvUID, err := resolveSrcPlv(ctx, p.pool, pf.PipelineID)
	if err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := insertPFAndRS(ctx, tx, pf, srcUID, plvUID, p.insUID); err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicate // already processed (same source+name)
		}
		return err
	}
	return tx.Commit(ctx)
}

func (p *PG) ListProcessedFiles(ctx context.Context, limit int) ([]ProcessedFile, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := p.pool.Query(ctx, `
		SELECT PF.PF_FILE_UID, PL.PL_UID, PL.PL_NAME, PF.PF_NAME, PF.PF_STATUS,
			COALESCE(RS.RS_IN_COUNT,0), COALESCE(RS.RS_ROUTED_COUNT,0), COALESCE(RS.RS_SUSPENDED_COUNT,0),
			COALESCE(PF.PF_COMPLETION_MARKER,''), PF.PF_COLLECTED_ON
		FROM PF_PROCESSED_FILE PF
		JOIN PLV_PIPELINE_VERSION PLV ON PLV.PLV_UID = PF.PF_PLV_UID
		JOIN PL_PIPELINE PL ON PL.PL_UID = PLV.PLV_PL_UID
		LEFT JOIN RS_RECONCILIATION_SUMMARY RS ON RS.RS_PF_UID = PF.PF_UID AND RS.RS_SCOPE='FILE'
		ORDER BY PF.PF_FILE_UID DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProcessedFile
	for rows.Next() {
		var f ProcessedFile
		var in, routed, susp int64
		if err := rows.Scan(&f.FileUID, &f.PipelineID, &f.PipelineName, &f.Name, &f.Status,
			&in, &routed, &susp, &f.OutputName, &f.CollectedOn); err != nil {
			return nil, err
		}
		f.RecordsIn, f.RecordsOut, f.Suspended = int(in), int(routed), int(susp)
		out = append(out, f)
	}
	return out, rows.Err()
}
