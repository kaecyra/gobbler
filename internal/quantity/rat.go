// Package quantity is the pure arithmetic core for recipe amounts (ADR-00005):
// exact rationals, ranges, units with dimensions, conversion, scaling and
// kitchen rounding. It performs no I/O and imports no other internal package.
// Floats appear only in display helpers, never in storage or arithmetic.
package quantity

import (
	"errors"
	"math/big"
	"strings"
)

// ErrDivideByZero is returned when a rational would have a zero denominator or
// be divided by zero.
var ErrDivideByZero = errors.New("division by zero")

// Rat is an immutable exact rational number. The zero value is 0. Every
// operation returns a new Rat, so values can be shared freely.
type Rat struct {
	v *big.Rat
}

// NewRat returns n/d, normalised. It returns ErrDivideByZero when d is zero.
func NewRat(n, d int64) (Rat, error) {
	if d == 0 {
		return Rat{}, ErrDivideByZero
	}
	return Rat{v: big.NewRat(n, d)}, nil
}

// Int returns the whole number n.
func Int(n int64) Rat {
	return Rat{v: big.NewRat(n, 1)}
}

// rat returns the underlying value for reading; callers must not mutate it.
func (r Rat) rat() *big.Rat {
	if r.v == nil {
		return new(big.Rat)
	}
	return r.v
}

// Add returns r + o.
func (r Rat) Add(o Rat) Rat { return Rat{v: new(big.Rat).Add(r.rat(), o.rat())} }

// Sub returns r - o.
func (r Rat) Sub(o Rat) Rat { return Rat{v: new(big.Rat).Sub(r.rat(), o.rat())} }

// Mul returns r * o.
func (r Rat) Mul(o Rat) Rat { return Rat{v: new(big.Rat).Mul(r.rat(), o.rat())} }

// Div returns r / o, or ErrDivideByZero when o is zero.
func (r Rat) Div(o Rat) (Rat, error) {
	if o.Sign() == 0 {
		return Rat{}, ErrDivideByZero
	}
	return Rat{v: new(big.Rat).Quo(r.rat(), o.rat())}, nil
}

// Cmp compares r with o and returns -1, 0 or +1.
func (r Rat) Cmp(o Rat) int { return r.rat().Cmp(o.rat()) }

// Equal reports whether r and o are the same number.
func (r Rat) Equal(o Rat) bool { return r.Cmp(o) == 0 }

// Sign returns -1, 0 or +1 for negative, zero and positive values.
func (r Rat) Sign() int { return r.rat().Sign() }

// IsInt reports whether r is a whole number.
func (r Rat) IsInt() bool { return r.rat().IsInt() }

// Num returns a copy of the normalised numerator.
func (r Rat) Num() *big.Int { return new(big.Int).Set(r.rat().Num()) }

// Denom returns a copy of the normalised, positive denominator.
func (r Rat) Denom() *big.Int { return new(big.Int).Set(r.rat().Denom()) }

// Floor returns the greatest whole number not above r.
func (r Rat) Floor() Rat {
	q := new(big.Int).Div(r.rat().Num(), r.rat().Denom()) // Euclidean: floor for positive denominators
	return Rat{v: new(big.Rat).SetInt(q)}
}

// Float64 returns the nearest float. For display only; never feed it back
// into arithmetic.
func (r Rat) Float64() float64 {
	f, _ := r.rat().Float64()
	return f
}

// String renders r as a whole number, a proper fraction or a mixed number:
// "2", "1/3", "1 1/2". Negative values carry a leading minus sign.
func (r Rat) String() string {
	v := r.rat()
	if v.IsInt() {
		return v.Num().String()
	}
	neg := v.Sign() < 0
	abs := new(big.Rat).Abs(v)
	whole := new(big.Int).Quo(abs.Num(), abs.Denom())
	rem := new(big.Int).Rem(abs.Num(), abs.Denom())
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	if whole.Sign() != 0 {
		b.WriteString(whole.String())
		b.WriteByte(' ')
	}
	b.WriteString(rem.String())
	b.WriteByte('/')
	b.WriteString(abs.Denom().String())
	return b.String()
}

// Decimal renders r as a decimal with at most places fractional digits and no
// trailing zeros: "2.5", "0.33", "4". For display only.
func (r Rat) Decimal(places int) string {
	s := r.rat().FloatString(places)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	if s == "-0" {
		return "0"
	}
	return s
}
