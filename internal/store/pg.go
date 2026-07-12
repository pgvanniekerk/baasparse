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
type outputDoc struct {
	Name   string          `json:"name"`
	DSUID  int64           `json:"dsUID,omitempty"`
	Kind   string          `json:"kind,omitempty"` // "" | "file" | "rdbms"
	Format spec.FormatSpec `json:"format"`
	RDBMS  *RDBMSTarget    `json:"rdbms,omitempty"`
	// Transform is this destination's own output structure. It MUST be persisted:
	// without it a destination silently falls back to the pipeline-level transform,
	// keeping its own columns but not its own fields — so it emits the right keys
	// with null values.
	Transform    *spec.TransformSpec `json:"transform,omitempty"`
	Dir          string              `json:"dir,omitempty"`
	DatasourceID int64               `json:"datasourceID,omitempty"`
	Bucket       string              `json:"bucket,omitempty"`
	CreateBucket bool                `json:"createBucket,omitempty"`
}

type pipelineDoc struct {
	Enabled   bool               `json:"enabled"`
	InputDir  string             `json:"inputDir"`
	OutputDir string             `json:"outputDir"`
	Input     spec.FormatSpec    `json:"input"`
	Transform spec.TransformSpec `json:"transform"`
	Output    spec.FormatSpec    `json:"output"`
	// Outputs carries every destination. Absent on pipelines created before
	// multi-destination support, which decode as a single "default" destination
	// built from Output/OutputDir.
	Outputs []outputDoc `json:"outputs,omitempty"`
	Batch   BatchSpec   `json:"batch,omitempty"`
	// Source and Archive are the per-source storage backend + lifecycle + fetch
	// transport and optional archival policy, persisted in the same JSONB doc
	// (secrets encrypted). Absent on legacy pipelines.
	Source  *sourceDoc  `json:"source,omitempty"`
	Archive *archiveDoc `json:"archive,omitempty"`
}

