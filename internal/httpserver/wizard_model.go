package httpserver

import (
	"encoding/json"
	"strings"

	"github.com/pgvanniekerk/baasparse/internal/spec"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

// wizardModel is an existing pipeline in the shape the wizard's JavaScript works
// in, so editing reuses the create wizard rather than duplicating it. The server
// renders it as one JSON blob and the script hydrates itself from it.
//
// It is deliberately the wizard's shape, not the store's: the two differ (a
// delimiter is "," in the spec but "comma" in a <select>), and doing the mapping
// here — once, in typed Go — beats scattering it through the template.
type wizardModel struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`

	DatasourceID       int64  `json:"datasourceID"`
	Bucket             string `json:"bucket"`
	CreateBucket       bool   `json:"createBucket"`
	OutputDatasourceID int64  `json:"outputDatasourceID"`
	OutputBucket       string `json:"outputBucket"`
	Disposition        string `json:"disposition"`
	PollSeconds        int    `json:"pollSeconds"`

	// Archive is restored too. The editor renders the WHOLE wizard, so anything the
	// model forgets comes back empty on save and silently erases what was stored —
	// a pipeline would quietly stop archiving and nothing would say so.
	Archive struct {
		Enabled            bool   `json:"enabled"`
		AgeDays            int    `json:"ageDays"`
		Compression        string `json:"compression"`
		ScheduleSeconds    int    `json:"scheduleSeconds"`
		IncludeQuarantined bool   `json:"includeQuarantined"`
		DestBackend        string `json:"destBackend"`
		DestRoot           string `json:"destRoot"`
	} `json:"archive"`

	Input struct {
		Kind       string `json:"kind"`
		Delimiter  string `json:"delimiter"`
		HasHeader  bool   `json:"hasHeader"`
		JSONMode   string `json:"jsonMode"`
		XMLRoot    string `json:"xmlRoot"`
		XMLRecord  string `json:"xmlRecord"`
		Container  bool   `json:"container"`
		MemberGlob string `json:"memberGlob"`
		Fields     []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"fields"`
	} `json:"input"`

	Batch struct {
		Enabled       bool  `json:"enabled"`
		MaxFiles      int   `json:"maxFiles"`
		MaxMB         int64 `json:"maxMB"`
		MaxAgeSeconds int   `json:"maxAgeSeconds"`
	} `json:"batch"`

	Destinations []destForm `json:"destinations"`
}

// buildWizardModel maps a stored pipeline into the wizard's shape.
func buildWizardModel(p store.Pipeline) wizardModel {
	var m wizardModel
	m.ID = p.ID
	m.Name = p.Name
	m.Description = p.Description
	m.Enabled = p.Enabled
	m.DatasourceID = p.Source.DatasourceID
	m.Bucket = p.Source.S3.Bucket
	m.CreateBucket = p.Source.S3.CreateBucket
	m.OutputDatasourceID = p.Source.OutputDatasourceID
	m.OutputBucket = p.Source.OutputBucket
	m.Disposition = p.Source.Disposition
	m.PollSeconds = p.Source.PollSeconds
	if a := p.Archive; a != nil {
		m.Archive.Enabled = a.Enabled
		m.Archive.AgeDays = a.AgeDays
		m.Archive.Compression = a.Compression
		m.Archive.ScheduleSeconds = a.ScheduleSeconds
		m.Archive.IncludeQuarantined = a.IncludeQuarantined
		m.Archive.DestBackend = a.Dest.Backend
		m.Archive.DestRoot = a.Dest.Root
	}

	m.Input.Kind = string(p.Input.Kind)
	if p.Input.DSV != nil {
		m.Input.Delimiter = delimiterName(p.Input.DSV.Delimiter)
		m.Input.HasHeader = p.Input.DSV.HasHeader
	}
	if p.Input.JSON != nil {
		m.Input.JSONMode = p.Input.JSON.Mode
	}
	if p.Input.XML != nil {
		m.Input.XMLRoot = p.Input.XML.RootElement
		m.Input.XMLRecord = p.Input.XML.RecordElement
	}
	m.Input.Container = p.Input.Container == "targz"
	m.Input.MemberGlob = p.Input.MemberGlob
	for _, f := range p.Input.Fields {
		m.Input.Fields = append(m.Input.Fields, struct {
			Name string `json:"name"`
			Type string `json:"type"`
		}{Name: f.Name, Type: string(f.Type)})
	}

	m.Batch.Enabled = p.Batch.Enabled
	m.Batch.MaxFiles = p.Batch.MaxFiles
	m.Batch.MaxAgeSeconds = p.Batch.MaxAgeSeconds
	if p.Batch.MaxBytes > 0 {
		m.Batch.MaxMB = p.Batch.MaxBytes / (1024 * 1024)
	}

	for _, o := range p.Outputs {
		m.Destinations = append(m.Destinations, outputToForm(o))
	}
	return m
}

