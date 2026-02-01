package crypto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateKey(t *testing.T) {
	key1, err := GenerateKey()
	require.NoError(t, err)
	assert.NotEmpty(t, key1)

	// Key should be unique
	key2, err := GenerateKey()
	require.NoError(t, err)
	assert.NotEqual(t, key1, key2)
}

func TestGeneratePassword(t *testing.T) {
	pwd1, err := GeneratePassword()
	require.NoError(t, err)
	assert.NotEmpty(t, pwd1)
	assert.Len(t, pwd1, 43) // 32 bytes base64url encoded = 43 chars

	// Password should be unique
	pwd2, err := GeneratePassword()
	require.NoError(t, err)
	assert.NotEqual(t, pwd1, pwd2)
}

func TestEncryptorRoundtrip(t *testing.T) {
	// Generate a key
	key, err := GenerateKey()
	require.NoError(t, err)

	// Create encryptor
	enc, err := NewEncryptor(key)
	require.NoError(t, err)

	// Test data
	plaintext := []byte("test-password-12345")

	// Encrypt
	ciphertext, err := enc.Encrypt(plaintext)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ciphertext)

	// Decrypt
	decrypted, err := enc.Decrypt(ciphertext)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestEncryptorDifferentCiphertext(t *testing.T) {
	key, err := GenerateKey()
	require.NoError(t, err)

	enc, err := NewEncryptor(key)
	require.NoError(t, err)

	plaintext := []byte("same-data")

	// Encrypt twice - should produce different ciphertext due to random nonce
	ct1, err := enc.Encrypt(plaintext)
	require.NoError(t, err)

	ct2, err := enc.Encrypt(plaintext)
	require.NoError(t, err)

	assert.NotEqual(t, ct1, ct2, "ciphertext should be different due to random nonce")

	// Both should decrypt to same plaintext
	pt1, err := enc.Decrypt(ct1)
	require.NoError(t, err)
	pt2, err := enc.Decrypt(ct2)
	require.NoError(t, err)

	assert.Equal(t, plaintext, pt1)
	assert.Equal(t, plaintext, pt2)
}

func TestEncryptorInvalidKey(t *testing.T) {
	// Invalid base64
	_, err := NewEncryptor("not-valid-base64!")
	assert.Error(t, err)

	// Wrong key length
	_, err = NewEncryptor("dGVzdA==") // "test" = 4 bytes
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be 32 bytes")
}

func TestDecryptInvalidData(t *testing.T) {
	key, err := GenerateKey()
	require.NoError(t, err)

	enc, err := NewEncryptor(key)
	require.NoError(t, err)

	// Too short (less than nonce size)
	_, err = enc.Decrypt([]byte("short"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too short")

	// Corrupted ciphertext
	validCt, err := enc.Encrypt([]byte("test"))
	require.NoError(t, err)
	validCt[len(validCt)-1] ^= 0xff // Corrupt last byte
	_, err = enc.Decrypt(validCt)
	assert.Error(t, err)
}
