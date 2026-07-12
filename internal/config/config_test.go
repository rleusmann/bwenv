package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "bwenv.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const validYAML = `
version: 1

provider:
  type: bitwarden
  server: https://vault.example.com
  email: ${BWENV_TEST_EMAIL}

secrets:
  - env: DATABASE_URL
    item: "prod/api"
    field: uri
  - env: STRIPE_KEY
    item_id: "id-2"
    field: password
  - from:
      folder: "dev-env"
    strategy: field-name-as-env

global:
  - env: GITHUB_TOKEN
    item: "gh cli token"
    field: password
`

func TestLoadValidConfig(t *testing.T) {
	t.Setenv("BWENV_TEST_EMAIL", "robert@example.com")
	cfg, err := Load(writeConfig(t, validYAML))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Provider.Type != "bitwarden" {
		t.Errorf("Provider.Type = %q", cfg.Provider.Type)
	}
	if cfg.Provider.Server != "https://vault.example.com" {
		t.Errorf("Provider.Server = %q", cfg.Provider.Server)
	}
	if cfg.Provider.Email != "robert@example.com" {
		t.Errorf("Email nicht expandiert: %q", cfg.Provider.Email)
	}

	if len(cfg.Secrets) != 3 {
		t.Fatalf("len(Secrets) = %d, want 3", len(cfg.Secrets))
	}
	if cfg.Secrets[0].Env != "DATABASE_URL" || cfg.Secrets[0].Item != "prod/api" || cfg.Secrets[0].Field != "uri" {
		t.Errorf("Secrets[0] = %+v", cfg.Secrets[0])
	}
	if cfg.Secrets[1].ItemID != "id-2" {
		t.Errorf("Secrets[1].ItemID = %q", cfg.Secrets[1].ItemID)
	}
	if cfg.Secrets[2].From == nil || cfg.Secrets[2].From.Folder != "dev-env" {
		t.Errorf("Secrets[2].From = %+v", cfg.Secrets[2].From)
	}
	if cfg.Secrets[2].Strategy != "field-name-as-env" {
		t.Errorf("Secrets[2].Strategy = %q", cfg.Secrets[2].Strategy)
	}

	if len(cfg.Global) != 1 || cfg.Global[0].Env != "GITHUB_TOKEN" {
		t.Errorf("Global = %+v", cfg.Global)
	}
}

func TestLoadRejectsUnsetEnvVar(t *testing.T) {
	// BWENV_TEST_EMAIL absichtlich nicht gesetzt.
	_ = os.Unsetenv("BWENV_TEST_EMAIL")
	_, err := Load(writeConfig(t, validYAML))
	if err == nil || !strings.Contains(err.Error(), "BWENV_TEST_EMAIL") {
		t.Fatalf("Fehler mit Var-Namen erwartet, bekam: %v", err)
	}
}

func TestLoadRejectsUnsupportedVersion(t *testing.T) {
	_, err := Load(writeConfig(t, "version: 99\nsecrets: []\n"))
	if err == nil {
		t.Fatal("Fehler für version 99 erwartet")
	}
}

func TestLoadRejectsSecretWithoutEnv(t *testing.T) {
	_, err := Load(writeConfig(t, `
version: 1
secrets:
  - item: "x"
    field: password
`))
	if err == nil {
		t.Fatal("Fehler für Secret ohne env erwartet")
	}
}

func TestLoadRejectsSecretWithoutItemOrFrom(t *testing.T) {
	_, err := Load(writeConfig(t, `
version: 1
secrets:
  - env: X
    field: password
`))
	if err == nil {
		t.Fatal("Fehler für Secret ohne item/item_id/from erwartet")
	}
}

func TestLoadRejectsEntryMixingItemAndFrom(t *testing.T) {
	_, err := Load(writeConfig(t, `
version: 1
secrets:
  - env: X
    item: "x"
    field: password
    from:
      folder: "f"
`))
	if err == nil {
		t.Fatal("Fehler für Eintrag mit item UND from erwartet")
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	_, err := Load(writeConfig(t, `
version: 1
secrets:
  - env: X
    itme: "typo"
    field: password
`))
	if err == nil {
		t.Fatal("Fehler für unbekannten Schlüssel 'itme' erwartet")
	}
}

func TestLoadMissingFileError(t *testing.T) {
	_, err := Load("/nicht/vorhanden/bwenv.yaml")
	if err == nil {
		t.Fatal("Fehler für fehlende Datei erwartet")
	}
}

func TestFindSearchesUpward(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, "bwenv.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: 1\nsecrets: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	found, err := Find(sub)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	// macOS: /var → /private/var, deshalb über EvalSymlinks vergleichen.
	wantPath, _ := filepath.EvalSymlinks(cfgPath)
	gotPath, _ := filepath.EvalSymlinks(found)
	if gotPath != wantPath {
		t.Errorf("Find = %q, want %q", found, cfgPath)
	}
}

func TestFindNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := Find(dir)
	if err == nil {
		t.Fatal("Fehler erwartet, wenn keine bwenv.yaml existiert")
	}
}