// buildDoc composes the JSONB document for a pipeline, encrypting any secrets in
// the Source/Archive sections.
func buildDoc(pl Pipeline) (pipelineDoc, error) {
	src, err := encodeSource(pl.Source)
	if err != nil {
		return pipelineDoc{}, err
	}
	arc, err := encodeArchive(pl.Archive)
	if err != nil {
		return pipelineDoc{}, err
	}
	inputDir := pl.InputDir
	if inputDir == "" && pl.Source.InputDir != "" {
		inputDir = pl.Source.InputDir
	}
	outputDir := pl.OutputDir
	if outputDir == "" && pl.Source.OutputDir != "" {
		outputDir = pl.Source.OutputDir
	}
	outs := make([]outputDoc, 0, len(pl.Outputs))
	for _, o := range pl.Outputs {
		outs = append(outs, outputDoc{
			Name: o.Name, DSUID: o.DSUID, Kind: o.Kind, Format: o.Format, RDBMS: o.RDBMS,
			Transform: o.Transform, Dir: o.Dir,
			DatasourceID: o.DatasourceID, Bucket: o.Bucket, CreateBucket: o.CreateBucket,
		})
	}
	// Mirror the first destination into the legacy single-output fields so the
	// preview/upload paths and any older reader still see a coherent pipeline.
	legacy := pl.Output
	if len(pl.Outputs) > 0 {
		legacy = pl.Outputs[0].Format
		if outputDir == "" {
			outputDir = pl.Outputs[0].Dir
		}
	}
	return pipelineDoc{
		Enabled: pl.Enabled, InputDir: inputDir, OutputDir: outputDir,
		Input: pl.Input, Transform: pl.Transform, Output: legacy,
		Outputs: outs, Batch: pl.Batch,
		Source: src, Archive: arc,
	}, nil
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
func (p *PG) Close()                         { p.pool.Close() }

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
	// Every pipeline has at least one destination; a form that posted none gets
	// the legacy single output so the rest of the code never special-cases it.
	if len(pl.Outputs) == 0 {
		pl.Outputs = []Output{{Name: "default", Format: pl.Output, Dir: pl.OutputDir}}
	}
	if err := ValidateOutputs(pl.Outputs); err != nil {
		return 0, err
	}
	inJSON, _ := json.Marshal(pl.Input)
	trJSON, _ := json.Marshal(pl.Transform)
	inputDir := pl.InputDir
	if inputDir == "" {
		inputDir = pl.Source.InputDir
	}
	// Denormalize the resolved input + lifecycle areas into SRC_SOURCE for
	// visibility (the authoritative copy remains the JSONB doc).
	inputDirs, _ := json.Marshal([]string{inputDir})
	srcInProgress := pl.Source.InProgressDir
	srcDone := pl.Source.DoneDir
	srcQuarantine := pl.Source.QuarantineDir
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
	// One DS_DESTINATION per output. Each owns its delivery rows (DL) and its own
	// gapless output sequence (DSQ), which is precisely what lets billing and
	// revenue assurance succeed and fail independently of one another.
	for i := range pl.Outputs {
		o := &pl.Outputs[i]
		dsJSON, _ := json.Marshal(map[string]any{
			"format": o.Format, "dir": o.Dir, "datasourceID": o.DatasourceID, "bucket": o.Bucket,
		})
		if err := tx.QueryRow(ctx, `
			INSERT INTO DS_DESTINATION (DS_VERSION_NO, DS_STATUS, DS_EFFECTIVE_FROM, DS_NAME, DS_KIND, DS_SPEC, DS_SPOOL_POLICY, DS_RETENTION, DS_CREATED_BY, DS_MODIFIED_BY)
			VALUES (1,'PUBLISHED',now(),$1,'FILE',$2,'{}'::jsonb,'{}'::jsonb,$3,$3) RETURNING DS_UID`,
			pl.Name+"-"+o.Name, dsJSON, by).Scan(&o.DSUID); err != nil {
			return 0, fmt.Errorf("insert DS %q: %w", o.Name, err)
		}
	}
	// Pipeline identity
	if err := tx.QueryRow(ctx, `
		INSERT INTO PL_PIPELINE (PL_NAME, PL_DESCRIPTION, PL_STATUS, PL_CREATED_BY, PL_MODIFIED_BY)
		VALUES ($1,NULLIF($2,''),'ACTIVE',$3,$3) RETURNING PL_UID`,
		pl.Name, pl.Description, by).Scan(&plUID); err != nil {
		return 0, fmt.Errorf("insert PL: %w", err)
	}
	// The doc is composed only NOW: it carries each destination's DS_UID, which
	// exists only after the inserts above.
	doc, err := buildDoc(pl)
	if err != nil {
		return 0, err
	}
	docJSON, _ := json.Marshal(doc)
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
		VALUES (1,'PUBLISHED',now(),$1,$2,$6,$7,$8,'','*','{}'::jsonb,$3,$4,$5,$5)`,
		pl.Name+"-source", inputDirs, fdUID, plUID, by, srcInProgress, srcDone, srcQuarantine); err != nil {
		return 0, fmt.Errorf("insert SRC: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return plUID, nil
}

func (p *PG) ListPipelines(ctx context.Context) ([]Pipeline, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT PL.PL_UID, COALESCE(SRC.SRC_UID,0), PL.PL_NAME, COALESCE(PL.PL_DESCRIPTION,''), PL.PL_CREATED_ON, PLV.PLV_STAGE_GRAPH
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
		pl, err := scanPipeline(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, pl)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Materialize any datasource references (needs its own queries; done after the
	// pipeline rows are drained so the connection is free).
	for i := range out {
		if err := p.resolveDatasources(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (p *PG) GetPipeline(ctx context.Context, id int64) (Pipeline, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT PL.PL_UID, COALESCE(SRC.SRC_UID,0), PL.PL_NAME, COALESCE(PL.PL_DESCRIPTION,''), PL.PL_CREATED_ON, PLV.PLV_STAGE_GRAPH
		FROM PL_PIPELINE PL
		JOIN PLV_PIPELINE_VERSION PLV ON PLV.PLV_PL_UID = PL.PL_UID AND PLV.PLV_STATUS='PUBLISHED' AND PLV.PLV_END_DATE IS NULL
		LEFT JOIN SRC_SOURCE SRC ON SRC.SRC_PL_UID = PL.PL_UID AND SRC.SRC_STATUS='PUBLISHED' AND SRC.SRC_END_DATE IS NULL
		WHERE PL.PL_UID=$1 AND PL.PL_STATUS='ACTIVE'`, id)
	pl, err := scanPipeline(row)
	if err == pgx.ErrNoRows {
		return Pipeline{}, ErrNotFound
	}
	if err != nil {
		return Pipeline{}, err
	}
	if err := p.resolveDatasources(ctx, &pl); err != nil {
		return Pipeline{}, err
	}
	return pl, nil
}

