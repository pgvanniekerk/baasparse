package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pgvanniekerk/baasparse/internal/spec"
)

// Output is one delivery destination of a pipeline (TS 07 §7.2). A pipeline fans
// the SAME canonical records out to every destination, but each carries its own
// FormatSpec — billing may want pipe-delimited DSV with a fixed column set while
// revenue assurance wants gzipped NDJSON with everything — so the records are
// decoded and transformed once and encoded per destination.
//
// DSUID points at the destination's DS_DESTINATION row, which owns its delivery
// records (DL) and its gapless output sequence (DSQ).
type Output struct {
	Name  string // operator label, unique within the pipeline ("billing")
	DSUID int64
	// Kind is what this destination IS, not merely how it is formatted:
	//   "file"  — encode the records into an object on a storage backend
	//   "rdbms" — insert the records into a table
	// A file destination carries Format (+ Compress); an RDBMS destination carries
	// RDBMS and has no file, no format and nothing to compress. Empty means "file",
	// so every pipeline written before this distinction still loads correctly.
	Kind   string
	Format spec.FormatSpec
	RDBMS  *RDBMSTarget
	// Transform is THIS destination's output structure: which fields it carries and
	// how each is derived. Destinations genuinely differ — billing wants a narrow
	// set of billable columns, revenue assurance wants everything plus a derived
	// key — so the record is decoded once and shaped per destination. nil falls back
	// to the pipeline-level transform (legacy single-output pipelines).
	Transform *spec.TransformSpec

	Dir          string // output prefix on the destination store (file destinations)
	DatasourceID int64  // 0 = write to the pipeline's own source store
	Bucket       string // s3 datasource: the per-pipeline bucket
	CreateBucket bool
	// Dest is materialized from DatasourceID at load; zero means "same store as
	// the source" (the classic same-backend pipeline).
	Dest Dest
}

// Destination kinds.
const (
	OutputFile  = "file"
	OutputRDBMS = "rdbms"
)

// IsFile reports whether this destination writes an object (the default).
func (o Output) IsFile() bool { return o.Kind == "" || o.Kind == OutputFile }

// RDBMSTarget describes a database destination: the records of a batch are
// inserted into Table, with each output field mapped to a column.
//
// It is defined here ahead of the data-plane implementation so the destination
// model, the wizard and the delivery records already have the right shape — an
// RDBMS delivery is still one delivery of one batch, tracked and retried exactly
// like a file one. The pieces that will hook it up:
//   - an encoder whose Begin/Write/End insert rather than serialize (the Session
//     already fans records to N encoders, so a DB "encoder" needs no new plumbing);
//   - an idempotency row written in the SAME transaction as the records, keyed by
//     the batch's delivery key, so a crash between COMMIT and MarkDelivered cannot
//     insert the batch twice on retry.
type RDBMSTarget struct {
	Table string `json:"table"`
	// Columns maps output field name -> table column name. Empty means "same name".
	Columns map[string]string `json:"columns,omitempty"`
	// Mode is "insert" (default) or "upsert".
	Mode string `json:"mode,omitempty"`
}

// HasDest reports whether this output writes to a store other than the source's.
func (o Output) HasDest() bool {
	return o.Dest.Backend != "" || o.Dest.Root != "" || o.Dest.S3.Bucket != ""
}

// BatchSpec is the output consolidation policy (TS 07 §7.3.2, BR-DST-005): many
// input files are concatenated into ONE output object per destination — ten
// 100-record files become one 1000-record file. Records are preserved exactly;
// only the file boundary changes. This is what downstream billing systems expect
// instead of a spray of tiny files.
//
// A batch closes on whichever trigger fires first:
//   - MaxFiles  — enough files have arrived (the common case)
//   - MaxBytes  — enough input bytes have arrived
//   - MaxAge    — the oldest waiting file has aged out
//
// MaxAge is an ADMISSION DELAY, not an open-file lifetime: we simply decline to
// batch a partial group until its oldest file is old enough. That yields 3GPP's
// "file open-time limit" trigger without ever holding an output file open across
// a claim boundary — which is what would force a single-writer/segment-registry
// design and all the failover machinery that comes with it.
type BatchSpec struct {
	Enabled       bool  `json:"enabled,omitempty"`
	MaxFiles      int   `json:"maxFiles,omitempty"`
	MaxBytes      int64 `json:"maxBytes,omitempty"`
	MaxAgeSeconds int   `json:"maxAgeSeconds,omitempty"`
}

// DefaultBatchFiles bounds a batch when the operator sets no file count. It also
// hard-caps how many claims one batch can hold: every member is a held lease, and
// a batch that never closes holds them all.
const DefaultBatchFiles = 10

// MaxBatchFiles is the ceiling regardless of configuration — a batch of 10,000
// files would hold 10,000 leases and re-read every one of them on a retry.
const MaxBatchFiles = 1000

// Files is the effective per-batch file cap.
func (b BatchSpec) Files() int {
	n := b.MaxFiles
	if n <= 0 {
		n = DefaultBatchFiles
	}
	if n > MaxBatchFiles {
		n = MaxBatchFiles
	}
	return n
}