// outputToForm is the inverse of parseOutputs: a stored destination back into the
// blob the editor round-trips. It must stay the exact inverse, or editing a
// pipeline would quietly change destinations the operator never touched.
func outputToForm(o store.Output) destForm {
	var df destForm
	df.Name = o.Name
	df.Kind = o.Kind
	if df.Kind == "" {
		df.Kind = store.OutputFile
	}
	df.DatasourceID = o.DatasourceID
	df.Bucket = o.Bucket
	df.Dir = o.Dir

	df.Format.Kind = string(o.Format.Kind)
	if o.Format.DSV != nil {
		df.Format.Delimiter = delimiterName(o.Format.DSV.Delimiter)
		df.Format.HasHeader = o.Format.DSV.HasHeader
	}
	if o.Format.JSON != nil {
		df.Format.JSONMode = o.Format.JSON.Mode
	}
	if o.Format.XML != nil {
		df.Format.XMLRoot = o.Format.XML.RootElement
		df.Format.XMLRecord = o.Format.XML.RecordElement
	}
	df.Compress = o.Format.Compress == "gzip"

	if o.RDBMS != nil {
		df.Table = o.RDBMS.Table
		df.DBMode = o.RDBMS.Mode
	}

	if o.Transform == nil || o.Transform.PassThrough {
		df.PassThrough = true
		return df
	}
	for _, f := range o.Transform.Fields {
		df.Fields = append(df.Fields, destFormField{
			Output: f.Output,
			Kind:   string(f.Kind),
			Source: f.Source,
			Const:  f.Const,
			Parts:  f.Parts,
			Sep:    f.Sep,
			Type:   string(f.Type),
		})
	}
	return df
}

// delimiterName maps a delimiter character back to the wizard's select value — the
// inverse of delimiterValue.
func delimiterName(d string) string {
	switch d {
	case "\t":
		return "tab"
	case "|":
		return "pipe"
	case ";":
		return "semicolon"
	default:
		return "comma"
	}
}

// wizardJSON renders the model for the page.
//
// The blob is embedded in a <script> block, so operator-supplied text (a
// description, a destination name) containing "</script>" would otherwise close the
// tag early and inject markup. encoding/json escapes <, > and & to \u003c, \u003e
// and \u0026 by DEFAULT, which makes that impossible — this relies on that, so do
// not swap in an Encoder with SetEscapeHTML(false).
//
// A model that cannot be rendered yields "null": the wizard then opens empty, which
// is visibly wrong rather than subtly wrong.
func wizardJSON(m wizardModel) string {
	b, err := json.Marshal(m)
	if err != nil {
		return "null"
	}
	return string(b)
}

