package store

import (
	"strconv"
	"strings"
	"time"
)

// Datasource is a reusable, named storage connection that pipelines select as
// their input and/or output location. It holds only the CONNECTION — a posix
// working directory, or an S3 endpoint + credentials — not per-pipeline
// specifics: the S3 bucket and the lifecycle sub-directories are supplied when a
// pipeline is created. Secret fields (S3.SecretKey) are held DECRYPTED here; the
// store encrypts on write and decrypts on read (mirrors Source). Persisted in
// DSR_DATASOURCE as an identity row + encrypted JSONB config blob.
type Datasource struct {
	ID        int64
	Name      string
	Kind      string   // "posix" | "s3"
	Root      string   // posix working directory (Kind=="posix")
	S3        S3Config // s3 connection (Kind=="s3"); Bucket unused at datasource level
	CreatedBy string
	CreatedOn time.Time
}

// datasourceDoc is the encrypted on-disk (JSONB) mirror of a Datasource.
type datasourceDoc struct {
	Kind string `json:"kind"`
	Root string `json:"root,omitempty"`
	S3   s3Doc  `json:"s3,omitempty"`
}

func encodeDatasource(d Datasource) (datasourceDoc, error) {
	s3, err := encodeS3(d.S3)
	if err != nil {
		return datasourceDoc{}, err
	}
	return datasourceDoc{Kind: d.Kind, Root: d.Root, S3: s3}, nil
}

func decodeDatasource(doc datasourceDoc) (Datasource, error) {
	s3, err := decodeS3(doc.S3)
	if err != nil {
		return Datasource{}, err
	}
	return Datasource{Kind: doc.Kind, Root: doc.Root, S3: s3}, nil
}

// applyDatasources materializes a pipeline's datasource references into its
// runtime storage config: the INPUT datasource sets the Source backend/root/creds
// (input + in-progress + quarantine live here); the OUTPUT datasource (which may
// be a different backend, enabling read-S3 → write-done-to-FS) becomes
// p.OutputDest (done + output live there). Per-pipeline lifecycle dirs default to
// a pipeline-name-scoped prefix so one datasource can host many pipelines. Called
// after a pipeline row is scanned; a nil datasource leaves that side untouched
// (inline config or "same as input"). Pipelines with no datasourceId keep their
// inline Source verbatim (backward compatible).
func applyDatasources(p *Pipeline, inputDS, outputDS *Datasource) {
	// The prefix scopes a pipeline's lifecycle dirs on a shared datasource. It MUST
	// be unique per pipeline: slug() is not injective (e.g. "cdr-in", "cdr_in" and
	// "cdr in" all slug to "cdr-in"), so two distinct pipelines could otherwise
	// alias to the same dirs and double-process the same files. Appending the
	// pipeline id guarantees uniqueness while staying human-readable.
	prefix := slug(p.Name)
	if prefix == "" {
		prefix = "pipeline"
	}
	prefix = prefix + "-" + strconv.FormatInt(p.ID, 10)

	if inputDS != nil {
		p.Source.Backend = inputDS.Kind
		switch inputDS.Kind {
		case "posix":
			p.Source.Root = inputDS.Root
		case "s3":
			p.Source.S3 = mergeS3Conn(inputDS.S3, p.Source.S3.Bucket, p.Source.S3.CreateBucket)
		}
		setDefault(&p.Source.InputDir, prefix+"/input")
		setDefault(&p.Source.InProgressDir, prefix+"/in-progress")
		setDefault(&p.Source.QuarantineDir, prefix+"/quarantine")
		setDefault(&p.Source.DoneDir, prefix+"/done")
		setDefault(&p.Source.OutputDir, prefix+"/output")
	}

	if outputDS != nil {
		dest := Dest{Backend: outputDS.Kind, Root: outputDS.Root}
		if outputDS.Kind == "s3" {
			bucket := p.Source.OutputBucket
			if bucket == "" {
				bucket = p.Source.S3.Bucket // fall back to the input bucket when both are s3
			}
			dest.S3 = mergeS3Conn(outputDS.S3, bucket, p.Source.S3.CreateBucket)
		}
		p.OutputDest = dest
	}

	// Default each destination's output directory under the pipeline prefix, so
	// two destinations on one datasource never collide: "<pipeline>-<id>/output/billing".
	for i := range p.Outputs {
		if p.Outputs[i].Dir == "" {
			if len(p.Outputs) == 1 && p.Source.OutputDir != "" {
				p.Outputs[i].Dir = p.Source.OutputDir // single destination: the classic output dir
			} else {
				p.Outputs[i].Dir = prefix + "/output/" + slugOr(p.Outputs[i].Name, strconv.Itoa(i+1))
			}
		}
	}
}

// applyOutputDatasource materializes one destination's storage connection from its
// datasource. A destination with no datasource writes to the pipeline's own output
// store — that is the same-backend case and stays zero-valued.
func applyOutputDatasource(o *Output, ds *Datasource, srcS3 S3Config) {
	if ds == nil {
		return
	}
	dest := Dest{Backend: ds.Kind, Root: ds.Root}
	if ds.Kind == "s3" {
		bucket := o.Bucket
		if bucket == "" {
			bucket = srcS3.Bucket
		}
		dest.S3 = mergeS3Conn(ds.S3, bucket, o.CreateBucket)
	}
	o.Dest = dest
}

// slugOr slugs s, falling back to alt when s slugs to nothing.
func slugOr(s, alt string) string {
	if v := slug(s); v != "" {
		return v
	}
	return alt
}

// mergeS3Conn overlays a per-pipeline bucket/createBucket onto a datasource's
// connection (endpoint, region, credentials).
func mergeS3Conn(conn S3Config, bucket string, createBucket bool) S3Config {
	conn.Bucket = bucket
	conn.CreateBucket = createBucket
	return conn
}

func setDefault(p *string, v string) {
	if *p == "" {
		*p = v
	}
}

// slug lowercases s and replaces any run of non-alphanumeric characters with a
// single '-', trimming leading/trailing dashes — a filesystem/S3-key-safe prefix
// derived from a pipeline name.
func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