// MaxAge is the effective admission delay (0 = batch as soon as any trigger allows).
func (b BatchSpec) MaxAge() time.Duration {
	if b.MaxAgeSeconds <= 0 {
		return 0
	}
	return time.Duration(b.MaxAgeSeconds) * time.Second
}

// Batch is a frozen set of input files that together produce one output object per
// destination. Frozen is the operative word: once ANY destination has published,
// the membership can never change, or a retry would re-deliver already-published
// records to that destination as duplicates.
type Batch struct {
	UID      int64
	Identity string
	Files    []BatchFile
	Attempts int
}

// DeliveryKey identifies THIS batch's deliveries (the DL_OUTPUT_IDENTITY).
//
// It is the batch's UID, deliberately NOT its content identity. The content hash
// repeats whenever the same filenames and sizes come round again — a daily feed of
// fixed-size files does exactly that — and DL rows are unique per (destination,
// identity) with no status filter. Keying deliveries on the content hash would
// therefore make TODAY's batch find YESTERDAY's DELIVERED row, conclude it had
// already published, skip writing the output entirely, and still mark the files
// DONE: silent data loss. The UID is unique per batch, so the same files coming
// round again form a genuinely new batch with genuinely new deliveries.
func (b Batch) DeliveryKey() string { return "bt:" + strconv.FormatInt(b.UID, 10) }

// BatchFile is one member of a batch, carrying the record counts it contributed
// once the batch has been encoded. Those counts are PERSISTED so a batch that
// crashed after delivering but before committing can still write truthful per-file
// records on resume, instead of claiming the files held zero records.
type BatchFile struct {
	Name       string
	Size       int64
	RecordsIn  int64
	RecordsOut int64
	Suspended  int64
}

// Names returns the member names in batch order.
func (b Batch) Names() []string {
	out := make([]string, len(b.Files))
	for i, f := range b.Files {
		out[i] = f.Name
	}
	return out
}

// TotalBytes sums the members' input sizes.
func (b Batch) TotalBytes() int64 {
	var n int64
	for _, f := range b.Files {
		n += f.Size
	}
	return n
}

// BatchIdentity derives a batch's stable identity from its member set. It is what
// makes an interrupted batch RESUMABLE rather than re-formed: the same files
// always hash to the same identity, so the delivery rows written on a previous
// attempt (keyed by this identity) are found again, and the destinations that
// already published are skipped instead of being sent duplicates.
//
// It is derived from names + sizes only — never from the instance, the clock, or
// the order the scan happened to see them — so two instances that form the same
// batch collide on the unique index and one resumes the other's work.
func BatchIdentity(files []BatchFile) string {
	sorted := make([]BatchFile, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	h := sha256.New()
	for _, f := range sorted {
		h.Write([]byte(f.Name))
		h.Write([]byte{0})
		h.Write([]byte(strconv.FormatInt(f.Size, 10)))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// ValidateOutputs rejects a destination set that cannot be delivered coherently,
// at SAVE time rather than at 3am. Names are not cosmetic: they key the
// DS_DESTINATION row, the delivery records and the per-destination output
// directory, so a duplicate would silently make two destinations share one
// sequence and overwrite each other's files.
func ValidateOutputs(outs []Output) error {
	if len(outs) == 0 {
		return fmt.Errorf("pipeline needs at least one output destination")
	}
	seen := map[string]bool{}
	for i := range outs {
		o := &outs[i]
		n := strings.TrimSpace(o.Name)
		if n == "" {
			return fmt.Errorf("output destination %d has no name", i+1)
		}
		if seen[strings.ToLower(n)] {
			return fmt.Errorf("duplicate output destination name %q", n)
		}
		seen[strings.ToLower(n)] = true
		switch o.Kind {
		case "", OutputFile:
		case OutputRDBMS:
			// Guard rail until the data plane can actually insert: a pipeline that
			// silently dropped every record for a configured destination would be far
			// worse than a refusal at save time.
			return fmt.Errorf("destination %q: database destinations are not deliverable yet", n)
		default:
			return fmt.Errorf("destination %q: unknown kind %q", n, o.Kind)
		}
	}
	return nil
}

// Delivery is one destination's output for one batch (a DL_DELIVERY row).
//
// SequenceNo is RESERVED when the delivery row is created and reused on every
// retry of the same batch. That is deliberate: if a failed attempt burned a
// number, a gap in the delivered sequence would mean "an attempt failed" rather
// than "an output is missing" — and downstream gap detection (BR-DST-011) would
// page on noise. A gap therefore only ever appears for an ABANDONED batch, which
// is an operator-visible event.
type Delivery struct {
	UID        int64
	DSUID      int64
	OutputName string
	SequenceNo int64
	Status     string // PENDING | DELIVERED | FAILED
	Records    int64
	SizeBytes  int64
}

// Contribution records how many records one input file put into one delivery
// (a DC_DELIVERY_CONTRIBUTION row) — the per-file lineage that survives
// consolidation, so "which output file did this input file's records land in,
// and at which offsets" is answerable after N files merge into one.
type Contribution struct {
	FileName   string
	Records    int64 // records this file put into the output
	RecordsIn  int64 // records decoded from this file
	Suspended  int64
	FirstIndex int64
	LastIndex  int64
}
