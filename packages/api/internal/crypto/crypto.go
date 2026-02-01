// Package crypto provides symmetric encryption utilities for sensitive data.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Encryptor provides AES-256-GCM encryption and decryption.
type Encryptor struct {
	gcm cipher.AEAD
}

// NewEncryptor creates a new Encryptor with the given base64-encoded key.
// The key must be 32 bytes (256 bits) when decoded.
func NewEncryptor(keyBase64 string) (*Encryptor, error) {
	key, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return nil, fmt.Errorf("decode encryption key: %w", err)
	}

	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	return &Encryptor{gcm: gcm}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM.
// Returns ciphertext with prepended nonce.
func (e *Encryptor) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	// Seal appends ciphertext to nonce
	ciphertext := e.gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts ciphertext that was encrypted with Encrypt.
// Expects nonce prepended to ciphertext.
func (e *Encryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	nonceSize := e.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := e.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plaintext, nil
}

// GeneratePassword generates a cryptographically secure random password.
// Returns 32 random bytes as a base64 URL-safe encoded string.
func GeneratePassword() (string, error) {
	bytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, bytes); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// GenerateKey generates a new 256-bit encryption key.
// Returns the key as a base64 standard encoded string.
func GenerateKey() (string, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", fmt.Errorf("generate key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}
