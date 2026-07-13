// Package format erzeugt Shell-Ausgaben (export/unset), maskierte
// Anzeigen und redigiert Secret-Werte aus Fehlermeldungen.
package format

import (
	"fmt"
	"sort"
	"strings"
)

// ShellExports rendert deterministisch sortierte, POSIX-korrekt gequotete
// export-Statements (gültig für sh und zsh).
func ShellExports(env map[string]string) (string, error) {
	names := sortedNames(env)
	var b strings.Builder
	for _, name := range names {
		if !validName(name) {
			return "", fmt.Errorf("ungültiger Variablenname: %q", name)
		}
		b.WriteString("export ")
		b.WriteString(name)
		b.WriteString("=")
		b.WriteString(singleQuote(env[name]))
		b.WriteString("\n")
	}
	return b.String(), nil
}

// Unsets rendert ein unset-Statement für alle Variablen (für den
// direnv-Stil-Unload beim Verlassen eines Verzeichnisses).
func Unsets(env map[string]string) string {
	names := sortedNames(env)
	if len(names) == 0 {
		return ""
	}
	return "unset " + strings.Join(names, " ") + "\n"
}

// Masked rendert Variablennamen mit maskierten Werten.
func Masked(env map[string]string) string {
	names := sortedNames(env)
	var b strings.Builder
	for _, name := range names {
		b.WriteString(name)
		b.WriteString("=***\n")
	}
	return b.String()
}

// singleQuote quotet s für POSIX-Shells: '…' mit '\'' für eingebettete Quotes.
func singleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func validName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		isAlpha := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_'
		isDigit := r >= '0' && r <= '9'
		if i == 0 && !isAlpha {
			return false
		}
		if !isAlpha && !isDigit {
			return false
		}
	}
	return true
}

func sortedNames(env map[string]string) []string {
	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Redactor ersetzt bekannte Secret-Werte in Texten durch ***.
type Redactor struct {
	values []string
}

// minRedactLen: kürzere Werte werden nicht redigiert, weil sie sonst
// beliebigen Text zerstückeln (z. B. eine einstellige Portnummer).
const minRedactLen = 4

// NewRedactor baut einen Redactor über die gegebenen Secret-Werte.
func NewRedactor(values []string) *Redactor {
	kept := make([]string, 0, len(values))
	for _, v := range values {
		if len(v) >= minRedactLen {
			kept = append(kept, v)
		}
	}
	// Längere Werte zuerst, damit Teilstrings sauber ersetzt werden.
	sort.Slice(kept, func(i, j int) bool { return len(kept[i]) > len(kept[j]) })
	return &Redactor{values: kept}
}

// Redact ersetzt alle bekannten Werte in s durch ***.
func (r *Redactor) Redact(s string) string {
	for _, v := range r.values {
		s = strings.ReplaceAll(s, v, "***")
	}
	return s
}
