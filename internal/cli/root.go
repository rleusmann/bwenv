// Package cli wires up the bwenv command tree.
package cli

import (
	"syscall"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags.
var Version = "dev"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "bwenv",
		Short: "Secrets aus Vaultwarden/Bitwarden als Umgebungsvariablen injizieren",
		Long: `bwenv lädt Secrets aus einem (selbstgehosteten) Vaultwarden bzw. Bitwarden
und injiziert sie als Umgebungsvariablen in Subprozesse oder die Shell —
ohne dass Secrets in Shell-History, Prozessliste oder Klartext-Dateien landen.`,
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Hardening (Plan §7): keine Core-Dumps, bevor Secrets in den
			// Prozess-Speicher gelangen können.
			_ = syscall.Setrlimit(syscall.RLIMIT_CORE, &syscall.Rlimit{Cur: 0, Max: 0})
		},
	}

	root.AddCommand(
		newRunCmd(),
		newExportCmd(),
		newShowCmd(),
		newUnlockCmd(),
		newLockCmd(),
		newAgentCmd(),
		newHookCmd(),
		newAllowCmd(),
		newDenyCmd(),
		newConfigCmd(),
	)
	return root
}

// Execute führt den Command-Tree aus und liefert den Prozess-Exit-Code.
func Execute() int {
	return runRoot(newRootCmd())
}
