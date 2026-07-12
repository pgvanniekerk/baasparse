package store

import (
	"fmt"

	"github.com/pgvanniekerk/baasparse/internal/secret"
)

// Source is a pipeline's runtime input configuration: where its files come from
// (backend + optional SFTP fetch transport), where the lifecycle areas live, the
// done disposition, and the declared input fields the creation wizard offers as
// dropdowns. Secret fields (S3.SecretKey, Remote.Password, Remote.Key) are held
// DECRYPTED here — the store decrypts on read and encrypts on write. It is
// persisted inside the pipeline JSONB document (pipelineDoc.Source), NOT as SQL
// columns.
type Source struct {
	Backend       string       // "posix" | "s3" | "sftp" ("sftp" => fetcher lands into a posix InputDir)
	InputDir      string       // input location key/prefix the watcher scans
	Root          string       // posix root for the landing/lifecycle areas
	S3            S3Config     // used when Backend=="s3"
	Remote        RemoteConfig // used when Backend=="sftp" (fetch transport)
	DoneDir       string       // completed-file area (empty => engine default)
	InProgressDir string       // reserved lifecycle area (empty => engine default)
	QuarantineDir string       // poison-file area (empty => <done>/quarantine)
	OutputDir     string       // output location (empty => pipeline/engine default)
	Disposition   string       // "done" | "delete" | "leaveMarked" (empty => "done")
	Fields        []FieldDecl  // declared input fields (wizard dropdowns)
	PollSeconds   int          // scan interval override (0 => engine default)
	// Datasource references (0 => inline config above). When set, the store
	// materializes the referenced datasource's connection into Backend/Root/S3 at
	// read time (applyDatasources). OutputDatasourceID lets output/done land on a
	// different backend than the input (cross-backend, e.g. read S3 → write FS).
	DatasourceID       int64
	OutputDatasourceID int64
	OutputBucket       string // per-pipeline output bucket when the output datasource is s3
}

// S3Config holds S3-compatible object-store parameters. SecretKey is plaintext at
// runtime (encrypted at rest as SecretKeyEnc).
type S3Config struct {
	Endpoint     string
	Region       string
	Bucket       string
	AccessKey    string
	SecretKey    string
	UseSSL       bool
	CreateBucket bool
}

// RemoteConfig holds an SFTP fetch endpoint. Password and Key are plaintext at
// runtime (encrypted at rest as PasswordEnc / KeyEnc).
type RemoteConfig struct {
	Host      string
	Port      int
	User      string
	Password  string
	Key       string // PEM private key
	Path      string // remote directory to fetch from
	PostFetch string // "leave" | "delete" | "move"
	MovePath  string // remote move target when PostFetch=="move"
	HostKey   string // known host key (optional pin)
}

// FieldDecl is one declared input field offered to the transform wizard.
type FieldDecl struct {
	Name string
	Type string
}

// Archive is a pipeline's optional archival configuration (age-out of DONE files
// to a destination, with optional pruning). Persisted inside the pipeline JSONB
// document (pipelineDoc.Archive).
type Archive struct {
	Enabled            bool
	AgeDays            int
	Compression        string // "gzip" | "zip" | "tar_gz"
	Dest               Dest
	ScheduleSeconds    int
	IncludeQuarantined bool
}

// Dest is an archive destination (a storage backend + credentials).
type Dest struct {
	Backend string // "posix" | "s3" | "sftp"
	Root    string
	S3      S3Config
	Remote  RemoteConfig
}

// --- on-disk (encrypted) document mirror ---

type sourceDoc struct {
	Backend            string     `json:"backend,omitempty"`
	InputDir           string     `json:"inputDir,omitempty"`
	Root               string     `json:"root,omitempty"`
	S3                 s3Doc      `json:"s3,omitempty"`
	Remote             remoteDoc  `json:"remote,omitempty"`
	DoneDir            string     `json:"doneDir,omitempty"`
	InProgressDir      string     `json:"inProgressDir,omitempty"`
	QuarantineDir      string     `json:"quarantineDir,omitempty"`
	OutputDir          string     `json:"outputDir,omitempty"`
	Disposition        string     `json:"disposition,omitempty"`
	Fields             []fieldDoc `json:"fields,omitempty"`
	PollSeconds        int        `json:"pollSeconds,omitempty"`
	DatasourceID       int64      `json:"datasourceId,omitempty"`
	OutputDatasourceID int64      `json:"outputDatasourceId,omitempty"`
	OutputBucket       string     `json:"outputBucket,omitempty"`
}

