// Package cli wires up the bwenv command tree.
package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags.
var Version = "dev"

var errNotImplemented = errors.New("noch nicht implementiert")

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

// Execute runs the root command.
func Execute() error {
	return newRootCmd().Execute()
}
