package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strings"
)

// The S256 challenge is the unpadded base64url of a SHA-256, 43 characters.
const challengeLen = 43

// ValidChallenge reports whether s has the shape of an S256 code challenge.
func ValidChallenge(s string) bool {
	if len(s) != challengeLen {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	return err == nil && len(b) == sha256.Size
}

// ValidVerifier reports whether s is a code verifier as RFC 7636 section 4.1
// allows: 43 to 128 characters of the unreserved set.
func ValidVerifier(s string) bool {
	if len(s) < 43 || len(s) > 128 {
		return false
	}
	return !strings.ContainsFunc(s, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return false
		}
		return !strings.ContainsRune("-._~", r)
	})
}

// VerifyPKCE reports whether verifier is the one the S256 challenge was made
// from.
func VerifyPKCE(challenge, verifier string) bool {
	if !ValidChallenge(challenge) || !ValidVerifier(verifier) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(want), []byte(challenge)) == 1
}

// Challenge returns the S256 challenge for verifier. Clients make this; the
// server needs it for its tests and for nothing else.
func Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
