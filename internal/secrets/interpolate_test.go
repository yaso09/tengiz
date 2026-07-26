package secrets

import (
	"testing"
)

func TestResolveInterpolationsNoSecrets(t *testing.T) {
	env := map[string]string{
		"HOST": "localhost",
		"PORT": "5432",
	}
	result := ResolveInterpolations(env, nil)
	if result["HOST"] != "localhost" || result["PORT"] != "5432" {
		t.Fatal("no secrets should leave env unchanged")
	}
}

func TestResolveInterpolationsNoMatches(t *testing.T) {
	env := map[string]string{
		"URL": "postgres://localhost:5432/mydb",
	}
	secrets := map[string]string{"PASSWORD": "hunter2"}
	result := ResolveInterpolations(env, secrets)
	if result["URL"] != "postgres://localhost:5432/mydb" {
		t.Fatal("no [[secret.*]] pattern should leave values unchanged")
	}
}

func TestResolveInterpolationsSingleMatch(t *testing.T) {
	env := map[string]string{
		"DATABASE_URL": "postgres://user:[[secret.DB_PASSWORD]]@localhost:5432/mydb",
	}
	secrets := map[string]string{"DB_PASSWORD": "hunter2"}
	result := ResolveInterpolations(env, secrets)
	expected := "postgres://user:hunter2@localhost:5432/mydb"
	if result["DATABASE_URL"] != expected {
		t.Fatalf("expected %q, got %q", expected, result["DATABASE_URL"])
	}
}

func TestResolveInterpolationsMultipleMatches(t *testing.T) {
	env := map[string]string{
		"URL": "http://[[secret.USER]]:[[secret.PASS]]@example.com",
	}
	secrets := map[string]string{"USER": "admin", "PASS": "s3cret"}
	result := ResolveInterpolations(env, secrets)
	expected := "http://admin:s3cret@example.com"
	if result["URL"] != expected {
		t.Fatalf("expected %q, got %q", expected, result["URL"])
	}
}

func TestResolveInterpolationsMissingSecret(t *testing.T) {
	env := map[string]string{
		"URL": "http://[[secret.MISSING]]@example.com",
	}
	secrets := map[string]string{"OTHER": "val"}
	result := ResolveInterpolations(env, secrets)
	expected := "http://[[secret.MISSING]]@example.com"
	if result["URL"] != expected {
		t.Fatalf("expected %q, got %q", expected, result["URL"])
	}
}

func TestResolveInterpolationsSecretItself(t *testing.T) {
	secrets := map[string]string{"SECRET_A": "val_a"}
	env := map[string]string{
		"DIRECT": "[[secret.SECRET_A]]",
	}
	result := ResolveInterpolations(env, secrets)
	if result["DIRECT"] != "val_a" {
		t.Fatalf("expected val_a, got %q", result["DIRECT"])
	}
}
