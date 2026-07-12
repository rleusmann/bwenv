package cli

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestCoreDumpsDisabledForSecretCommands(t *testing.T) {
	inProjectDir(t, testYAML)
	withFakeProvider(t, &fakeProv{values: map[string]string{
		"app/password": "geheim",
		"app/username": "user1",
	}}, nil)

	// macOS default ist bereits 0 — hochsetzen, damit der Test beweist,
	// dass bwenv selbst das Limit senkt.
	if err := syscall.Setrlimit(syscall.RLIMIT_CORE, &syscall.Rlimit{Cur: 1 << 20, Max: 1 << 20}); err != nil {
		t.Skipf("Setrlimit nicht möglich: %v", err)
	}

	_, _, code := execute("show")
	if code != 0 {
		t.Fatalf("show: exit=%d", code)
	}

	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_CORE, &lim); err != nil {
		t.Fatal(err)
	}
	if lim.Cur != 0 {
		t.Errorf("RLIMIT_CORE.Cur = %d, want 0 (keine Core-Dumps mit Secrets im Speicher)", lim.Cur)
	}
}

func TestConfigServerCallsBw(t *testing.T) {
	// Fake-bw ins PATH legen, das seine Argumente protokolliert.
	dir := t.TempDir()
	logFile := filepath.Join(dir, "args.log")
	script := "#!/bin/sh\necho \"$@\" > " + logFile + "\n"
	if err := os.WriteFile(filepath.Join(dir, "bw"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	_, stderr, code := execute("config", "server", "https://vault.example.com")
	if code != 0 {
		t.Fatalf("config server: exit=%d stderr=%q", code, stderr)
	}
	logged, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("bw wurde nicht aufgerufen: %v", err)
	}
	if strings.TrimSpace(string(logged)) != "config server https://vault.example.com" {
		t.Errorf("bw-Aufruf = %q", logged)
	}
}