type s3Doc struct {
	Endpoint     string `json:"endpoint,omitempty"`
	Region       string `json:"region,omitempty"`
	Bucket       string `json:"bucket,omitempty"`
	AccessKey    string `json:"accessKey,omitempty"`
	SecretKeyEnc string `json:"secretKeyEnc,omitempty"`
	UseSSL       bool   `json:"useSSL,omitempty"`
	CreateBucket bool   `json:"createBucket,omitempty"`
}

type remoteDoc struct {
	Host        string `json:"host,omitempty"`
	Port        int    `json:"port,omitempty"`
	User        string `json:"user,omitempty"`
	PasswordEnc string `json:"passwordEnc,omitempty"`
	KeyEnc      string `json:"keyEnc,omitempty"`
	Path        string `json:"path,omitempty"`
	PostFetch   string `json:"postFetch,omitempty"`
	MovePath    string `json:"movePath,omitempty"`
	HostKey     string `json:"hostKey,omitempty"`
}

type fieldDoc struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

type archiveDoc struct {
	Enabled            bool    `json:"enabled,omitempty"`
	AgeDays            int     `json:"ageDays,omitempty"`
	Compression        string  `json:"compression,omitempty"`
	Dest               destDoc `json:"dest,omitempty"`
	ScheduleSeconds    int     `json:"scheduleSeconds,omitempty"`
	IncludeQuarantined bool    `json:"includeQuarantined,omitempty"`
}

type destDoc struct {
	Backend string    `json:"backend,omitempty"`
	Root    string    `json:"root,omitempty"`
	S3      s3Doc     `json:"s3,omitempty"`
	Remote  remoteDoc `json:"remote,omitempty"`
}

// --- encode (encrypt) / decode (decrypt) ---

func encodeSource(s Source) (*sourceDoc, error) {
	if isZeroSource(s) {
		return nil, nil
	}
	s3, err := encodeS3(s.S3)
	if err != nil {
		return nil, err
	}
	rm, err := encodeRemote(s.Remote)
	if err != nil {
		return nil, err
	}
	d := &sourceDoc{
		Backend: s.Backend, InputDir: s.InputDir, Root: s.Root, S3: s3, Remote: rm,
		DoneDir: s.DoneDir, InProgressDir: s.InProgressDir, QuarantineDir: s.QuarantineDir,
		OutputDir: s.OutputDir, Disposition: s.Disposition, PollSeconds: s.PollSeconds,
		DatasourceID: s.DatasourceID, OutputDatasourceID: s.OutputDatasourceID, OutputBucket: s.OutputBucket,
	}
	for _, f := range s.Fields {
		d.Fields = append(d.Fields, fieldDoc{Name: f.Name, Type: f.Type})
	}
	return d, nil
}

func decodeSource(d *sourceDoc) (Source, error) {
	if d == nil {
		return Source{}, nil
	}
	s3, err := decodeS3(d.S3)
	if err != nil {
		return Source{}, err
	}
	rm, err := decodeRemote(d.Remote)
	if err != nil {
		return Source{}, err
	}
	s := Source{
		Backend: d.Backend, InputDir: d.InputDir, Root: d.Root, S3: s3, Remote: rm,
		DoneDir: d.DoneDir, InProgressDir: d.InProgressDir, QuarantineDir: d.QuarantineDir,
		OutputDir: d.OutputDir, Disposition: d.Disposition, PollSeconds: d.PollSeconds,
		DatasourceID: d.DatasourceID, OutputDatasourceID: d.OutputDatasourceID, OutputBucket: d.OutputBucket,
	}
	for _, f := range d.Fields {
		s.Fields = append(s.Fields, FieldDecl{Name: f.Name, Type: f.Type})
	}
	return s, nil
}

func encodeArchive(a *Archive) (*archiveDoc, error) {
	if a == nil {
		return nil, nil
	}
	dest, err := encodeDest(a.Dest)
	if err != nil {
		return nil, err
	}
	return &archiveDoc{
		Enabled: a.Enabled, AgeDays: a.AgeDays, Compression: a.Compression, Dest: dest,
		ScheduleSeconds: a.ScheduleSeconds, IncludeQuarantined: a.IncludeQuarantined,
	}, nil
}

