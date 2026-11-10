package settings

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// KeyBytes is the size of the environment's encryption key.
const KeyBytes = 32

// hkdfInfo binds the derived key to this use of the environment key.
const hkdfInfo = "gobbler integration settings v1"

// ParseKey decodes the environment's encryption key: 32 random bytes in
// standard base64. The error never contains the value.
func ParseKey(s string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, errors.New("the encryption key is not valid base64")
	}
	if len(b) != KeyBytes {
		return nil, fmt.Errorf("the encryption key must be %d bytes, got %d", KeyBytes, len(b))
	}
	return b, nil
}

// newAEAD derives the AES-256-GCM key from the environment key.
func newAEAD(envKey []byte) (cipher.AEAD, error) {
	k, err := hkdf.Key(sha256.New, envKey, nil, hkdfInfo, KeyBytes)
	if err != nil {
		return nil, fmt.Errorf("derive settings key: %w", err)
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, fmt.Errorf("settings cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

// aad is the associated data of a sealed value.
func aad(f Field) []byte { return []byte(f.Integration + "\x00" + f.Name) }
