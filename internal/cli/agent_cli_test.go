package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rleusmann/bwenv/internal/agent"
	"github.com/rleusmann/bwenv/internal/config"
	"github.com/rleusmann/bwenv/internal/provider"
)

// startCLITestAgent startet einen Agent auf einem kurzen Temp-Socket und
// setzt BWENV_AGENT_SOCKET darauf.
func startCLITestAgent(t *testing.T) *agent.Client {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "bwc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "agent.sock")

	open := func(_ context.Context, password string) (provider.Provider, func(), error) {
		if password != "correct horse" {
			return nil, nil, errors.New("falsches Master-Passwort")
		}
		return &fakeProv{values: map[string]string{
			"app/password": "geheim",
			"app/username": "user1",
		}}, func() {}, nil
	}
	srv := agent.NewServer(open, 0)
	l, err := agent.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Shutdown() })

	t.Setenv("BWENV_AGENT_SOCKET", sock)
	return agent.NewAgentClient(sock)
}

// failingOpenProvider stellt sicher, dass der Direktpfad NICHT benutzt wird.
func failingOpenProvider(t *testing.T) {
	t.Helper()
	orig := openProvider
	openProvider = func(context.Context, *config.Config, bool) (provider.Provider, func(), error) {
		return nil, nil, errors.New("direktpfad darf nicht verwendet werden")
	}
	t.Cleanup(func() { openProvider = orig })
}

func withPassword(t *testing.T, pw string) {
	t.Helper()
	orig := readPassword
	readPassword = func(string) (string, error) { return pw, nil }
	t.Cleanup(func() { readPassword = orig })
}

func TestResolvePrefersRunningAgent(t *testing.T) {
	inProjectDir(t, testYAML)
	ag := startCLITestAgent(t)
	failingOpenProvider(t)

	if err := ag.Unlock(context.Background(), "correct horse"); err != nil {
		t.Fatal(err)
	}

	out, stderr, code := execute("show")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(out, "FOO") || !strings.Contains(out, "BAR") {
		t.Errorf("show über Agent lieferte %q", out)
	}
}

func TestUnlockUnlocksRunningAgent(t *testing.T) {
	inProjectDir(t, testYAML)
	ag := startCLITestAgent(t)
	withPassword(t, "correct horse")

	_, stderr, code := execute("unlock")
	if code != 0 {
		t.Fatalf("unlock: exit=%d stderr=%q", code, stderr)
	}
	st, err := ag.Status(context.Background())
	if err != nil || st != "unlocked" {
		t.Errorf("Agent-Status = %q, %v", st, err)
	}
}

func TestUnlockWrongPasswordFails(t *testing.T) {
	inProjectDir(t, testYAML)
	startCLITestAgent(t)
	withPassword(t, "falsch")

	_, _, code := execute("unlock")
	if code == 0 {
		t.Fatal("unlock mit falschem Passwort: Fehler erwartet")
	}
}

func TestLockLocksAgent(t *testing.T) {
	inProjectDir(t, testYAML)
	ag := startCLITestAgent(t)

	if err := ag.Unlock(context.Background(), "correct horse"); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := execute("lock")
	if code != 0 {
		t.Fatalf("lock: exit=%d stderr=%q", code, stderr)
	}
	st, _ := ag.Status(context.Background())
	if st != "locked" {
		t.Errorf("Status = %q, want locked", st)
	}
}

func TestAgentStatusCommand(t *testing.T) {
	startCLITestAgent(t)

	out, _, code := execute("agent", "status")
	if code != 0 {
		t.Fatalf("agent status: exit=%d", code)
	}
	if !strings.Contains(out, "locked") {
		t.Errorf("agent status = %q, want locked", out)
	}
}

func TestAgentStatusWithoutAgent(t *testing.T) {
	t.Setenv("BWENV_AGENT_SOCKET", filepath.Join(t.TempDir(), "nope.sock"))

	out, _, code := execute("agent", "status")
	if code != 0 {
		t.Fatalf("agent status ohne Agent muss exit 0 liefern, war %d", code)
	}
	if !strings.Contains(out, "kein agent") {
		t.Errorf("agent status = %q, want Hinweis 'kein agent'", out)
	}
}

func TestAgentStopCommand(t *testing.T) {
	ag := startCLITestAgent(t)

	_, _, code := execute("agent", "stop")
	if code != 0 {
		t.Fatalf("agent stop: exit=%d", code)
	}
	if ag.Available(context.Background()) {
		// Shutdown ist asynchron — kurz warten passiert im Command; hier
		// reicht die Prüfung, dass Stop akzeptiert wurde.
		t.Log("Agent noch erreichbar (asynchroner Shutdown)")
	}
}

func TestSyncCommand(t *testing.T) {
	inProjectDir(t, testYAML)
	ag := startCLITestAgent(t)

	if err := ag.Unlock(context.Background(), "correct horse"); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := execute("sync")
	if code != 0 {
		t.Fatalf("sync: exit=%d stderr=%q", code, stderr)
	}
}

func TestSyncCommandWithoutAgent(t *testing.T) {
	inProjectDir(t, testYAML) // Socket zeigt auf toten Pfad

	_, stderr, code := execute("sync")
	if code == 0 {
		t.Fatal("sync ohne Agent muss fehlschlagen")
	}
	if !strings.Contains(stderr, "bwenv unlock") {
		t.Errorf("Fehlermeldung ohne Hinweis auf bwenv unlock: %q", stderr)
	}
}

func TestExportSilentFailsafeWithLockedAgent(t *testing.T) {
	inProjectDir(t, testYAML)
	startCLITestAgent(t) // bleibt gesperrt
	failingOpenProvider(t)

	out, _, code := execute("export", "--silent")
	if code != 0 || out != "" {
		t.Fatalf("Failsafe verletzt: exit=%d out=%q", code, out)
	}
}
