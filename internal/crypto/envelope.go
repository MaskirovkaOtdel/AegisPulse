package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

var (
	ErrInvalidKeySize     = errors.New("master key must be 32 bytes for AES-256")
	ErrCiphertextTooShort = errors.New("ciphertext is too short to contain nonce")
	ErrDecryptionFailed   = errors.New("decryption failed or message corrupted")
)

type EnvelopeCipher struct {
	mu        sync.RWMutex
	masterKey []byte
}

func NewEnvelopeCipher(rawKey string) (*EnvelopeCipher, error) {
	keyBytes, err := resolveMasterKey(rawKey)
	if err != nil {
		return nil, err
	}
	return &EnvelopeCipher{masterKey: keyBytes}, nil
}

func resolveMasterKey(rawKey string) ([]byte, error) {
	if envKey := os.Getenv("AEGIS_MASTER_KEY"); envKey != "" {
		rawKey = envKey
	}

	if len(rawKey) == 64 {
		// Hex encoded 32-byte key
		decoded, err := hex.DecodeString(rawKey)
		if err == nil && len(decoded) == 32 {
			return decoded, nil
		}
	}

	if len(rawKey) == 32 {
		return []byte(rawKey), nil
	}

	// If key is shorter/longer, derive a deterministic 32-byte key via SHA-256
	h := sha256.Sum256([]byte(rawKey))
	return h[:], nil
}

// Encrypt encrypts plaintext using AES-256-GCM with a randomized 12-byte nonce.
// Returns base64(nonce || ciphertext || tag)
func (ec *EnvelopeCipher) Encrypt(plaintext []byte) (string, error) {
	ec.mu.RLock()
	key := ec.masterKey
	ec.mu.RUnlock()

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes cipher error: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("gcm mode error: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("nonce generation error: %w", err)
	}

	// Seal appends ciphertext and authentication tag to nonce
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts base64(nonce || ciphertext || tag) using AES-256-GCM.
func (ec *EnvelopeCipher) Decrypt(encodedCiphertext string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encodedCiphertext)
	if err != nil {
		return nil, fmt.Errorf("base64 decode error: %w", err)
	}

	ec.mu.RLock()
	key := ec.masterKey
	ec.mu.RUnlock()

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher error: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm mode error: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, ErrCiphertextTooShort
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// HashKey produces a SHA-256 hex digest for indexed database lookup
func HashKey(apiKey string) string {
	h := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(h[:])
}
