package editor

import "testing"

func TestStageSelectOffersServe(t *testing.T) {
	for _, k := range stageOptions() {
		if k == "serve" {
			return
		}
	}
	t.Errorf("stageOptions() = %v, want it to include serve", stageOptions())
}
