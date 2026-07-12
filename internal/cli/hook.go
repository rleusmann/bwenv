package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rleusmann/bwenv/internal/config"
	"github.com/rleusmann/bwenv/internal/format"
	"github.com/rleusmann/bwenv/internal/trust"
)

// Statusvariablen, über die der Hook Load-Zustand zwischen Aufrufen trägt.
const (
	hookDirVar  = "BWENV_HOOK_DIR"
	hookVarsVar = "BWENV_HOOK_VARS"
)

// currentTrustedConfigDir liefert das Verzeichnis der nächstgelegenen
// bwenv.yaml, sofern es per Allowlist freigegeben ist — sonst "".
func currentTrustedConfigDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	cfgPath, err := config.Find(cwd)
	if err != nil {
		return ""
	}
	dir := filepath.Dir(cfgPath)
	ok, err := trust.IsAllowed(dir)
	if err != nil || !ok {
		if os.Getenv("BWENV_QUIET") == "" {
			fmt.Fprintf(os.Stderr, "bwenv: %s ist nicht freigegeben — `bwenv allow` zum Erlauben\n", dir)
		}
		return ""
	}
	return dir
}

// hookOutput berechnet das eval-bare Skript für den chpwd-Hook:
// Unload beim Verlassen, Load beim Betreten eines erlaubten Verzeichnisses.
// Fehler führen nie zu einem Fehl-Exit (Failsafe) — höchstens zu weniger Output.
func hookOutput(ctx context.Context) string {
	prevDir := os.Getenv(hookDirVar)
	prevVars := strings.Fields(os.Getenv(hookVarsVar))

	newDir := currentTrustedConfigDir()
	if newDir == prevDir {
		return "" // Kein Wechsel — kein Vault-Zugriff, keine Ausgabe.
	}

	var b strings.Builder
	if prevDir != "" {
		names := append(append([]string{}, prevVars...), hookDirVar, hookVarsVar)
		b.WriteString("unset " + strings.Join(names, " ") + "\n")
	}
	if newDir != "" {
		env, err := resolveProject(ctx, false)
		if err != nil {
			return b.String() // Failsafe: nur Unload; nächster Hook versucht es erneut.
		}
		exports, err := format.ShellExports(env)
		if err != nil {
			return b.String()
		}
		names := make([]string, 0, len(env))
		for name := range env {
			names = append(names, name)
		}
		sort.Strings(names)

		b.WriteString(exports)
		state, err := format.ShellExports(map[string]string{
			hookDirVar:  newDir,
			hookVarsVar: strings.Join(names, " "),
		})
		if err != nil {
			return ""
		}
		b.WriteString(state)
	}
	return b.String()
}

// zshHookSnippet ist die Ausgabe von `bwenv hook zsh` (für die .zshrc).
const zshHookSnippet = `# bwenv hook für zsh — in der .zshrc: eval "$(bwenv hook zsh)"
_bwenv_hook() {
  eval "$(command bwenv export --hook --timeout=300ms)"
}
typeset -ag chpwd_functions
if (( ! ${chpwd_functions[(I)_bwenv_hook]} )); then
  chpwd_functions+=(_bwenv_hook)
fi
# Überall-Secrets (global:-Sektion) einmal beim Shell-Init laden.
eval "$(command bwenv export --global --silent --timeout=500ms)"
# Initialer Lauf für das aktuelle Verzeichnis.
_bwenv_hook
`