func decodeArchive(d *archiveDoc) (*Archive, error) {
	if d == nil {
		return nil, nil
	}
	dest, err := decodeDest(d.Dest)
	if err != nil {
		return nil, err
	}
	return &Archive{
		Enabled: d.Enabled, AgeDays: d.AgeDays, Compression: d.Compression, Dest: dest,
		ScheduleSeconds: d.ScheduleSeconds, IncludeQuarantined: d.IncludeQuarantined,
	}, nil
}

func encodeDest(d Dest) (destDoc, error) {
	s3, err := encodeS3(d.S3)
	if err != nil {
		return destDoc{}, err
	}
	rm, err := encodeRemote(d.Remote)
	if err != nil {
		return destDoc{}, err
	}
	return destDoc{Backend: d.Backend, Root: d.Root, S3: s3, Remote: rm}, nil
}

func decodeDest(d destDoc) (Dest, error) {
	s3, err := decodeS3(d.S3)
	if err != nil {
		return Dest{}, err
	}
	rm, err := decodeRemote(d.Remote)
	if err != nil {
		return Dest{}, err
	}
	return Dest{Backend: d.Backend, Root: d.Root, S3: s3, Remote: rm}, nil
}

func encodeS3(s S3Config) (s3Doc, error) {
	enc, err := secret.Encrypt(s.SecretKey)
	if err != nil {
		return s3Doc{}, fmt.Errorf("encrypt s3 secret key: %w", err)
	}
	return s3Doc{
		Endpoint: s.Endpoint, Region: s.Region, Bucket: s.Bucket, AccessKey: s.AccessKey,
		SecretKeyEnc: enc, UseSSL: s.UseSSL, CreateBucket: s.CreateBucket,
	}, nil
}

func decodeS3(d s3Doc) (S3Config, error) {
	sk, err := secret.Decrypt(d.SecretKeyEnc)
	if err != nil {
		return S3Config{}, fmt.Errorf("decrypt s3 secret key: %w", err)
	}
	return S3Config{
		Endpoint: d.Endpoint, Region: d.Region, Bucket: d.Bucket, AccessKey: d.AccessKey,
		SecretKey: sk, UseSSL: d.UseSSL, CreateBucket: d.CreateBucket,
	}, nil
}

func encodeRemote(r RemoteConfig) (remoteDoc, error) {
	pw, err := secret.Encrypt(r.Password)
	if err != nil {
		return remoteDoc{}, fmt.Errorf("encrypt remote password: %w", err)
	}
	key, err := secret.Encrypt(r.Key)
	if err != nil {
		return remoteDoc{}, fmt.Errorf("encrypt remote key: %w", err)
	}
	return remoteDoc{
		Host: r.Host, Port: r.Port, User: r.User, PasswordEnc: pw, KeyEnc: key,
		Path: r.Path, PostFetch: r.PostFetch, MovePath: r.MovePath, HostKey: r.HostKey,
	}, nil
}

func decodeRemote(d remoteDoc) (RemoteConfig, error) {
	pw, err := secret.Decrypt(d.PasswordEnc)
	if err != nil {
		return RemoteConfig{}, fmt.Errorf("decrypt remote password: %w", err)
	}
	key, err := secret.Decrypt(d.KeyEnc)
	if err != nil {
		return RemoteConfig{}, fmt.Errorf("decrypt remote key: %w", err)
	}
	return RemoteConfig{
		Host: d.Host, Port: d.Port, User: d.User, Password: pw, Key: key,
		Path: d.Path, PostFetch: d.PostFetch, MovePath: d.MovePath, HostKey: d.HostKey,
	}, nil
}

// isZeroSource reports whether a Source carries no configuration worth persisting.
func isZeroSource(s Source) bool {
	return s.Backend == "" && s.InputDir == "" && s.Root == "" &&
		s.DoneDir == "" && s.InProgressDir == "" && s.QuarantineDir == "" &&
		s.OutputDir == "" && s.Disposition == "" && s.PollSeconds == 0 &&
		len(s.Fields) == 0 && s.S3 == (S3Config{}) && s.Remote == (RemoteConfig{}) &&
		s.DatasourceID == 0 && s.OutputDatasourceID == 0 && s.OutputBucket == ""
}
