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
	hookDirVar    = "BWENV_HOOK_DIR"
	hookVarsVar   = "BWENV_HOOK_VARS"
	hookGlobalVar = "BWENV_HOOK_GLOBAL"
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

// hookOutput berechnet das eval-bare Skript für den precmd-Hook:
// global:-Secrets einmal pro Shell (sobald ein entsperrter Agent da ist),
// Projekt-Secrets beim Betreten/Verlassen erlaubter Verzeichnisse.
// Neues Laden läuft ausschließlich über den Agent — der Hook startet nie
// selbst bw serve (zu langsam für jeden Prompt). Fehler führen nie zu
// einem Fehl-Exit (Failsafe), höchstens zu weniger Output.
func hookOutput(ctx context.Context) string {
	prevDir := os.Getenv(hookDirVar)
	prevVars := strings.Fields(os.Getenv(hookVarsVar))
	globalDone := os.Getenv(hookGlobalVar) != ""

	newDir := currentTrustedConfigDir()
	if newDir == prevDir && globalDone {
		return "" // Nichts zu tun — kein Socket-, kein Vault-Zugriff.
	}

	agentReady := false
	if _, st, ok := tryAgent(ctx); ok && st == "unlocked" {
		agentReady = true
	}

	var b strings.Builder

	// Überall-Secrets einmal pro Shell; solange kein entsperrter Agent da
	// ist, bleibt das Flag ungesetzt und der nächste Prompt versucht es neu.
	if !globalDone && agentReady {
		if env, err := resolveGlobal(ctx, false); err == nil {
			if exports, err := format.ShellExports(env); err == nil {
				b.WriteString(exports)
				b.WriteString(exportStatement(hookGlobalVar, "1"))
			}
		}
	}

	// Projekt-Teil (direnv-Stil Load/Unload).
	if newDir != prevDir {
		if prevDir != "" {
			names := append(append([]string{}, prevVars...), hookDirVar, hookVarsVar)
			b.WriteString("unset " + strings.Join(names, " ") + "\n")
		}
		if newDir != "" && agentReady {
			if env, err := resolveProject(ctx, false); err == nil {
				if exports, err := format.ShellExports(env); err == nil {
					names := make([]string, 0, len(env))
					for name := range env {
						names = append(names, name)
					}
					sort.Strings(names)
					b.WriteString(exports)
					b.WriteString(exportStatement(hookDirVar, newDir))
					b.WriteString(exportStatement(hookVarsVar, strings.Join(names, " ")))
				}
			}
		}
	}
	return b.String()
}

// exportStatement rendert ein einzelnes export-Statement.
func exportStatement(name, value string) string {
	out, err := format.ShellExports(map[string]string{name: value})
	if err != nil {
		return ""
	}
	return out
}

// zshHookSnippet ist die Ausgabe von `bwenv hook zsh` (für die .zshrc).
// precmd statt chpwd: so kommen global:-Secrets und Projekt-Secrets auch
// in bereits offenen Shells beim nächsten Prompt an, sobald `bwenv unlock`
// gelaufen ist.
const zshHookSnippet = `# bwenv hook für zsh — in der .zshrc: eval "$(bwenv hook zsh)"
_bwenv_hook() {
  eval "$(command bwenv export --hook --timeout=300ms)"
}
typeset -ag precmd_functions
if (( ! ${precmd_functions[(I)_bwenv_hook]} )); then
  precmd_functions+=(_bwenv_hook)
fi
_bwenv_hook
`
