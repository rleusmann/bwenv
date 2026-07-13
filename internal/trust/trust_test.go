package trust

import (
	"os"
	"path/filepath"
	"testing"
)

func setup(t *testing.T) string {
	t.Helper()
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	return cfgHome
}

func TestAllowThenIsAllowed(t *testing.T) {
	setup(t)
	dir := t.TempDir()

	ok, err := IsAllowed(dir)
	if err != nil {
		t.Fatalf("IsAllowed: %v", err)
	}
	if ok {
		t.Fatal("Verzeichnis darf initial nicht erlaubt sein")
	}

	if err := Allow(dir); err != nil {
		t.Fatalf("Allow: %v", err)
	}
	ok, err = IsAllowed(dir)
	if err != nil || !ok {
		t.Fatalf("IsAllowed nach Allow = %v, %v", ok, err)
	}
}

func TestDenyRemovesTrust(t *testing.T) {
	setup(t)
	dir := t.TempDir()

	if err := Allow(dir); err != nil {
		t.Fatal(err)
	}
	if err := Deny(dir); err != nil {
		t.Fatalf("Deny: %v", err)
	}
	ok, _ := IsAllowed(dir)
	if ok {
		t.Fatal("Verzeichnis nach Deny weiterhin erlaubt")
	}
}

func TestDenyWithoutAllowIsNoop(t *testing.T) {
	setup(t)
	if err := Deny(t.TempDir()); err != nil {
		t.Fatalf("Deny ohne Allow muss ohne Fehler durchgehen: %v", err)
	}
}

func TestTrustIsPathSpecific(t *testing.T) {
	setup(t)
	a, b := t.TempDir(), t.TempDir()

	if err := Allow(a); err != nil {
		t.Fatal(err)
	}
	ok, _ := IsAllowed(b)
	if ok {
		t.Fatal("fremdes Verzeichnis darf nicht erlaubt sein")
	}
}

func TestAllowStoreFilesContainPath(t *testing.T) {
	cfgHome := setup(t)
	dir := t.TempDir()

	if err := Allow(dir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(cfgHome, "bwenv", "allow"))
	if err != nil {
		t.Fatalf("Allow-Verzeichnis fehlt: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d Einträge, want 1", len(entries))
	}
	content, err := os.ReadFile(filepath.Join(cfgHome, "bwenv", "allow", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(dir)
	if string(content) != resolved+"\n" {
		t.Errorf("Datei-Inhalt = %q, want Pfad %q", content, resolved)
	}
}

func TestSymlinkVariantsMatch(t *testing.T) {
	setup(t)
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	if err := Allow(link); err != nil {
		t.Fatal(err)
	}
	ok, _ := IsAllowed(real)
	if !ok {
		t.Error("Symlink-Variante und realer Pfad müssen als derselbe Trust-Eintrag gelten")
	}
}
