// Package trust verwaltet die Allowlist der Verzeichnisse, die per
// Shell-Hook automatisch Secrets laden dürfen (direnv-Stil: eine Datei
// pro Pfad, benannt nach dem Pfad-Hash).
package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// storeDir liefert das Allowlist-Verzeichnis (~/.config/bwenv/allow,
// respektiert $XDG_CONFIG_HOME).
func storeDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "bwenv", "allow"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "bwenv", "allow"), nil
}

// normalize löst Symlinks auf und macht den Pfad absolut, damit alle
// Schreibweisen desselben Verzeichnisses denselben Eintrag ergeben.
func normalize(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// Verzeichnis existiert (noch) nicht — absoluter Pfad genügt.
		return abs, nil //nolint:nilerr // bewusster Fallback
	}
	return resolved, nil
}

func entryPath(dir string) (string, string, error) {
	store, err := storeDir()
	if err != nil {
		return "", "", err
	}
	norm, err := normalize(dir)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(norm))
	return filepath.Join(store, hex.EncodeToString(sum[:])), norm, nil
}

// Allow erlaubt dir für den Auto-Load.
func Allow(dir string) error {
	entry, norm, err := entryPath(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(entry), 0o700); err != nil {
		return err
	}
	return os.WriteFile(entry, []byte(norm+"\n"), 0o600)
}

// Deny entfernt dir aus der Allowlist (kein Fehler, wenn nicht vorhanden).
func Deny(dir string) error {
	entry, _, err := entryPath(dir)
	if err != nil {
		return err
	}
	err = os.Remove(entry)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// IsAllowed meldet, ob dir für den Auto-Load freigegeben ist.
func IsAllowed(dir string) (bool, error) {
	entry, _, err := entryPath(dir)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(entry)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
