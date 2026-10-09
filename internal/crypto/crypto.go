// Package crypto holds the two primitives envgrid relies on: AES-256-GCM for
// values at rest and argon2id for passwords.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// KeySize is the master key length in bytes (AES-256).
const KeySize = 32

// ErrDecrypt is returned for any ciphertext that fails authentication. It
// deliberately carries no detail about the input.
var ErrDecrypt = errors.New("decryption failed")

// ParseKey decodes a base64 master key and checks its length.
func ParseKey(b64 string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, errors.New("master key is not valid base64")
	}
	if len(key) != KeySize {
		return nil, fmt.Errorf("master key must decode to %d bytes, got %d", KeySize, len(key))
	}
	return key, nil
}

// GenerateKey returns a new random master key, base64 encoded.
func GenerateKey() string {
	return base64.StdEncoding.EncodeToString(randomBytes(KeySize))
}

// Token returns 32 random bytes, base64url encoded, for session cookies.
func Token() string {
	return base64.RawURLEncoding.EncodeToString(randomBytes(32))
}

// Cipher seals and opens values with AES-256-GCM.
// The stored format is nonce (12 bytes) || sealed ciphertext and tag.
type Cipher struct{ aead cipher.AEAD }

// NewCipher builds a Cipher from a 32-byte key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("key must be %d bytes", KeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Seal encrypts plaintext with a fresh random nonce. aad is authenticated but
// not stored; callers use it to bind a ciphertext to its key and environment
// so a row moved elsewhere in the database no longer decrypts.
func (c *Cipher) Seal(plaintext, aad []byte) []byte {
	nonce := randomBytes(c.aead.NonceSize())
	return c.aead.Seal(nonce, nonce, plaintext, aad)
}

// Open authenticates and decrypts a value produced by Seal with the same aad.
func (c *Cipher) Open(ciphertext, aad []byte) ([]byte, error) {
	n := c.aead.NonceSize()
	if len(ciphertext) < n+c.aead.Overhead() {
		return nil, ErrDecrypt
	}
	plaintext, err := c.aead.Open(nil, ciphertext[:n], ciphertext[n:], aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}

// Argon2Params are the argon2id cost settings.
type Argon2Params struct {
	Time    uint32
	Memory  uint32 // KiB
	Threads uint8
}

// PasswordParams is used for new hashes. Verification reads the parameters
// stored in each hash, so tests may lower this without breaking old hashes.
var PasswordParams = Argon2Params{Time: 3, Memory: 64 * 1024, Threads: 2}

// HashPassword returns a PHC-formatted argon2id hash.
func HashPassword(password string) string {
	p := PasswordParams
	salt := randomBytes(16)
	hash := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, 32)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Time, p.Threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash))
}

// VerifyPassword checks password against a hash from HashPassword using a
// constant-time comparison.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var p Argon2Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	// Since Go 1.24 Read crashes rather than returning an error; the check
	// keeps that guarantee explicit instead of relying on the toolchain.
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return b
}
