package httpserver

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The wizard's behaviour lives in app.js, and it is invisible to every other test
// here: the server-side tests post forms directly, so a broken button looks exactly
// like a working one. A bulk edit once deleted a dozen modal functions while leaving
// their call sites behind — braces stayed balanced, the file still parsed, and the
// "Add destination" button simply did nothing, because a delegated click handler
// that throws is silent.
//
// These tests are the cheap guard for that class of break: every function the script
// calls must exist, and the controls the template wires up must be handled.

var (
	reFuncDef  = regexp.MustCompile(`(?m)^\s*function\s+([A-Za-z_$][\w$]*)\s*\(`)
	reCall     = regexp.MustCompile(`(?m)\b([A-Za-z_$][\w$]*)\s*\(`)
	reDataAct  = regexp.MustCompile(`data-action="([a-z-]+)"`)
	reHandled  = regexp.MustCompile(`action === "([a-z-]+)"`)
	reFuncName = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)
)

// browserGlobals are the identifiers app.js may legitimately call without defining.
var browserGlobals = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "catch": true, "return": true,
	"function": true, "typeof": true, "new": true, "Number": true, "String": true, "Boolean": true,
	"Array": true, "Object": true, "JSON": true, "Math": true, "Date": true, "RegExp": true,
	"Error": true, "Option": true, "URLSearchParams": true, "FormData": true, "Promise": true,
	"fetch": true, "setTimeout": true, "clearTimeout": true, "setInterval": true, "parseInt": true,
	"parseFloat": true, "isNaN": true, "encodeURIComponent": true, "decodeURIComponent": true,
	"alert": true, "confirm": true, "console": true, "document": true, "window": true,
}

// TestAppJS_NoCallsToUndefinedFunctions fails when the script calls a bare function
// it never defines — the exact shape of the bug that silently killed the wizard.
func TestAppJS_NoCallsToUndefinedFunctions(t *testing.T) {
	src := stripCommentsAndStrings(readAppJS(t))

	defined := map[string]bool{}
	for _, m := range reFuncDef.FindAllStringSubmatch(src, -1) {
		defined[m[1]] = true
	}
	if len(defined) < 10 {
		t.Fatalf("only found %d function definitions — the scanner is broken, not the script", len(defined))
	}

	// Only bare calls matter: a member call (foo.bar()) is not a local function.
	var missing []string
	seen := map[string]bool{}
	for _, loc := range reCall.FindAllStringSubmatchIndex(src, -1) {
		name := src[loc[2]:loc[3]]
		if defined[name] || browserGlobals[name] || seen[name] {
			continue
		}
		// Skip member/property calls and declarations.
		if loc[2] > 0 {
			switch src[loc[2]-1] {
			case '.', '"', '\'', '`':
				continue
			}
		}
		// Only flag things that LOOK like our own helpers (camelCase, no dots).
		if !reFuncName.MatchString(name) {
			continue
		}
		seen[name] = true
		missing = append(missing, name)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("app.js calls functions that are never defined — the wizard will die "+
			"silently at the first click that reaches one: %s", strings.Join(missing, ", "))
	}
}

// TestAppJS_EveryDataActionIsHandled fails when the template wires a control to a
// data-action the script never handles — a button that renders but does nothing.
func TestAppJS_EveryDataActionIsHandled(t *testing.T) {
	js := readAppJS(t)
	tpl := readFile(t, "templates/pipeline_new.html")

	// A control is "handled" if the script mentions its action name at all — the
	// delegation reads it in more than one way, and what matters is that SOMETHING
	// responds, not the precise syntax.
	handled := func(a string) bool { return strings.Contains(js, `"`+a+`"`) }
	var dead []string
	seen := map[string]bool{}
	for _, m := range reDataAct.FindAllStringSubmatch(tpl, -1) {
		a := m[1]
		if handled(a) || seen[a] {
			continue
		}
		seen[a] = true
		dead = append(dead, a)
	}
	sort.Strings(dead)
	if len(dead) > 0 {
		t.Fatalf("the wizard renders controls whose data-action nothing handles — "+
			"they look enabled and do nothing when clicked: %s", strings.Join(dead, ", "))
	}
}

// TestAppJS_DestinationEditorIsWired pins the specific machinery the destinations
// step needs, so a future bulk edit cannot quietly remove it again.
func TestAppJS_DestinationEditorIsWired(t *testing.T) {
	src := readAppJS(t)
	for _, fn := range []string{
		"openDest", "closeDest", "saveDest", "draftDest", "renderDests", "destJSON", "newDest",
		"buildFields", "addFieldRow", "readFields", "addAllFields", "moveFieldRow",
		"dmKind", "dmFormat", "dmDatasource", "dmPassthrough",
	} {
		if !strings.Contains(src, "function "+fn+"(") {
			t.Errorf("app.js is missing %s() — the destination editor is broken", fn)
		}
	}
}

func readAppJS(t *testing.T) string { return readFile(t, "static/app.js") }

// stripCommentsAndStrings blanks out comments and string/regex literals so that
// prose ("the editor (uncommitted)") is not mistaken for a call.
func stripCommentsAndStrings(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	for i := 0; i < len(src); {
		switch {
		case strings.HasPrefix(src[i:], "//"):
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case strings.HasPrefix(src[i:], "/*"):
			j := strings.Index(src[i+2:], "*/")
			if j < 0 {
				i = len(src)
			} else {
				i += j + 4
			}
		case src[i] == '"' || src[i] == '\'' || src[i] == '`':
			q := src[i]
			i++
			for i < len(src) && src[i] != q {
				if src[i] == '\\' {
					i++
				}
				i++
			}
			i++
		default:
			b.WriteByte(src[i])
			i++
		}
	}
	return b.String()
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
