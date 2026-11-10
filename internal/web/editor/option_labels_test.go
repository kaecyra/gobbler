package editor

import (
	"strings"
	"testing"
)

// #611: the scaling and qualifier kind selects show labels, not stored values.
func TestTheEditorLabelsScalingRulesAndQualifierKinds(t *testing.T) {
	body := newEnv(t).get().body()
	for _, want := range []string{
		`<option value="linear" selected>Scales</option>`, `<option value="fixed">Fixed</option>`, `<option value="to_taste">To taste</option>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("editor page lacks %s", want)
		}
	}
	for _, raw := range []string{`>linear<`, `>to_taste<`, `>preparation<`, `>note<`} {
		if strings.Contains(body, raw) {
			t.Errorf("editor page shows the stored value %s", raw)
		}
	}
	if !strings.Contains(body, `>Preparation</option>`) {
		t.Error("qualifier kind select lacks the Preparation label")
	}
}
