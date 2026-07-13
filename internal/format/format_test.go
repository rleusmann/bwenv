package format

import (
	"strings"
	"testing"
)

func TestShellExportsSortedAndQuoted(t *testing.T) {
	got, err := ShellExports(map[string]string{
		"B_VAR": "einfach",
		"A_VAR": "mit 'quote' und $dollar",
	})
	if err != nil {
		t.Fatalf("ShellExports: %v", err)
	}
	want := "export A_VAR='mit '\\''quote'\\'' und $dollar'\n" +
		"export B_VAR='einfach'\n"
	if got != want {
		t.Errorf("ShellExports =\n%q\nwant\n%q", got, want)
	}
}

func TestShellExportsNewlineInValue(t *testing.T) {
	got, err := ShellExports(map[string]string{"PEM": "zeile1\nzeile2"})
	if err != nil {
		t.Fatalf("ShellExports: %v", err)
	}
	want := "export PEM='zeile1\nzeile2'\n"
	if got != want {
		t.Errorf("ShellExports = %q, want %q", got, want)
	}
}

func TestShellExportsRejectsInvalidName(t *testing.T) {
	_, err := ShellExports(map[string]string{"BAD-NAME": "x"})
	if err == nil {
		t.Fatal("Fehler für ungültigen Variablennamen erwartet")
	}
	_, err = ShellExports(map[string]string{"1LEAD": "x"})
	if err == nil {
		t.Fatal("Fehler für Variablennamen mit führender Ziffer erwartet")
	}
}

func TestUnsets(t *testing.T) {
	got := Unsets(map[string]string{"B": "x", "A": "y"})
	want := "unset A B\n"
	if got != want {
		t.Errorf("Unsets = %q, want %q", got, want)
	}
}

func TestMaskedShowsNamesNotValues(t *testing.T) {
	got := Masked(map[string]string{ //nolint:gosec // G101: Test-Fixture, kein echtes Credential
		"DATABASE_URL": "postgres://user:pass@host/db",
		"API_KEY":      "xyz",
	})
	if strings.Contains(got, "pass") || strings.Contains(got, "xyz") {
		t.Fatalf("Masked enthält Klartext:\n%s", got)
	}
	if !strings.Contains(got, "DATABASE_URL") || !strings.Contains(got, "API_KEY") {
		t.Errorf("Masked enthält Namen nicht:\n%s", got)
	}
	// Sortierung: API_KEY vor DATABASE_URL
	if strings.Index(got, "API_KEY") > strings.Index(got, "DATABASE_URL") {
		t.Errorf("Masked nicht sortiert:\n%s", got)
	}
}

func TestRedactorReplacesAllValues(t *testing.T) {
	r := NewRedactor([]string{"s3cret", "sk_live_abc"})
	in := "fehler: login mit s3cret fehlgeschlagen (key sk_live_abc)"
	got := r.Redact(in)
	if strings.Contains(got, "s3cret") || strings.Contains(got, "sk_live_abc") {
		t.Fatalf("Redact ließ Klartext durch: %q", got)
	}
	if !strings.Contains(got, "***") {
		t.Errorf("Redact ohne Platzhalter: %q", got)
	}
}

func TestRedactorIgnoresShortValues(t *testing.T) {
	// Zu kurze Werte (< 4 Zeichen) nicht redacten — sonst zerlegt "1" jeden Text.
	r := NewRedactor([]string{"1", "ab", ""})
	in := "port 1 und ab und leer"
	if got := r.Redact(in); got != in {
		t.Errorf("Redact veränderte Text mit Kurz-Werten: %q", got)
	}
}
