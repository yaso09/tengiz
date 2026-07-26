package encrypt

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestGenerateKey(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(key))
	}
}

func TestEncryptDecrypt(t *testing.T) {
	key, _ := GenerateKey()
	plaintext := []byte("my-secret-database-password-123")

	ciphertext, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if bytes.Equal(plaintext, ciphertext) {
		t.Fatal("ciphertext matches plaintext — no encryption")
	}

	decrypted, err := Decrypt(ciphertext, key)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Fatalf("decrypted text mismatch: got %q, want %q", string(decrypted), string(plaintext))
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	key1, _ := GenerateKey()
	key2, _ := GenerateKey()
	plaintext := []byte("sensitive-data")

	ciphertext, _ := Encrypt(plaintext, key1)
	_, err := Decrypt(ciphertext, key2)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestDecryptTamperedCiphertext(t *testing.T) {
	key, _ := GenerateKey()
	plaintext := []byte("data")
	ciphertext, _ := Encrypt(plaintext, key)

	ciphertext[len(ciphertext)-1] ^= 0xFF
	_, err := Decrypt(ciphertext, key)
	if err == nil {
		t.Fatal("expected error on tampered ciphertext")
	}
}

func TestKeyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".key")

	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	if err := SaveKey(path, key); err != nil {
		t.Fatalf("SaveKey: %v", err)
	}

	loaded, err := LoadKey(path)
	if err != nil {
		t.Fatalf("LoadKey: %v", err)
	}

	if !bytes.Equal(key, loaded) {
		t.Fatal("loaded key differs from saved key")
	}
}

func TestLoadKeyNotExists(t *testing.T) {
	_, err := LoadKey("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for nonexistent key file")
	}
}
