package llmusage

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrInvalid is what errors.Is finds in an error for input the store refuses: a
// price that is not a number, or a provider that is not one gobbler offers. The
// error's own message is fit to show the operator as it stands.
var ErrInvalid = errors.New("invalid price")

// invalid is an ErrInvalid whose message is its text alone.
type invalid string

func (e invalid) Error() string      { return string(e) }
func (invalid) Is(target error) bool { return target == ErrInvalid }

func refuse(format string, a ...any) error { return invalid(fmt.Sprintf(format, a...)) }

// The sources of a price.
const (
	// SourceOpenRouter is a price the daily sync set. The sync may replace it.
	SourceOpenRouter = "openrouter"
	// SourceManual is a price the operator set. The sync never changes it.
	SourceManual = "manual"
)

// Per-million-token prices are held in millionths of a US dollar, so $3.00 per
// million tokens is 3_000_000. A call's cost is in millionths of a US dollar.
const (
	microsPerUSD = 1_000_000
	tokensPerM   = 1_000_000
)

// Price is what one model of one provider costs, per million tokens, in
// millionths of a US dollar.
type Price struct {
	Provider, Model string
	InputMicros     int64
	OutputMicros    int64
	// Source is SourceOpenRouter or SourceManual.
	Source string
	// SyncedAt is when the sync last set the price; zero for a manual price.
	SyncedAt  time.Time
	UpdatedAt time.Time
}

// Cost is what a call of in input and out output tokens costs at p, in
// millionths of a US dollar, rounded to the nearest.
func (p Price) Cost(in, out int64) int64 {
	return (in*p.InputMicros + out*p.OutputMicros + tokensPerM/2) / tokensPerM
}

// maxPriceMicros bounds a price (per million tokens) well above any real one,
// $1000 per million, so a typo cannot overflow a cost.
const maxPriceMicros = 1_000 * microsPerUSD

var decimal = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

// parseScaled reads a plain non-negative decimal such as "3.5" and returns it
// times scale, rounded to the nearest whole number. It reads no exponent, sign
// or fraction, so the text means what it says.
func parseScaled(s string, scale int64) (int64, error) {
	s = strings.TrimSpace(s)
	if !decimal.MatchString(s) {
		return 0, fmt.Errorf("%q is not a plain decimal number", s)
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	r.Mul(r, new(big.Rat).SetInt64(scale))
	r.Add(r, big.NewRat(1, 2))
	n := new(big.Int).Quo(r.Num(), r.Denom())
	if !n.IsInt64() || n.Int64() > maxPriceMicros {
		return 0, fmt.Errorf("%q is out of range", s)
	}
	return n.Int64(), nil
}

// ParseUSDPerMillion reads a price typed as US dollars per million tokens,
// such as "3.00", into millionths of a dollar per million tokens.
func ParseUSDPerMillion(s string) (int64, error) {
	n, err := parseScaled(s, microsPerUSD)
	if err != nil {
		return 0, refuse("%s", err.Error())
	}
	return n, nil
}

// parseUSDPerToken reads OpenRouter's price: US dollars per token as a decimal
// string such as "0.000003".
func parseUSDPerToken(s string) (int64, error) {
	return parseScaled(s, microsPerUSD*tokensPerM)
}

// FormatPrice shows a per-million-token price in dollars, such as "$3.00".
func FormatPrice(micros int64) string { return formatUSD(micros) }

// FormatCost shows a cost in dollars, such as "$0.0123".
func FormatCost(micros int64) string { return formatUSD(micros) }

// formatUSD shows micros as dollars with at least two decimals and no trailing
// zeros beyond them.
func formatUSD(micros int64) string {
	whole, frac := micros/microsPerUSD, micros%microsPerUSD
	f := strings.TrimRight(fmt.Sprintf("%06d", frac), "0")
	for len(f) < 2 {
		f += "0"
	}
	return "$" + strconv.FormatInt(whole, 10) + "." + f
}