// resolveDatasources materializes a pipeline's input/output datasource references
// into its runtime Source + OutputDest. A no-op for inline pipelines
// (DatasourceID==0). A missing/soft-deleted datasource surfaces as an error so a
// broken reference is visible rather than silently running against the wrong
// location.
// ensureDestinations gives every output a real DS_DESTINATION row.
//
// This is the upgrade path. A pipeline created before multi-destination has no
// outputs block, so it loads with one synthesized "default" destination whose
// DSUID is 0. Left alone, two things break: enabling consolidation on it would
// insert delivery rows referencing DS_UID 0 (a foreign-key violation, so EVERY
// batch fails forever), and merely toggling the pipeline would persist that zero
// back into the document, orphaning the DS row it already had.
//
// So: adopt the existing DS row if there is one (the legacy "<pipeline>-output"),
// create one if not, and write the resolved UIDs back to the document once. It is
// idempotent and a no-op for any pipeline created with destinations.
func (p *PG) ensureDestinations(ctx context.Context, pl *Pipeline) error {
	missing := false
	for _, o := range pl.Outputs {
		if o.DSUID == 0 {
			missing = true
			break
		}
	}
	if !missing {
		return nil
	}
	for i := range pl.Outputs {
		o := &pl.Outputs[i]
		if o.DSUID != 0 {
			continue
		}
		dsJSON, _ := json.Marshal(map[string]any{"format": o.Format, "dir": o.Dir})
		// Adopt the row this pipeline already owns, under either naming.
		err := p.pool.QueryRow(ctx, `
			SELECT DS_UID FROM DS_DESTINATION
			WHERE DS_NAME = ANY($1) AND DS_STATUS='PUBLISHED' AND DS_END_DATE IS NULL
			ORDER BY DS_UID LIMIT 1`,
			[]string{pl.Name + "-" + o.Name, pl.Name + "-output"}).Scan(&o.DSUID)
		if err != nil && !isNoRows(err) {
			return fmt.Errorf("resolve destination %q for pipeline %d: %w", o.Name, pl.ID, err)
		}
		if o.DSUID == 0 {
			if err := p.pool.QueryRow(ctx, `
				INSERT INTO DS_DESTINATION (DS_VERSION_NO, DS_STATUS, DS_EFFECTIVE_FROM, DS_NAME, DS_KIND, DS_SPEC, DS_SPOOL_POLICY, DS_RETENTION, DS_CREATED_BY, DS_MODIFIED_BY)
				VALUES (1,'PUBLISHED',now(),$1,'FILE',$2,'{}'::jsonb,'{}'::jsonb,'engine','engine') RETURNING DS_UID`,
				pl.Name+"-"+o.Name, dsJSON).Scan(&o.DSUID); err != nil {
				return fmt.Errorf("create destination %q for pipeline %d: %w", o.Name, pl.ID, err)
			}
		}
	}
	doc, err := buildDoc(*pl)
	if err != nil {
		return err
	}
	docJSON, _ := json.Marshal(doc)
	_, err = p.pool.Exec(ctx, `
		UPDATE PLV_PIPELINE_VERSION SET PLV_STAGE_GRAPH=$1, PLV_MODIFIED_ON=now()
		WHERE PLV_PL_UID=$2 AND PLV_STATUS='PUBLISHED' AND PLV_END_DATE IS NULL`, docJSON, pl.ID)
	return err
}

func (p *PG) resolveDatasources(ctx context.Context, pl *Pipeline) error {
	// Give every destination a real DS row before anything else needs its UID.
	if err := p.ensureDestinations(ctx, pl); err != nil {
		return err
	}
	// Each destination may live on its OWN datasource — billing on one S3 bucket,
	// revenue assurance on a different filesystem — so they resolve independently
	// of the source and of each other. Cached per id: two destinations on the same
	// datasource must not fetch (and decrypt) it twice.
	seen := map[int64]*Datasource{}
	for i := range pl.Outputs {
		id := pl.Outputs[i].DatasourceID
		if id == 0 {
			continue
		}
		ds, ok := seen[id]
		if !ok {
			d, err := p.GetDatasource(ctx, id)
			if err != nil {
				return fmt.Errorf("resolve output datasource %d for pipeline %d (destination %q): %w", id, pl.ID, pl.Outputs[i].Name, err)
			}
			ds = &d
			seen[id] = ds
		}
		applyOutputDatasource(&pl.Outputs[i], ds, pl.Source.S3)
	}

	if pl.Source.DatasourceID == 0 && pl.Source.OutputDatasourceID == 0 {
		applyDatasources(pl, nil, nil) // still default per-destination dirs
		return nil
	}
	var inDS, outDS *Datasource
	if pl.Source.DatasourceID != 0 {
		d, err := p.GetDatasource(ctx, pl.Source.DatasourceID)
		if err != nil {
			return fmt.Errorf("resolve input datasource %d for pipeline %d: %w", pl.Source.DatasourceID, pl.ID, err)
		}
		inDS = &d
	}
	if pl.Source.OutputDatasourceID != 0 && pl.Source.OutputDatasourceID != pl.Source.DatasourceID {
		d, err := p.GetDatasource(ctx, pl.Source.OutputDatasourceID)
		if err != nil {
			return fmt.Errorf("resolve output datasource %d for pipeline %d: %w", pl.Source.OutputDatasourceID, pl.ID, err)
		}
		outDS = &d
	}
	applyDatasources(pl, inDS, outDS)
	return nil
}

