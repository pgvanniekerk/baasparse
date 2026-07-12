package httpserver

import (
	"encoding/json"

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
