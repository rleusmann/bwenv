package cli

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/rleusmann/bwenv/internal/trust"
)

// unlockTestAgent startet einen entsperrten Test-Agent.
func unlockTestAgent(t *testing.T) {
	t.Helper()
	ag := startCLITestAgent(t)
	if err := ag.Unlock(context.Background(), "correct horse"); err != nil {
		t.Fatal(err)
	}
}

func TestHookLoadsInAllowedDir(t *testing.T) {
	inProjectDir(t, testYAML)
	unlockTestAgent(t)

	out, _, code := execute("export", "--hook")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	for _, want := range []string{"export BAR='user1'", "export FOO='geheim'", "export BWENV_HOOK_DIR=", "export BWENV_HOOK_VARS='BAR FOO'"} {
		if !strings.Contains(out, want) {
			t.Errorf("Hook-Ausgabe ohne %q:\n%s", want, out)
		}
	}
}

func TestHookNoopWhenSameDir(t *testing.T) {
	inProjectDir(t, testYAML)
	unlockTestAgent(t)
	failingOpenProvider(t)

	cwd, _ := os.Getwd()
	t.Setenv("BWENV_HOOK_DIR", cwd)
	t.Setenv("BWENV_HOOK_VARS", "BAR FOO")

	out, _, code := execute("export", "--hook")
	if code != 0 || out != "" {
		t.Fatalf("Noop erwartet, bekam exit=%d out=%q", code, out)
	}
}

func TestHookUnloadsOnLeave(t *testing.T) {
	dir := t.TempDir() // kein bwenv.yaml
	t.Chdir(dir)
	t.Setenv("XDG_CONFIG_HOME", dir+"/.xdg")
	t.Setenv("BWENV_AGENT_SOCKET", dir+"/kein-agent.sock")
	t.Setenv("BWENV_HOOK_DIR", "/vorheriges/projekt")
	t.Setenv("BWENV_HOOK_VARS", "BAR FOO")

	out, _, code := execute("export", "--hook")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, "unset BAR FOO BWENV_HOOK_DIR BWENV_HOOK_VARS") {
		t.Errorf("Unload fehlt:\n%s", out)
	}
	if strings.Contains(out, "export ") {
		t.Errorf("kein export erwartet:\n%s", out)
	}
}

func TestHookIgnoresUntrustedDir(t *testing.T) {
	inProjectDir(t, testYAML)
	unlockTestAgent(t)
	cwd, _ := os.Getwd()
	if err := trust.Deny(cwd); err != nil {
		t.Fatal(err)
	}

	out, _, code := execute("export", "--hook")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.Contains(out, "export FOO") {
		t.Errorf("nicht erlaubtes Verzeichnis darf nichts laden:\n%s", out)
	}
}

func TestHookFailsafeWhenAgentLocked(t *testing.T) {
	inProjectDir(t, testYAML)
	startCLITestAgent(t) // gesperrt
	failingOpenProvider(t)

	out, _, code := execute("export", "--hook")
	if code != 0 || strings.Contains(out, "export FOO") {
		t.Fatalf("Failsafe verletzt: exit=%d out=%q", code, out)
	}
}

func TestExportRefusesUntrustedDirLoud(t *testing.T) {
	inProjectDir(t, testYAML)
	unlockTestAgent(t)
	cwd, _ := os.Getwd()
	if err := trust.Deny(cwd); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := execute("export")
	if code == 0 {
		t.Fatal("export in nicht erlaubtem Verzeichnis muss laut fehlschlagen")
	}
	if !strings.Contains(stderr, "bwenv allow") {
		t.Errorf("Fehlermeldung ohne Hinweis auf `bwenv allow`: %q", stderr)
	}
}

func TestAllowAndDenyCommands(t *testing.T) {
	inProjectDir(t, testYAML)
	cwd, _ := os.Getwd()
	if err := trust.Deny(cwd); err != nil {
		t.Fatal(err)
	}

	_, _, code := execute("allow")
	if code != 0 {
		t.Fatalf("allow: exit=%d", code)
	}
	ok, _ := trust.IsAllowed(cwd)
	if !ok {
		t.Fatal("Verzeichnis nach `bwenv allow` nicht erlaubt")
	}

	_, _, code = execute("deny")
	if code != 0 {
		t.Fatalf("deny: exit=%d", code)
	}
	ok, _ = trust.IsAllowed(cwd)
	if ok {
		t.Fatal("Verzeichnis nach `bwenv deny` weiterhin erlaubt")
	}
}

func TestExportGlobalSection(t *testing.T) {
	inProjectDir(t, testYAML) // setzt XDG_CONFIG_HOME isoliert
	unlockTestAgent(t)

	// Globale Config mit global:-Sektion anlegen.
	cfgDir := os.Getenv("XDG_CONFIG_HOME") + "/bwenv"
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	globalYAML := "version: 1\nglobal:\n  - env: GH_TOKEN\n    item: \"app\"\n    field: password\n"
	if err := os.WriteFile(cfgDir+"/config.yaml", []byte(globalYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	out, stderr, code := execute("export", "--global")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(out, "export GH_TOKEN='geheim'") {
		t.Errorf("global-Export fehlt: %q", out)
	}
	if strings.Contains(out, "FOO") {
		t.Errorf("--global darf keine Projekt-Secrets laden: %q", out)
	}
}

func TestExportGlobalSilentWithoutGlobalConfig(t *testing.T) {
	inProjectDir(t, testYAML) // keine globale Config vorhanden

	out, _, code := execute("export", "--global", "--silent")
	if code != 0 || out != "" {
		t.Fatalf("Failsafe verletzt: exit=%d out=%q", code, out)
	}
}

func TestHookZshSnippet(t *testing.T) {
	out, _, code := execute("hook", "zsh")
	if code != 0 {
		t.Fatalf("hook zsh: exit=%d", code)
	}
	for _, want := range []string{"_bwenv_hook", "chpwd_functions", "export --hook", "--timeout"} {
		if !strings.Contains(out, want) {
			t.Errorf("Snippet ohne %q:\n%s", want, out)
		}
	}
}
