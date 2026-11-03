package apikey

import (
	"fmt"
	"time"
)

// Lifetime is one of the expiry choices offered when a key is made
// (ADR-00031). Value is what the form posts.
type Lifetime struct {
	Value string
	Label string
	// Days is how long the key lasts; zero means it never expires.
	Days int
}

// Lifetimes are the choices, in the order a form lists them. DefaultLifetime
// is the one selected first.
var Lifetimes = []Lifetime{
	{"30d", "30 days", 30},
	{"90d", "90 days", 90},
	{"1y", "1 year", 365},
	{"never", "No expiry", 0},
}

// DefaultLifetime is the Value a form selects first: a key that expires is the
// safer default, and no expiry is a choice made on purpose.
const DefaultLifetime = "90d"

// LifetimeFor returns the choice a form posted, or ErrInvalid.
func LifetimeFor(value string) (Lifetime, error) {
	for _, l := range Lifetimes {
		if l.Value == value {
			return l, nil
		}
	}
	return Lifetime{}, fmt.Errorf("%w: choose how long the key lasts", ErrInvalid)
}

// Duration is how long a key made with this choice lasts; zero is no expiry.
func (l Lifetime) Duration() time.Duration { return time.Duration(l.Days) * 24 * time.Hour }
