package httpserver

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestWizardJSON_EscapesScriptClose: the model is embedded in a <script> block, so a
// description containing "</script>" must not be able to close it and inject markup.
func TestWizardJSON_EscapesScriptClose(t *testing.T) {
	var m wizardModel
	m.Description = `</script><img src=x onerror=alert(1)>`
	out := wizardJSON(m)
	if contains(out, "</script>") {
		t.Fatalf("wizard JSON can break out of its script block: %s", out)
	}
}

func contains(h, n string) bool {
	return len(h) >= len(n) && (func() bool {
		for i := 0; i+len(n) <= len(h); i++ {
			if h[i:i+len(n)] == n {
				return true
			}
		}
		return false
	})()
}

// TestHydrateCoversEveryFormField is the guard for a bug that shipped: the editor
// renders the WHOLE wizard, so any control the hydration forgets is posted back
// empty on save and SILENTLY ERASES what was stored. Archiving and the poll interval
// were both being wiped that way — a pipeline quietly stopped archiving and nothing
// said so.
//
// So: every form field the server reads must be restored by hydrate(). This compares
// the two lists mechanically, because remembering to is exactly what failed.
func TestHydrateCoversEveryFormField(t *testing.T) {
	forms := readFile(t, "forms.go")
	js := readFile(t, "static/app.js")

	// Fields read by the server's form parsers.
	read := regexp.MustCompile(`r\.FormValue\("([a-z_0-9]+)"\)`)
	// Fields the wizard's hydration restores.
	restored := regexp.MustCompile(`name="([a-z_0-9]+)"`)

	rest := map[string]bool{}
	for _, m := range restored.FindAllStringSubmatch(js, -1) {
		rest[m[1]] = true
	}

	// Fields that legitimately need no hydration: they are per-datasource-creation
	// (a different form), or derived server-side from the datasource rather than
	// entered in the pipeline wizard.
	exempt := map[string]bool{
		"ds_kind": true, "ds_name": true, "ds_root": true, // the datasource form, not this one
		"src_backend": true, "src_root": true, "src_input_dir": true, // derived from the datasource
		"in_progress_dir": true, "done_dir": true, "quarantine_dir": true, "output_dir": true,
		"passthrough":     true, // per-destination now, carried in dest_json
		"output_compress": true, // per-destination now, carried in dest_json
	}

	var missed []string
	seen := map[string]bool{}
	for _, m := range read.FindAllStringSubmatch(forms, -1) {
		f := m[1]
		if rest[f] || exempt[f] || seen[f] {
			continue
		}
		seen[f] = true
		missed = append(missed, f)
	}
	sort.Strings(missed)
	if len(missed) > 0 {
		t.Fatalf("the editor does not restore these fields, so saving an edit SILENTLY ERASES them: %s",
			strings.Join(missed, ", "))
	}
}
