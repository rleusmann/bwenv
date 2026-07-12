// Package config liest und validiert bwenv.yaml.
//
// Die Datei enthält ausschließlich Referenzen auf Vault-Items — niemals
// Secret-Werte oder Passwörter.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// FileName ist der Name der Projekt-Konfigurationsdatei.
const FileName = "bwenv.yaml"

// Config ist das geparste bwenv.yaml.
type Config struct {
	Version  int           `yaml:"version"`
	Provider Provider      `yaml:"provider"`
	Secrets  []SecretEntry `yaml:"secrets"`
	Global   []SecretEntry `yaml:"global"`
}

// Provider beschreibt das Secret-Backend.
type Provider struct {
	Type   string `yaml:"type"`
	Server string `yaml:"server"`
	Email  string `yaml:"email"`
}

// SecretEntry ist ein Eintrag unter secrets: oder global:.
// Entweder Item/ItemID+Field (einzelnes Secret) oder From+Strategy (bulk).
type SecretEntry struct {
	Env      string `yaml:"env"`
	Item     string `yaml:"item"`
	ItemID   string `yaml:"item_id"`
	Field    string `yaml:"field"`
	From     *From  `yaml:"from"`
	Strategy string `yaml:"strategy"`
}

// From beschreibt eine Bulk-Quelle.
type From struct {
	Folder string `yaml:"folder"`
}

// Load liest, expandiert (${VAR} in provider.server/email) und validiert
// eine bwenv.yaml.
func Load(path string) (*Config, error) {
	f, err := os.Open(path) //nolint:gosec // G304: Pfad kommt vom Aufrufer (CLI/Hook), kein Server-Input
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	if err := cfg.expandEnv(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &cfg, nil
}

// Find sucht von dir aus aufwärts nach einer bwenv.yaml.
func Find(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, FileName)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("keine %s gefunden (aufwärts ab %s)", FileName, dir)
		}
		dir = parent
	}
}

// expandEnv löst ${VAR}-Referenzen in provider.server und provider.email auf.
// Nicht gesetzte Variablen sind ein Fehler.
func (c *Config) expandEnv() error {
	var missing []string
	expand := func(s string) string {
		return os.Expand(s, func(name string) string {
			val, ok := os.LookupEnv(name)
			if !ok {
				missing = append(missing, name)
			}
			return val
		})
	}
	c.Provider.Server = expand(c.Provider.Server)
	c.Provider.Email = expand(c.Provider.Email)
	if len(missing) > 0 {
		return fmt.Errorf("umgebungsvariable(n) nicht gesetzt: %v", missing)
	}
	return nil
}

func (c *Config) validate() error {
	if c.Version != 1 {
		return fmt.Errorf("nicht unterstützte config-version %d (erwartet: 1)", c.Version)
	}
	for i, e := range c.Secrets {
		if err := e.validate(); err != nil {
			return fmt.Errorf("secrets[%d]: %w", i, err)
		}
	}
	for i, e := range c.Global {
		if err := e.validate(); err != nil {
			return fmt.Errorf("global[%d]: %w", i, err)
		}
	}
	return nil
}

func (e *SecretEntry) validate() error {
	isBulk := e.From != nil
	isSingle := e.Item != "" || e.ItemID != ""

	switch {
	case isBulk && isSingle:
		return errors.New("item/item_id und from schließen sich aus")
	case isBulk:
		if e.From.Folder == "" {
			return errors.New("from.folder fehlt")
		}
		if e.Strategy != "field-name-as-env" {
			return fmt.Errorf("unbekannte strategy %q (unterstützt: field-name-as-env)", e.Strategy)
		}
		if e.Env != "" || e.Field != "" {
			return errors.New("env/field sind bei from-Einträgen nicht erlaubt")
		}
	case isSingle:
		if e.Env == "" {
			return errors.New("env fehlt")
		}
		if e.Field == "" {
			return errors.New("field fehlt")
		}
	default:
		return errors.New("weder item/item_id noch from angegeben")
	}
	return nil
}
