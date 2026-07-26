package secrets

import (
	"os"
	"testing"
)

func TestVaultProviderImplementsProvider(t *testing.T) {
	var _ Provider = (*VaultProvider)(nil)
}

func TestVaultProviderRequiresConfig(t *testing.T) {
	_, err := NewVaultProvider(VaultConfig{})
	if err == nil {
		t.Fatal("expected error with empty config")
	}
}

func TestVaultProviderName(t *testing.T) {
	addr := os.Getenv("TENGIZ_VAULT_ADDR")
	token := os.Getenv("TENGIZ_VAULT_TOKEN")
	if addr == "" || token == "" {
		t.Skip("TENGIZ_VAULT_ADDR and TENGIZ_VAULT_TOKEN not set")
	}
	p, err := NewVaultProvider(VaultConfig{
		Address: addr,
		Token:   token,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "vault" {
		t.Fatalf("expected 'vault', got %q", p.Name())
	}
}

func TestVaultProviderSetGetUnsetList(t *testing.T) {
	addr := os.Getenv("TENGIZ_VAULT_ADDR")
	token := os.Getenv("TENGIZ_VAULT_TOKEN")
	if addr == "" || token == "" {
		t.Skip("TENGIZ_VAULT_ADDR and TENGIZ_VAULT_TOKEN not set")
	}
	p, err := NewVaultProvider(VaultConfig{
		Address: addr,
		Token:   token,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Set("testapp", "VAULT_KEY", "vault_val"); err != nil {
		t.Fatal(err)
	}

	val, ok, err := p.Get("testapp", "VAULT_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || val != "vault_val" {
		t.Fatalf("expected vault_val, got %q (ok=%v)", val, ok)
	}

	secrets, err := p.List("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if secrets["VAULT_KEY"] != "vault_val" {
		t.Fatal("List did not return secret")
	}

	if err := p.Unset("testapp", "VAULT_KEY"); err != nil {
		t.Fatal(err)
	}
	_, ok, _ = p.Get("testapp", "VAULT_KEY")
	if ok {
		t.Fatal("expected key to be deleted")
	}
}