// mergeForEdit applies an edit to a stored pipeline.
//
// The rule is: an edit may only change what the editor can SHOW. Everything else is
// carried forward verbatim.
//
// Taking the posted pipeline wholesale is what made editing dangerous: the wizard
// renders a subset of a pipeline, so every field it does NOT render came back as a
// zero value and silently erased what was stored. That is how an S3 archive
// destination lost its endpoint, bucket and credentials — and archiving then stopped
// forever, with nothing to show why. Starting from the stored pipeline and
// overwriting only the wizard's own fields makes that impossible by construction,
// rather than by remembering.
//
// It also means secrets never have to be round-tripped through the browser to
// survive an edit.
func mergeForEdit(stored, posted store.Pipeline) store.Pipeline {
	out := stored // everything the wizard cannot show survives untouched

	// --- fields the wizard genuinely owns ---
	out.Name = posted.Name
	out.Description = posted.Description
	out.Enabled = posted.Enabled
	out.Input = posted.Input
	out.Input.DSV = keepDelimiter(stored.Input.DSV, out.Input.DSV)
	out.Transform = posted.Transform
	out.Output = posted.Output
	out.Batch = posted.Batch

	// Source: only the parts the wizard renders. Backend, root, the lifecycle
	// directories and any credentials stay as stored — re-deriving the directories
	// from a new name would relocate the watched folders and strand the files already
	// sitting in them.
	out.Source.DatasourceID = posted.Source.DatasourceID
	out.Source.OutputDatasourceID = posted.Source.OutputDatasourceID
	out.Source.OutputBucket = posted.Source.OutputBucket
	out.Source.Disposition = posted.Source.Disposition
	out.Source.PollSeconds = posted.Source.PollSeconds
	out.Source.S3.Bucket = posted.Source.S3.Bucket
	out.Source.S3.CreateBucket = posted.Source.S3.CreateBucket

	// Archive: the wizard shows the policy and a posix destination root. An S3 archive
	// destination's endpoint, region, bucket and credentials are NOT rendered, so they
	// are preserved rather than blanked.
	out.Archive = mergeArchive(stored.Archive, posted.Archive)

	// Destinations: the wizard owns them, except for the bits it does not render —
	// the DS row identity (which owns the output sequence) and the create-bucket flag.
	out.Outputs = make([]store.Output, len(posted.Outputs))
	copy(out.Outputs, posted.Outputs)
	prior := map[string]store.Output{}
	for _, o := range stored.Outputs {
		prior[strings.ToLower(o.Name)] = o
	}
	for i := range out.Outputs {
		if p, ok := prior[strings.ToLower(out.Outputs[i].Name)]; ok {
			out.Outputs[i].DSUID = p.DSUID
			out.Outputs[i].CreateBucket = p.CreateBucket
			out.Outputs[i].Format.DSV = keepDelimiter(p.Format.DSV, out.Outputs[i].Format.DSV)
		}
	}
	return out
}

// keepDelimiter preserves a delimiter the wizard cannot express.
//
// The wizard's delimiter is a four-option select, so delimiterName maps anything
// outside {tab, pipe, semicolon} to "comma". A pipeline using some other character
// would therefore come back as a comma on a save that never touched it. When the
// selected option still NAMES the stored delimiter, the stored one is the truth.
func keepDelimiter(stored, posted *spec.DSVSpec) *spec.DSVSpec {
	if stored == nil || posted == nil {
		return posted
	}
	if delimiterName(stored.Delimiter) == delimiterName(posted.Delimiter) {
		posted.Delimiter = stored.Delimiter
	}
	return posted
}

// mergeArchive keeps the stored archive destination's connection (an S3 endpoint,
// region, bucket and credentials) while taking the policy the wizard shows.
func mergeArchive(stored, posted *store.Archive) *store.Archive {
	if posted == nil || !posted.Enabled {
		if stored == nil {
			return posted
		}
		// Archiving turned off: keep the destination so turning it back on does not
		// require re-entering the connection.
		off := *stored
		off.Enabled = false
		return &off
	}
	merged := *posted
	if stored != nil {
		merged.Dest = stored.Dest // connection + credentials the wizard cannot show
		if posted.Dest.Backend != "" {
			merged.Dest.Backend = posted.Dest.Backend
		}
		if posted.Dest.Root != "" {
			merged.Dest.Root = posted.Dest.Root
		}
	}
	return &merged
}
