package editor

import (
	"slices"
	"testing"
)

func TestStageSelectOffersServeAndMix(t *testing.T) {
	for _, want := range []string{"serve", "mix"} {
		if !slices.Contains(stageOptions(), want) {
			t.Errorf("stageOptions() = %v, want it to include %s", stageOptions(), want)
		}
	}
}
