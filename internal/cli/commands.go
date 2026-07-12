package cli

import (
	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run -- <cmd> [args...]",
		Short: "Secrets injizieren und Befehl starten",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented
		},
	}
}

func newExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "export",
		Aliases: []string{"sh"},
		Short:   "Shell-Export-Statements ausgeben (für eval \"$(bwenv sh)\")",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented
		},
	}
	cmd.Flags().String("format", "sh", "Ausgabeformat: sh|zsh")
	cmd.Flags().Bool("silent", false, "bei Fehlern still bleiben (exit 0)")
	cmd.Flags().Duration("timeout", 0, "harter Timeout, z. B. 300ms")
	return cmd
}

func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Geladene Variablennamen anzeigen (Werte maskiert)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented
		},
	}
}

func newUnlockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unlock",
		Short: "Session entsperren (Master-Passwort-Prompt oder Touch ID)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented
		},
	}
	cmd.Flags().Bool("enroll-touchid", false, "Touch-ID-Unlock einrichten (macOS)")
	return cmd
}

func newLockCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lock",
		Short: "Session sofort sperren",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented
		},
	}
}

func newAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent [stop|status]",
		Short: "Agent-Lifecycle verwalten",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented
		},
	}
	return cmd
}

func newHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "hook <shell>",
		Short:     "Shell-Integrations-Snippet ausgeben (z. B. für .zshrc)",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"zsh"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented
		},
	}
}

func newAllowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "allow [dir]",
		Short: "Verzeichnis für Auto-Load erlauben",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented
		},
	}
}

func newDenyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "deny [dir]",
		Short: "Verzeichnis für Auto-Load sperren",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented
		},
	}
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Globale bwenv-Konfiguration verwalten",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "server <url>",
		Short: "Vaultwarden-/Bitwarden-Endpunkt setzen",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented
		},
	})
	return cmd
}
