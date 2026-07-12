package httpserver

import "testing"

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

func contains(h, n string) bool { return len(h) >= len(n) && (func() bool {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return true
		}
	}
	return false
})() }
