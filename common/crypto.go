package common

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const cryptoSecretCipherV1Prefix = "v1:"

func GenerateHMACWithKey(key []byte, data string) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func GenerateHMAC(data string) string {
	h := hmac.New(sha256.New, []byte(CryptoSecret))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func Password2Hash(password string) (string, error) {
	passwordBytes := []byte(password)
	hashedPassword, err := bcrypt.GenerateFromPassword(passwordBytes, bcrypt.DefaultCost)
	return string(hashedPassword), err
}

func ValidatePasswordAndHash(password string, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func cryptoSecretAESGCM() (cipher.AEAD, error) {
	key := sha256.Sum256([]byte(CryptoSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// EncryptWithCryptoSecret encrypts plaintext with AES-GCM using a key derived
// from common.CryptoSecret. The returned value is versioned so future cipher
// formats can coexist. Production deployments must keep CRYPTO_SECRET stable
// across restarts if they need to decrypt persisted values later.
func EncryptWithCryptoSecret(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	aead, err := cryptoSecretAESGCM()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return cryptoSecretCipherV1Prefix + base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// DecryptWithCryptoSecret decrypts values produced by EncryptWithCryptoSecret.
func DecryptWithCryptoSecret(encrypted string) (string, error) {
	encrypted = strings.TrimSpace(encrypted)
	if encrypted == "" {
		return "", nil
	}
	if !strings.HasPrefix(encrypted, cryptoSecretCipherV1Prefix) {
		return "", fmt.Errorf("unsupported encrypted value format")
	}
	payload := strings.TrimPrefix(encrypted, cryptoSecretCipherV1Prefix)
	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", err
	}
	aead, err := cryptoSecretAESGCM()
	if err != nil {
		return "", err
	}
	if len(data) < aead.NonceSize() {
		return "", fmt.Errorf("encrypted value is too short")
	}
	nonce := data[:aead.NonceSize()]
	ciphertext := data[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
