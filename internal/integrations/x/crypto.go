package xintegration

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

const encryptionVersion byte = 1

var encryptionContext = []byte("infoai/x-token/v1")

type AESGCMEncryptor struct {
	aead cipher.AEAD
}

func NewAESGCMEncryptor(key []byte) (*AESGCMEncryptor, error) {
	if len(key) != 32 {
		return nil, errors.New("AES-GCM key must contain 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	return &AESGCMEncryptor{aead: aead}, nil
}

func (e *AESGCMEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, e.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate encryption nonce: %w", err)
	}
	result := make([]byte, 1, 1+len(nonce)+len(plaintext)+e.aead.Overhead())
	result[0] = encryptionVersion
	result = append(result, nonce...)
	result = e.aead.Seal(result, nonce, plaintext, encryptionContext)
	return result, nil
}

func (e *AESGCMEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	minimum := 1 + e.aead.NonceSize() + e.aead.Overhead()
	if len(ciphertext) < minimum || ciphertext[0] != encryptionVersion {
		return nil, errors.New("invalid encrypted value")
	}
	nonce := ciphertext[1 : 1+e.aead.NonceSize()]
	plaintext, err := e.aead.Open(nil, nonce, ciphertext[1+e.aead.NonceSize():], encryptionContext)
	if err != nil {
		return nil, fmt.Errorf("decrypt value: %w", err)
	}
	return plaintext, nil
}
