package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const keyDirName = "ssh"
const privateKeyName = "id_ed25519"
const publicKeyName = "id_ed25519.pub"

func keyDir(dataDir string) string {
	return filepath.Join(dataDir, keyDirName)
}

func privateKeyPath(dataDir string) string {
	return filepath.Join(keyDir(dataDir), privateKeyName)
}

func publicKeyPath(dataDir string) string {
	return filepath.Join(keyDir(dataDir), publicKeyName)
}

func EnsureKeyDir(dataDir string) error {
	return os.MkdirAll(keyDir(dataDir), 0700)
}

func HasKey(dataDir string) bool {
	_, err := os.Stat(privateKeyPath(dataDir))
	return err == nil
}

func GenerateKey(dataDir string) (string, error) {
	if err := EnsureKeyDir(dataDir); err != nil {
		return "", fmt.Errorf("ensure key dir: %w", err)
	}

	keyPath := privateKeyPath(dataDir)
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", keyPath, "-N", "", "-q")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ssh-keygen: %w", err)
	}

	if err := os.Chmod(keyPath, 0600); err != nil {
		return "", fmt.Errorf("chmod private key: %w", err)
	}

	pub, err := PublicKey(dataDir)
	if err != nil {
		return "", fmt.Errorf("read public key: %w", err)
	}

	return pub, nil
}

func PublicKey(dataDir string) (string, error) {
	data, err := os.ReadFile(publicKeyPath(dataDir))
	if err != nil {
		return "", fmt.Errorf("read public key: %w", err)
	}
	return string(data), nil
}

func RemoveKey(dataDir string) error {
	os.Remove(privateKeyPath(dataDir))
	os.Remove(publicKeyPath(dataDir))
	return nil
}
