package auth

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// clockLeeway tolerates small clock differences between Cloudflare and this
// host when checking exp and nbf.
const clockLeeway = 30 * time.Second

// errToken marks a token that failed verification. Its messages are fixed
// strings: nothing derived from the token is ever put in an error, so no error
// path can leak it (ADR-00003 obligation 4).
var errToken = errors.New("invalid access token")

func reject(reason string) error { return fmt.Errorf("%w: %s", errToken, reason) }

type header struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

type claims struct {
	Iss   string   `json:"iss"`
	Aud   audience `json:"aud"`
	Email string   `json:"email"`
	Exp   *float64 `json:"exp"`
	Nbf   *float64 `json:"nbf"`
}

// audience decodes the JWT aud claim, which is a string or an array of them.
type audience []string

func (a *audience) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*a = audience{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return errors.New("aud is neither a string nor a list of strings")
	}
	*a = many
	return nil
}

func (a audience) contains(want string) bool {
	for _, v := range a {
		if v == want {
			return true
		}
	}
	return false
}

// parsed is a token whose structure has been decoded but not yet verified.
type parsed struct {
	header  header
	claims  claims
	signing string // "<header>.<payload>", the signed bytes
	sig     []byte
}

// parseToken splits and decodes a compact JWS. It checks structure only.
func parseToken(token string) (parsed, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return parsed{}, reject("not a three-part token")
	}
	var p parsed
	if err := decodePart(parts[0], &p.header); err != nil {
		return parsed{}, reject("unreadable header")
	}
	if err := decodePart(parts[1], &p.claims); err != nil {
		return parsed{}, reject("unreadable claims")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return parsed{}, reject("unreadable signature")
	}
	p.sig = sig
	p.signing = parts[0] + "." + parts[1]
	return p, nil
}

func decodePart(part string, v any) error {
	raw, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

// verifySignature checks an RS256 signature, the only algorithm Cloudflare
// Access signs with. The algorithm is fixed here, never taken from the token,
// so "none" and HMAC downgrades cannot be selected by the sender.
func (p parsed) verifySignature(key *rsa.PublicKey) error {
	sum := sha256.Sum256([]byte(p.signing))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], p.sig); err != nil {
		return reject("bad signature")
	}
	return nil
}

// checkClaims enforces iss, aud, exp and nbf and returns the email.
func (c claims) check(issuer, aud string, now time.Time) (string, error) {
	if c.Iss != issuer {
		return "", reject("wrong issuer")
	}
	if !c.Aud.contains(aud) {
		return "", reject("wrong audience")
	}
	if c.Exp == nil {
		return "", reject("no expiry")
	}
	if now.After(unix(*c.Exp).Add(clockLeeway)) {
		return "", reject("expired")
	}
	if c.Nbf != nil && now.Add(clockLeeway).Before(unix(*c.Nbf)) {
		return "", reject("not yet valid")
	}
	if c.Email == "" {
		return "", reject("no email")
	}
	return c.Email, nil
}

func unix(sec float64) time.Time { return time.Unix(int64(sec), 0) }