// --- datasources ---

func (p *PG) CreateDatasource(ctx context.Context, d Datasource) (int64, error) {
	doc, err := encodeDatasource(d)
	if err != nil {
		return 0, err
	}
	docJSON, _ := json.Marshal(doc)
	by := d.CreatedBy
	if by == "" {
		by = "operator"
	}
	var uid int64
	err = p.pool.QueryRow(ctx, `
		INSERT INTO DSR_DATASOURCE (DSR_NAME, DSR_KIND, DSR_CONFIG, DSR_CREATED_BY, DSR_MODIFIED_BY)
		VALUES ($1,$2,$3,$4,$4) RETURNING DSR_UID`, d.Name, d.Kind, docJSON, by).Scan(&uid)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrDuplicate
		}
		return 0, fmt.Errorf("insert datasource: %w", err)
	}
	return uid, nil
}

func (p *PG) ListDatasources(ctx context.Context) ([]Datasource, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT DSR_UID, DSR_NAME, DSR_CONFIG, DSR_CREATED_BY, DSR_CREATED_ON
		FROM DSR_DATASOURCE WHERE DSR_STATUS='ACTIVE' ORDER BY DSR_NAME`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Datasource
	for rows.Next() {
		d, err := scanDatasource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (p *PG) GetDatasource(ctx context.Context, id int64) (Datasource, error) {
	// No status filter: a soft-deleted (DISABLED) datasource must still resolve for
	// any pipeline that still references it by id.
	row := p.pool.QueryRow(ctx, `
		SELECT DSR_UID, DSR_NAME, DSR_CONFIG, DSR_CREATED_BY, DSR_CREATED_ON
		FROM DSR_DATASOURCE WHERE DSR_UID=$1`, id)
	d, err := scanDatasource(row)
	if err == pgx.ErrNoRows {
		return Datasource{}, ErrNotFound
	}
	return d, err
}

func (p *PG) DeleteDatasource(ctx context.Context, id int64) error {
	// Soft delete: keep the row so existing pipeline references still resolve.
	_, err := p.pool.Exec(ctx, `
		UPDATE DSR_DATASOURCE SET DSR_STATUS='DISABLED', DSR_MODIFIED_ON=now(), DSR_MODIFIED_BY='operator'
		WHERE DSR_UID=$1`, id)
	return err
}

func scanDatasource(row scannable) (Datasource, error) {
	var (
		id        int64
		name      string
		cfg       []byte
		createdBy string
		createdOn time.Time
	)
	if err := row.Scan(&id, &name, &cfg, &createdBy, &createdOn); err != nil {
		return Datasource{}, err
	}
	var doc datasourceDoc
	if err := json.Unmarshal(cfg, &doc); err != nil {
		return Datasource{}, fmt.Errorf("decode datasource %d config: %w", id, err)
	}
	d, err := decodeDatasource(doc)
	if err != nil {
		return Datasource{}, fmt.Errorf("decrypt datasource %d: %w", id, err)
	}
	d.ID, d.Name, d.CreatedBy, d.CreatedOn = id, name, createdBy, createdOn
	return d, nil
}

func (p *PG) SetPipelineEnabled(ctx context.Context, id int64, enabled bool) error {
	pl, err := p.GetPipeline(ctx, id)
	if err != nil {
		return err
	}
	pl.Enabled = enabled
	doc, err := buildDoc(pl) // preserve the full Source+Archive doc
	if err != nil {
		return err
	}
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
	if err := row.Scan(&p.ID, &p.SrcUID, &p.Name, &p.Description, &p.CreatedOn, &doc); err != nil {
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
	p.Batch = d.Batch
	for _, o := range d.Outputs {
		p.Outputs = append(p.Outputs, Output{
			Name: o.Name, DSUID: o.DSUID, Kind: o.Kind, Format: o.Format, RDBMS: o.RDBMS,
			Transform: o.Transform, Dir: o.Dir,
			DatasourceID: o.DatasourceID, Bucket: o.Bucket, CreateBucket: o.CreateBucket,
		})
	}
	src, err := decodeSource(d.Source)
	if err != nil {
		return Pipeline{}, fmt.Errorf("decode source for pipeline %d: %w", p.ID, err)
	}
	p.Source = src
	// Back-compat: a pipeline created before multi-destination has no Outputs
	// block. Materialize its single output — including its cross-backend output
	// datasource, if any — as the "default" destination, so the data plane only
	// ever deals with the Outputs slice and never with two shapes.
	if len(p.Outputs) == 0 {
		// The synthesized destination carries the pipeline-level transform EXPLICITLY.
		//
		// A nil Transform is ambiguous and the ambiguity is dangerous: the runner reads
		// it as "inherit the pipeline transform", while anything that renders a
		// destination reads it as "pass-through". A legacy pipeline loaded with nil and
		// saved back would therefore have its whole field mapping replaced by
		// pass-through — the feed silently starts emitting every raw column, unrenamed
		// and untyped, and nothing says so. Materializing it here means nil never
		// escapes the store, so the two readings cannot diverge.
		tr := d.Transform
		p.Outputs = []Output{{
			Name: "default", Kind: OutputFile, Format: d.Output, Dir: d.OutputDir,
			Transform:    &tr,
			DatasourceID: src.OutputDatasourceID, Bucket: src.OutputBucket,
			CreateBucket: src.S3.CreateBucket,
		}}
	}
	// A destination written before destinations carried their own structure also has
	// a nil transform. Same ambiguity, same fix.
	for i := range p.Outputs {
		if p.Outputs[i].Transform == nil {
			tr := d.Transform
			p.Outputs[i].Transform = &tr
		}
	}
	// Legacy DSV inputs declared their columns but not their FIELDS. The wizard now
	// derives everything from the declared fields, so a pipeline with columns and no
	// fields would open with an empty field table and save with none — the decoder
	// would then fall back to col1/col2/col3 and every downstream column would shift.
	if len(p.Input.Fields) == 0 && p.Input.DSV != nil && len(p.Input.DSV.Columns) > 0 {
		for _, c := range p.Input.DSV.Columns {
			p.Input.Fields = append(p.Input.Fields, spec.FieldSpec{Name: c})
		}
	}
	arc, err := decodeArchive(d.Archive)
	if err != nil {
		return Pipeline{}, fmt.Errorf("decode archive for pipeline %d: %w", p.ID, err)
	}
	p.Archive = arc
	// Legacy pipelines have no Source block; surface the top-level dirs so the
	// data plane and per-source resolution still see them.
	if p.Source.InputDir == "" {
		p.Source.InputDir = d.InputDir
	}
	if p.Source.OutputDir == "" {
		p.Source.OutputDir = d.OutputDir
	}
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
			COALESCE(PF.PF_COMPLETION_MARKER,''), PF.PF_COLLECTED_ON,
			COALESCE(PF.PF_ERROR_TEXT, PF.PF_REASON_CODE, '')
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
			&in, &routed, &susp, &f.OutputName, &f.CollectedOn, &f.Reason); err != nil {
			return nil, err
		}
		f.RecordsIn, f.RecordsOut, f.Suspended = int(in), int(routed), int(susp)
		out = append(out, f)
	}
	return out, rows.Err()
}

// --- fetch transport (fetcher) ---

// ListSFTPSourcePipelines returns the published pipelines whose Source.Backend is
// "sftp" — the ones the fetcher pulls from a remote endpoint and lands into the
// (posix/s3) input area. Secrets are decrypted.
func (p *PG) ListSFTPSourcePipelines(ctx context.Context) ([]Pipeline, error) {
	all, err := p.ListPipelines(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Pipeline, 0, len(all))
	for _, pl := range all {
		if pl.Source.Backend == "sftp" {
			out = append(out, pl)
		}
	}
	return out, nil
}

// --- archival (archiver) ---

// ListArchivablePF returns DONE (and, when includeQuarantined, QUARANTINED)
// processed files for a pipeline whose completion is older than olderThan and
// that have not already been archived. Ordered oldest-first.
func (p *PG) ListArchivablePF(ctx context.Context, pipelineID int64, olderThan time.Time, includeQuarantined bool) ([]ArchivablePF, error) {
	statuses := []string{"DONE"}
	if includeQuarantined {
		statuses = append(statuses, "QUARANTINED")
	}
	rows, err := p.pool.Query(ctx, `
		SELECT PF.PF_UID, PF.PF_FILE_UID, PF.PF_NAME, COALESCE(PF.PF_COMPLETION_MARKER,''),
			PF.PF_SIZE_BYTES, PF.PF_STATUS, COALESCE(PF.PF_COMPLETED_ON, PF.PF_COLLECTED_ON)
		FROM PF_PROCESSED_FILE PF
		JOIN PLV_PIPELINE_VERSION PLV ON PLV.PLV_UID = PF.PF_PLV_UID
		WHERE PLV.PLV_PL_UID = $1
		  AND PF.PF_STATUS = ANY($2)
		  AND COALESCE(PF.PF_COMPLETED_ON, PF.PF_COLLECTED_ON) < $3
		ORDER BY COALESCE(PF.PF_COMPLETED_ON, PF.PF_COLLECTED_ON) ASC`,
		pipelineID, statuses, olderThan)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArchivablePF
	for rows.Next() {
		var a ArchivablePF
		if err := rows.Scan(&a.PFUID, &a.FileUID, &a.Name, &a.OutputName, &a.Size, &a.Status, &a.CompletedOn); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// MarkArchived records one completed archive run: it writes the AR_ARCHIVE_RUN
// header and one ARF_ARCHIVE_RUN_FILE row per included file, and flips those
// processed files to ARCHIVED — all in one transaction. It ensures a minimal
// per-pipeline archive policy (AP_ARCHIVE_POLICY + its RE_REMOTE_ENDPOINT) exists
// to satisfy the audit foreign keys. Returns the AR_UID of the run.
func (p *PG) MarkArchived(ctx context.Context, pipelineID int64, run ArchiveRun) (int64, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	apUID, err := ensureArchivePolicy(ctx, tx, pipelineID, run)
	if err != nil {
		return 0, err
	}

	status := "COMPLETE"
	arStatus := status
	var arUID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO AR_ARCHIVE_RUN (AR_AP_UID, AR_FINISHED_ON, AR_ARCHIVE_NAME, AR_SIZE_BYTES, AR_CHECKSUM,
			AR_TARGET, AR_FILE_COUNT, AR_STATUS, AR_CREATED_BY, AR_MODIFIED_BY)
		VALUES ($1, now(), NULLIF($2,''), $3, NULLIF($4,''), NULLIF($5,''), $6, $7, 'engine', 'engine')
		RETURNING AR_UID`,
		apUID, run.ArchiveName, run.SizeBytes, run.Checksum, run.Target, len(run.PFUIDs), arStatus).Scan(&arUID); err != nil {
		return 0, fmt.Errorf("insert AR: %w", err)
	}
	for _, pfUID := range run.PFUIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO ARF_ARCHIVE_RUN_FILE (ARF_AR_UID, ARF_PF_UID, ARF_PRUNED, ARF_CREATED_BY, ARF_MODIFIED_BY)
			VALUES ($1, $2, $3, 'engine', 'engine')
			ON CONFLICT (ARF_AR_UID, ARF_PF_UID) DO NOTHING`,
			arUID, pfUID, run.Pruned); err != nil {
			return 0, fmt.Errorf("insert ARF for PF %d: %w", pfUID, err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE PF_PROCESSED_FILE SET PF_STATUS='ARCHIVED', PF_MODIFIED_ON=now(), PF_MODIFIED_BY='engine'
			WHERE PF_UID=$1`, pfUID); err != nil {
			return 0, fmt.Errorf("mark PF %d archived: %w", pfUID, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return arUID, nil
}

// ensureArchivePolicy finds or lazily creates the minimal system archive policy
// (and its remote-endpoint FK) for a pipeline, returning its AP_UID. The policy
// is an audit anchor for archive runs — the effective archival config lives in
// the pipeline JSONB document, not here.
func ensureArchivePolicy(ctx context.Context, q querier, pipelineID int64, run ArchiveRun) (int64, error) {
	name := fmt.Sprintf("sys-archive-pl-%d", pipelineID)
	var apUID int64
	err := q.QueryRow(ctx,
		`SELECT AP_UID FROM AP_ARCHIVE_POLICY WHERE AP_NAME=$1 AND AP_STATUS='PUBLISHED' AND AP_END_DATE IS NULL LIMIT 1`,
		name).Scan(&apUID)
	if err == nil {
		return apUID, nil
	}
	if err != pgx.ErrNoRows {
		return 0, fmt.Errorf("lookup archive policy: %w", err)
	}
	var reUID int64
	if err := q.QueryRow(ctx, `
		INSERT INTO RE_REMOTE_ENDPOINT (RE_VERSION_NO, RE_STATUS, RE_EFFECTIVE_FROM, RE_NAME, RE_PROTOCOL,
			RE_HOST, RE_PORT, RE_REMOTE_PATH, RE_CREDENTIALS, RE_POST_FETCH, RE_CREATED_BY, RE_MODIFIED_BY)
		VALUES (1,'PUBLISHED',now(),$1,'SFTP','',0,'', '{}'::jsonb,'LEAVE','engine','engine')
		RETURNING RE_UID`, name+"-endpoint").Scan(&reUID); err != nil {
		return 0, fmt.Errorf("insert archive RE: %w", err)
	}
	if err := q.QueryRow(ctx, `
		INSERT INTO AP_ARCHIVE_POLICY (AP_VERSION_NO, AP_STATUS, AP_EFFECTIVE_FROM, AP_NAME, AP_AGE_DAYS,
			AP_COMPRESSION, AP_GROUPING, AP_NAME_TEMPLATE, AP_LOCAL_RETENTION, AP_SCHEDULE, AP_RE_UID,
			AP_CREATED_BY, AP_MODIFIED_BY)
		VALUES (1,'PUBLISHED',now(),$1,0,$2,'{}'::jsonb,'','{}'::jsonb,'',$3,'engine','engine')
		RETURNING AP_UID`, name, compressionDB(run.Compression), reUID).Scan(&apUID); err != nil {
		return 0, fmt.Errorf("insert archive policy: %w", err)
	}
	return apUID, nil
}

// compressionDB maps the config compression token to the AP_COMPRESSION domain
// (GZIP|ZIP|TAR_GZ), defaulting to GZIP for an unknown/empty value.
func compressionDB(c string) string {
	switch strings.ToLower(c) {
	case "zip":
		return "ZIP"
	case "tar_gz", "tar.gz", "targz":
		return "TAR_GZ"
	default:
		return "GZIP"
	}
}

// UpdatePipeline saves an edited pipeline.
//
// Two things make this more than "write the new document":
//
//  1. A destination that is REMOVED keeps its DS_DESTINATION row. It owns delivery
//     history (DL rows, and a sequence other systems have already consumed), so
//     deleting it would either fail on the foreign key or erase the audit trail of
//     data that really was delivered. It is simply dropped from the document, which
//     stops future delivery while leaving what happened intact.
//
//  2. A destination that SURVIVES keeps its DS_UID, matched by name. That is what
//     continues its output sequence: give it a fresh DS row and its numbering would
//     restart at 1, and downstream gap detection would see the whole history vanish.
//
// Structural edits are refused while a batch is open. An open batch has already
// reserved deliveries against the destinations as they were; changing them
// underneath it can leave it referencing a destination that no longer exists, which
// it can never complete — and, if some destinations already published, cannot be
// abandoned either. Editing the name, description or enabled flag is always allowed.
func (p *PG) UpdatePipeline(ctx context.Context, pl Pipeline) error {
	if err := ValidatePipeline(pl); err != nil {
		return err
	}
	cur, err := p.GetPipeline(ctx, pl.ID)
	if err != nil {
		return err
	}
	if structuralChange(cur, pl) {
		open, err := p.ListOpenBatches(ctx, cur.SrcUID)
		if err != nil {
			return err
		}
		if len(open) > 0 {
			return fmt.Errorf("cannot change the input, transform or destinations while %d batch(es) are still in flight: "+
				"disable the pipeline, let them finish, then edit", len(open))
		}
	}

	by := pl.CreatedBy
	if by == "" {
		by = "operator"
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Carry forward each destination's DS_UID BY NAME — and look it up across EVERY
	// DS row this pipeline has ever had, not just the ones currently configured.
	//
	// A destination that was removed keeps its DS row (it owns delivery history). If
	// it is later re-added under the same name and we minted a FRESH row, its gapless
	// sequence would restart at 1 — and the output objects it writes would collide
	// with, and overwrite, the ones the original destination already delivered under
	// those numbers. Reclaiming the old row continues the sequence instead.
	existing := map[string]int64{}
	rows, err := p.pool.Query(ctx,
		`SELECT DS_NAME, DS_UID FROM DS_DESTINATION WHERE DS_NAME LIKE $1 ORDER BY DS_UID`, cur.Name+"-%")
	if err != nil {
		return fmt.Errorf("load destinations: %w", err)
	}
	prefix := strings.ToLower(cur.Name) + "-"
	for rows.Next() {
		var dsName string
		var uid int64
		if err := rows.Scan(&dsName, &uid); err != nil {
			rows.Close()
			return err
		}
		if n := strings.ToLower(dsName); strings.HasPrefix(n, prefix) {
			existing[strings.TrimPrefix(n, prefix)] = uid
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, o := range cur.Outputs {
		if o.DSUID != 0 {
			existing[strings.ToLower(o.Name)] = o.DSUID
		}
	}
	for i := range pl.Outputs {
		o := &pl.Outputs[i]
		if uid, ok := existing[strings.ToLower(o.Name)]; ok {
			o.DSUID = uid
			continue
		}
		dsJSON, _ := json.Marshal(map[string]any{
			"format": o.Format, "dir": o.Dir, "datasourceID": o.DatasourceID, "bucket": o.Bucket,
		})
		if err := tx.QueryRow(ctx, `
			INSERT INTO DS_DESTINATION (DS_VERSION_NO, DS_STATUS, DS_EFFECTIVE_FROM, DS_NAME, DS_KIND, DS_SPEC, DS_SPOOL_POLICY, DS_RETENTION, DS_CREATED_BY, DS_MODIFIED_BY)
			VALUES (1,'PUBLISHED',now(),$1,'FILE',$2,'{}'::jsonb,'{}'::jsonb,$3,$3) RETURNING DS_UID`,
			pl.Name+"-"+o.Name, dsJSON, by).Scan(&o.DSUID); err != nil {
			return fmt.Errorf("insert DS %q: %w", o.Name, err)
		}
	}

	doc, err := buildDoc(pl)
	if err != nil {
		return err
	}
	docJSON, _ := json.Marshal(doc)

	if _, err := tx.Exec(ctx, `
		UPDATE PL_PIPELINE SET PL_NAME=$2, PL_DESCRIPTION=NULLIF($3,''), PL_MODIFIED_BY=$4, PL_MODIFIED_ON=now()
		WHERE PL_UID=$1`, pl.ID, pl.Name, pl.Description, by); err != nil {
		return fmt.Errorf("update PL: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE PLV_PIPELINE_VERSION SET PLV_STAGE_GRAPH=$2, PLV_MODIFIED_BY=$3, PLV_MODIFIED_ON=now()
		WHERE PLV_PL_UID=$1 AND PLV_STATUS='PUBLISHED' AND PLV_END_DATE IS NULL`,
		pl.ID, docJSON, by); err != nil {
		return fmt.Errorf("update PLV: %w", err)
	}
	// Keep the denormalized source areas in step with the document.
	inputDirs, _ := json.Marshal([]string{doc.InputDir})
	if _, err := tx.Exec(ctx, `
		UPDATE SRC_SOURCE SET SRC_INPUT_DIRS=$2, SRC_IN_PROGRESS_DIR=$3, SRC_DONE_DIR=$4, SRC_QUARANTINE_DIR=$5,
			SRC_MODIFIED_BY=$6, SRC_MODIFIED_ON=now()
		WHERE SRC_PL_UID=$1 AND SRC_STATUS='PUBLISHED' AND SRC_END_DATE IS NULL`,
		pl.ID, inputDirs, pl.Source.InProgressDir, pl.Source.DoneDir, pl.Source.QuarantineDir, by); err != nil {
		return fmt.Errorf("update SRC: %w", err)
	}
	return tx.Commit(ctx)
}

// structuralChange reports whether an edit touches anything the data plane runs on —
// as opposed to the name, description or enabled flag, which are always safe to
// change while files are in flight.
//
// It compares only what the data plane actually reads, and it compares the two sides
// on EQUAL TERMS. Comparing a materialized pipeline against a freshly-parsed one made
// every save look structural (resolved directories, DS UIDs and decrypted credentials
// all differ), so no edit at all was possible while a batch was open — including the
// name and description edits the UI promises are always allowed, and including the
// "disable it and let it drain" step the error message tells the operator to take.
func structuralChange(before, after Pipeline) bool {
	return dataPlaneShape(before) != dataPlaneShape(after)
}

// dataPlaneShape is the part of a pipeline the runner and watcher actually behave on.
// Deliberately NOT the whole Source: its credentials are decrypted on load and its
// directories are re-derived, neither of which changes how a file is processed.
func dataPlaneShape(p Pipeline) string {
	type destShape struct {
		Name      string
		Kind      string
		Format    spec.FormatSpec
		Transform *spec.TransformSpec
		Dir       string
		DSUID     int64
		DSID      int64
		Bucket    string
	}
	shape := struct {
		Input       spec.FormatSpec
		Dests       []destShape
		Batch       BatchSpec
		InputDir    string
		Disposition string
		DSID        int64
	}{
		Input:       p.Input,
		Batch:       p.Batch,
		InputDir:    p.Source.InputDir,
		Disposition: p.Source.Disposition,
		DSID:        p.Source.DatasourceID,
	}
	for _, o := range p.Outputs {
		shape.Dests = append(shape.Dests, destShape{
			Name: o.Name, Kind: o.Kind, Format: o.Format, Transform: o.Transform,
			Dir: o.Dir, DSUID: o.DSUID, DSID: o.DatasourceID, Bucket: o.Bucket,
		})
	}
	b, _ := json.Marshal(shape)
	return string(b)
}
