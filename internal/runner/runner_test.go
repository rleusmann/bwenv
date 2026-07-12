package runner

import (
	"bytes"
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestRunInjectsEnv(t *testing.T) {
	var out bytes.Buffer
	code, err := Run(context.Background(), []string{"sh", "-c", `printf '%s' "$BWENV_TEST_VAR"`}, Options{
		Env:    map[string]string{"BWENV_TEST_VAR": "geheim-123"},
		Stdout: &out,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("Exit-Code = %d, want 0", code)
	}
	if out.String() != "geheim-123" {
		t.Errorf("Ausgabe = %q, want geheim-123", out.String())
	}
}

func TestRunInheritsParentEnv(t *testing.T) {
	t.Setenv("BWENV_PARENT_VAR", "vom-parent")
	var out bytes.Buffer
	code, err := Run(context.Background(), []string{"sh", "-c", `printf '%s' "$BWENV_PARENT_VAR"`}, Options{
		Stdout: &out,
	})
	if err != nil || code != 0 {
		t.Fatalf("Run: code=%d err=%v", code, err)
	}
	if out.String() != "vom-parent" {
		t.Errorf("Ausgabe = %q, want vom-parent (geerbte Env fehlt)", out.String())
	}
}

func TestRunInjectedEnvOverridesParent(t *testing.T) {
	t.Setenv("BWENV_TEST_VAR", "alt")
	var out bytes.Buffer
	_, err := Run(context.Background(), []string{"sh", "-c", `printf '%s' "$BWENV_TEST_VAR"`}, Options{
		Env:    map[string]string{"BWENV_TEST_VAR": "neu"},
		Stdout: &out,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.String() != "neu" {
		t.Errorf("Ausgabe = %q, want neu (Injektion muss Parent-Env überschreiben)", out.String())
	}
}

func TestRunExitCodePassthrough(t *testing.T) {
	code, err := Run(context.Background(), []string{"sh", "-c", "exit 3"}, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 3 {
		t.Errorf("Exit-Code = %d, want 3", code)
	}
}

func TestRunCommandNotFound(t *testing.T) {
	_, err := Run(context.Background(), []string{"/gibt/es/nicht"}, Options{})
	if err == nil {
		t.Fatal("Fehler für nicht existierendes Kommando erwartet")
	}
}

func TestRunForwardsSignals(t *testing.T) {
	sigCh := make(chan os.Signal, 1)
	done := make(chan struct{})
	var code int
	var err error

	go func() {
		defer close(done)
		code, err = Run(context.Background(),
			[]string{"sh", "-c", `trap 'exit 42' TERM; while :; do sleep 0.05; done`},
			Options{Signals: sigCh})
	}()

	// Dem Kind Zeit geben, den Trap zu installieren.
	time.Sleep(300 * time.Millisecond)
	sigCh <- syscall.SIGTERM

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Runner hat SIGTERM nicht weitergeleitet (Timeout)")
	}
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 42 {
		t.Errorf("Exit-Code = %d, want 42 (Trap im Kind)", code)
	}
}
