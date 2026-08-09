package xintegration

import (
	"bytes"
	"testing"
)

func TestAESGCMEncryptorRoundTripsWithoutDeterministicCiphertext(t *testing.T) {
	encryptor, err := NewAESGCMEncryptor(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatalf("NewAESGCMEncryptor() error = %v", err)
	}

	first, err := encryptor.Encrypt([]byte("secret-token"))
	if err != nil {
		t.Fatalf("first Encrypt() error = %v", err)
	}
	second, err := encryptor.Encrypt([]byte("secret-token"))
	if err != nil {
		t.Fatalf("second Encrypt() error = %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("ciphertexts must use distinct nonces")
	}

	plaintext, err := encryptor.Decrypt(first)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if string(plaintext) != "secret-token" {
		t.Fatalf("Decrypt() = %q, want secret-token", plaintext)
	}
}

func TestAESGCMEncryptorRejectsInvalidKeysAndTampering(t *testing.T) {
	if _, err := NewAESGCMEncryptor(make([]byte, 16)); err == nil {
		t.Fatal("NewAESGCMEncryptor() error = nil, want invalid key error")
	}

	encryptor, err := NewAESGCMEncryptor(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatalf("NewAESGCMEncryptor() error = %v", err)
	}
	ciphertext, err := encryptor.Encrypt([]byte("secret-token"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	ciphertext[len(ciphertext)-1] ^= 1
	if _, err := encryptor.Decrypt(ciphertext); err == nil {
		t.Fatal("Decrypt() tampered error = nil, want authentication error")
	}
}
