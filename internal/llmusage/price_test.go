package llmusage_test

import (
	"errors"
	"testing"

	"github.com/kaecyra/gobbler/internal/llmusage"
)

func TestParseUSDPerMillion(t *testing.T) {
	for in, want := range map[string]int64{
		"3": 3_000_000, "3.00": 3_000_000, " 0.075 ": 75_000, "0": 0, "15.5": 15_500_000, "0.0000004": 0, "0.0000005": 1,
	} {
		got, err := llmusage.ParseUSDPerMillion(in)
		if err != nil || got != want {
			t.Errorf("ParseUSDPerMillion(%q) = %d, %v, want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "abc", "-1", "1e3", "1/2", "+3", "3,5", ".5", "5.", "1001", "99999999999999999999"} {
		if _, err := llmusage.ParseUSDPerMillion(in); !errors.Is(err, llmusage.ErrInvalid) {
			t.Errorf("ParseUSDPerMillion(%q) error = %v, want ErrInvalid", in, err)
		}
	}
}

func TestFormatUSD(t *testing.T) {
	for micros, want := range map[int64]string{0: "$0.00", 3_000_000: "$3.00", 75_000: "$0.075", 33_000: "$0.033", 1: "$0.000001", 12_345_600: "$12.3456", 500_000: "$0.50"} {
		if got := llmusage.FormatCost(micros); got != want {
			t.Errorf("FormatCost(%d) = %q, want %q", micros, got, want)
		}
		if got := llmusage.FormatPrice(micros); got != want {
			t.Errorf("FormatPrice(%d) = %q, want %q", micros, got, want)
		}
	}
}

func TestCostRoundsToTheNearestMicro(t *testing.T) {
	p := llmusage.Price{InputMicros: 3_000_000, OutputMicros: 15_000_000}
	for _, c := range []struct{ in, out, want int64 }{
		{0, 0, 0}, {1000, 2000, 33_000}, {1, 0, 3}, {0, 1, 15}, {100_000, 0, 300_000},
	} {
		if got := p.Cost(c.in, c.out); got != c.want {
			t.Errorf("Cost(%d, %d) = %d, want %d", c.in, c.out, got, c.want)
		}
	}
	if got := (llmusage.Price{InputMicros: 100_000}).Cost(5, 0); got != 1 { // 0.5 rounds up
		t.Errorf("Cost rounding = %d, want 1", got)
	}
}
