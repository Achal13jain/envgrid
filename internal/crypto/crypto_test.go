package crypto

import (
	"bytes"
	"testing"
)

func testCipher(t *testing.T) *Cipher {
	t.Helper()
	key, err := ParseKey(GenerateKey())
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSealOpenRoundTrip(t *testing.T) {
	c := testCipher(t)
	aad := []byte("key=1:env=2")
	for _, pt := range []string{"", "hello", "multi\nline\nvalue", string(bytes.Repeat([]byte{0xff, 0x00}, 5000))} {
		ct := c.Seal([]byte(pt), aad)
		if bytes.Contains(ct, []byte(pt)) && pt != "" {
			t.Fatalf("ciphertext contains plaintext")
		}
		got, err := c.Open(ct, aad)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		if string(got) != pt {
			t.Fatalf("round trip mismatch")
		}
	}
}

func TestSealUsesFreshNonce(t *testing.T) {
	c := testCipher(t)
	a, b := c.Seal([]byte("same"), nil), c.Seal([]byte("same"), nil)
	if bytes.Equal(a, b) {
		t.Fatal("two seals of the same plaintext produced identical ciphertext")
	}
	if len(a) != 12+4+16 {
		t.Fatalf("unexpected ciphertext length %d (want nonce||sealed)", len(a))
	}
}

func TestTamperDetection(t *testing.T) {
	c := testCipher(t)
	aad := []byte("key=1:env=2")
	ct := c.Seal([]byte("secret value"), aad)

	for i := range ct {
		bad := bytes.Clone(ct)
		bad[i] ^= 0x01
		if _, err := c.Open(bad, aad); err != ErrDecrypt {
			t.Fatalf("flipping byte %d was not detected: %v", i, err)
		}
	}
	if _, err := c.Open(ct, []byte("key=1:env=3")); err != ErrDecrypt {
		t.Fatal("ciphertext opened under a different aad")
	}
	if _, err := c.Open(ct[:10], aad); err != ErrDecrypt {
		t.Fatal("truncated ciphertext was accepted")
	}
	other := testCipher(t)
	if _, err := other.Open(ct, aad); err != ErrDecrypt {
		t.Fatal("ciphertext opened under a different key")
	}
}

func TestParseKey(t *testing.T) {
	if _, err := ParseKey("not base64!"); err == nil {
		t.Fatal("accepted invalid base64")
	}
	if _, err := ParseKey("c2hvcnQ="); err == nil {
		t.Fatal("accepted short key")
	}
	if k, err := ParseKey(" " + GenerateKey() + "\n"); err != nil || len(k) != KeySize {
		t.Fatalf("rejected valid key: %v", err)
	}
}

func TestPasswordHash(t *testing.T) {
	old := PasswordParams
	PasswordParams = Argon2Params{Time: 1, Memory: 1024, Threads: 1}
	defer func() { PasswordParams = old }()

	h := HashPassword("correct horse")
	if !VerifyPassword("correct horse", h) {
		t.Fatal("valid password rejected")
	}
	if VerifyPassword("correct horsf", h) {
		t.Fatal("wrong password accepted")
	}
	if HashPassword("correct horse") == h {
		t.Fatal("hash is not salted")
	}
	for _, bad := range []string{"", "$argon2i$v=19$m=1024,t=1,p=1$AAAA$AAAA", "$argon2id$v=19$m=x$AAAA$AAAA", h + "$"} {
		if VerifyPassword("correct horse", bad) {
			t.Fatalf("malformed hash accepted: %q", bad)
		}
	}
}

func TestDefaultPasswordParams(t *testing.T) {
	// The spec fixes these; guard against accidental edits.
	if PasswordParams != (Argon2Params{Time: 3, Memory: 64 * 1024, Threads: 2}) {
		t.Fatalf("argon2id params changed: %+v", PasswordParams)
	}
}
