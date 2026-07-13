package cli

import (
	"io"
	"os"
	"testing"
)

// Regression: cobra's cmd.Print fällt ohne SetOut auf stderr zurück.
// Damit landete produktiv (ohne das SetOut der Tests) jede Ausgabe —
// hook-Snippet, export-Statements — auf stderr, und eval "$(bwenv …)"
// bekam nichts. Ausgaben MÜSSEN auf stdout gehen.
func TestCommandOutputGoesToStdout(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origOut := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = origOut }()

	root := newRootCmd() // muss os.Stdout (jetzt der Pipe-Writer) übernehmen
	root.Print("stdout-marker")

	_ = w.Close()
	os.Stdout = origOut
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "stdout-marker" {
		t.Fatalf("cmd.Print schrieb nicht auf stdout (gelesen: %q) — cobra-Fallback auf stderr aktiv", got)
	}
}
