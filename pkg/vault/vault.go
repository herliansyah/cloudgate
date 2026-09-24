package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	saltLen   = 16
	nonceLen  = 12
	keyLen    = 32 // 256 bits for AES-256
	iterCount = 100_000
)

var (
	ErrDecryptionFailed = errors.New("decryption failed: incorrect password or corrupted vault")
	ErrInvalidData      = errors.New("invalid vault payload: too short")
)

// Encrypt encrypts plaintext bytes with a user-supplied password using PBKDF2 + AES-256-GCM.
func Encrypt(plaintext []byte, password string) ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("failed to generate random salt: %w", err)
	}

	key, err := pbkdf2.Key(sha256.New, password, salt, iterCount, keyLen)
	if err != nil {
		return nil, fmt.Errorf("failed to derive key: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create gcm: %w", err)
	}

	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Payload: Salt (16) + Nonce (12) + Sealed Ciphertext
	sealed := gcm.Seal(nil, nonce, plaintext, nil)
	result := make([]byte, 0, saltLen+nonceLen+len(sealed))
	result = append(result, salt...)
	result = append(result, nonce...)
	result = append(result, sealed...)

	return result, nil
}

// Decrypt decrypts encrypted vault bytes using the provided password.
func Decrypt(data []byte, password string) ([]byte, error) {
	if len(data) < saltLen+nonceLen+16 {
		return nil, ErrInvalidData
	}

	salt := data[:saltLen]
	nonce := data[saltLen : saltLen+nonceLen]
	ciphertext := data[saltLen+nonceLen:]

	key, err := pbkdf2.Key(sha256.New, password, salt, iterCount, keyLen)
	if err != nil {
		return nil, fmt.Errorf("failed to derive key: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create gcm: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// SaveToFile encrypts and saves data to a destination file.
func SaveToFile(filePath string, plaintext []byte, password string) error {
	encrypted, err := Encrypt(plaintext, password)
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, encrypted, 0600)
}

// ReadFromFile reads and decrypts data from a vault file.
func ReadFromFile(filePath string, password string) ([]byte, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	return Decrypt(data, password)
}
