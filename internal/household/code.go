package household

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// An invitation's code is its only identifier outside the database
// (ADR-00028): CodeLength characters from codeAlphabet, drawn uniformly from
// a cryptographically secure source.
const (
	CodeLength   = 8
	codeAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	// codeByteLimit is the largest multiple of len(codeAlphabet) a byte can
	// hold. A byte at or above it is discarded, so byte % 62 favours no
	// character.
	codeByteLimit = 256 - 256%len(codeAlphabet)
	// codeDraws bounds how many fresh codes Invite tries when each collides
	// with an existing one. At 47 bits a single collision is already
	// improbable; running out means the random source is broken.
	codeDraws = 5
)

// errCodeTaken marks an insert that collided with an existing code.
var errCodeTaken = errors.New("invitation code already in use")

// ValidCode reports whether s has the shape of an invitation code. It says
// nothing about whether an invitation has that code.
func ValidCode(s string) bool {
	if len(s) != CodeLength {
		return false
	}
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(codeAlphabet, s[i]) < 0 {
			return false
		}
	}
	return true
}

// newCode draws a code from src by rejection sampling.
func newCode(src io.Reader) (string, error) {
	out := make([]byte, 0, CodeLength)
	buf := make([]byte, CodeLength)
	for len(out) < CodeLength {
		if _, err := io.ReadFull(src, buf); err != nil {
			return "", fmt.Errorf("draw invitation code: %w", err)
		}
		for _, b := range buf {
			if int(b) < codeByteLimit && len(out) < CodeLength {
				out = append(out, codeAlphabet[int(b)%len(codeAlphabet)])
			}
		}
	}
	return string(out), nil
}
