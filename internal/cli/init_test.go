package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rleusmann/bwenv/internal/config"
)

// inEmptyDir wechselt in ein isoliertes Verzeichnis ohne bwenv.yaml.
func inEmptyDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".xdg"))
	t.Setenv("BWENV_AGENT_SOCKET", filepath.Join(dir, "kein-agent.sock"))
	return dir
}

func TestInitCreatesLocalTemplate(t *testing.T) {
	dir := inEmptyDir(t)

	out, _, code := execute("init")
	if code != 0 {
		t.Fatalf("init: exit=%d", code)
	}
	path := filepath.Join(dir, "bwenv.yaml")
	if !strings.Contains(out, "bwenv.yaml") {
		t.Errorf("Ausgabe ohne Pfad: %q", out)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("bwenv.yaml fehlt: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("Rechte = %o, want 600", perm)
	}

	// Template muss valide parsen und inert sein (keine aktiven Einträge).
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Template parst nicht: %v", err)
	}
	if len(cfg.Secrets) != 0 || len(cfg.Global) != 0 {
		t.Errorf("Template darf keine aktiven Einträge haben: %+v", cfg)
	}

	// Platzhalter-Beispiele als Kommentare vorhanden.
	content, _ := os.ReadFile(path)
	for _, want := range []string{"# ", "item:", "field:", "field-name-as-env"} {
		if !strings.Contains(string(content), want) {
			t.Errorf("Template ohne %q:\n%s", want, content)
		}
	}
}

func TestInitRefusesToOverwrite(t *testing.T) {
	dir := inEmptyDir(t)
	path := filepath.Join(dir, "bwenv.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nsecrets: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := execute("init")
	if code == 0 {
		t.Fatal("init über bestehende Datei muss fehlschlagen")
	}
	if !strings.Contains(stderr, "existiert") {
		t.Errorf("Fehlermeldung unklar: %q", stderr)
	}
	content, _ := os.ReadFile(path)
	if string(content) != "version: 1\nsecrets: []\n" {
		t.Error("bestehende Datei wurde verändert")
	}
}

func TestInitGlobalCreatesConfig(t *testing.T) {
	inEmptyDir(t)

	out, _, code := execute("init", "--global")
	if code != 0 {
		t.Fatalf("init --global: exit=%d", code)
	}
	path, err := config.GlobalPath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, path) {
		t.Errorf("Ausgabe ohne Pfad %q: %q", path, out)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("globale Config fehlt: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("Datei-Rechte = %o, want 600", perm)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("Verzeichnis-Rechte = %o, want 700", perm)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("globales Template parst nicht: %v", err)
	}
	if len(cfg.Global) != 0 || len(cfg.Secrets) != 0 {
		t.Errorf("Template darf keine aktiven Einträge haben: %+v", cfg)
	}
	content, _ := os.ReadFile(path)
	if !strings.Contains(string(content), "global") {
		t.Errorf("globales Template ohne global-Beispiel:\n%s", content)
	}
}

func TestInitGlobalRefusesToOverwrite(t *testing.T) {
	inEmptyDir(t)
	path, _ := config.GlobalPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("version: 1\nglobal: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, code := execute("init", "--global")
	if code == 0 {
		t.Fatal("init --global über bestehende Config muss fehlschlagen")
	}
	content, _ := os.ReadFile(path)
	if string(content) != "version: 1\nglobal: []\n" {
		t.Error("bestehende globale Config wurde verändert")
	}
}
