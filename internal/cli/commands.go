package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/rleusmann/bwenv/internal/config"
	"github.com/rleusmann/bwenv/internal/format"
	"github.com/rleusmann/bwenv/internal/resolver"
	"github.com/rleusmann/bwenv/internal/runner"
	"github.com/rleusmann/bwenv/internal/trust"
)

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run -- <cmd> [args...]",
		Short: "Secrets injizieren und Befehl starten",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := resolveProject(cmd.Context(), true)
			if err != nil {
				return err
			}
			code, err := runner.Run(cmd.Context(), args, runner.Options{
				Env:    env,
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}
			if code != 0 {
				return exitCodeError(code)
			}
			return nil
		},
	}
}

func newExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "export",
		Aliases: []string{"sh"},
		Short:   "Shell-Export-Statements ausgeben (für eval \"$(bwenv sh)\")",
		RunE: func(cmd *cobra.Command, args []string) error {
			silent, _ := cmd.Flags().GetBool("silent")
			hook, _ := cmd.Flags().GetBool("hook")
			global, _ := cmd.Flags().GetBool("global")
			timeout, _ := cmd.Flags().GetDuration("timeout")
			formatName, _ := cmd.Flags().GetString("format")
			if formatName != "sh" && formatName != "zsh" {
				return fmt.Errorf("unbekanntes format %q (unterstützt: sh, zsh)", formatName)
			}

			ctx := cmd.Context()
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}

			// Hook-Modus: Load/Unload-Diff, Failsafe immer aktiv.
			if hook {
				cmd.Print(hookOutput(ctx))
				return nil
			}

			// Failsafe (Plan §3.4): im Silent-Modus führt *jeder* Fehler zu
			// leerer Ausgabe und Exit 0 — die Shell darf nie blockieren.
			fail := func(err error) error {
				if silent {
					return nil
				}
				return err
			}

			var env resolver.EnvMap
			var err error
			if global {
				env, err = resolveGlobal(ctx, !silent)
			} else {
				// Trust gilt für den Auto-Load-Pfad: export lädt nur
				// freigegebene Verzeichnisse (direnv-Stil).
				cwd, cwdErr := os.Getwd()
				if cwdErr != nil {
					return fail(cwdErr)
				}
				if cfgPath, findErr := config.Find(cwd); findErr == nil {
					dir := filepath.Dir(cfgPath)
					if ok, _ := trust.IsAllowed(dir); !ok {
						return fail(fmt.Errorf("%s ist nicht freigegeben — mit `bwenv allow` erlauben", dir))
					}
				}
				env, err = resolveProject(ctx, !silent)
			}
			if err != nil {
				return fail(err)
			}
			out, err := format.ShellExports(env)
			if err != nil {
				return fail(err)
			}
			cmd.Print(out)
			return nil
		},
	}
	cmd.Flags().String("format", "sh", "Ausgabeformat: sh|zsh")
	cmd.Flags().Bool("silent", false, "bei Fehlern still bleiben (exit 0)")
	cmd.Flags().Bool("hook", false, "Hook-Modus: Load/Unload-Diff für die Shell-Integration")
	cmd.Flags().Bool("global", false, "global:-Sektion der globalen Config laden")
	cmd.Flags().Duration("timeout", 0, "harter Timeout, z. B. 300ms")
	return cmd
}

func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Geladene Variablennamen anzeigen (Werte maskiert)",
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := resolveProject(cmd.Context(), true)
			if err != nil {
				return err
			}
			cmd.Print(format.Masked(env))
			return nil
		},
	}
}

func newUnlockCmd() *cobra.Command {
	return newUnlockCmdImpl()
}

func newLockCmd() *cobra.Command {
	return newLockCmdImpl()
}

func newAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Agent-Lifecycle verwalten",
	}
	cmd.AddCommand(newAgentRunCmd(), newAgentStatusCmd(), newAgentStopCmd())
	return cmd
}

func newHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "hook <shell>",
		Short:     "Shell-Integrations-Snippet ausgeben (z. B. für .zshrc)",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"zsh"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != "zsh" {
				return fmt.Errorf("shell %q nicht unterstützt (aktuell: zsh)", args[0])
			}
			cmd.Print(zshHookSnippet)
			return nil
		},
	}
}

// trustTargetDir liefert das Argument-Verzeichnis oder das aktuelle.
func trustTargetDir(args []string) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}
	return os.Getwd()
}

func newAllowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "allow [dir]",
		Short: "Verzeichnis für Auto-Load erlauben",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := trustTargetDir(args)
			if err != nil {
				return err
			}
			if err := trust.Allow(dir); err != nil {
				return err
			}
			cmd.Printf("erlaubt: %s\n", dir)
			return nil
		},
	}
}

func newDenyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "deny [dir]",
		Short: "Verzeichnis für Auto-Load sperren",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := trustTargetDir(args)
			if err != nil {
				return err
			}
			if err := trust.Deny(dir); err != nil {
				return err
			}
			cmd.Printf("gesperrt: %s\n", dir)
			return nil
		},
	}
}

func newSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Vault-Stand vom Server holen (über den laufenden Agent)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, st, ok := tryAgent(cmd.Context())
			if !ok || st != "unlocked" {
				return fmt.Errorf("sync braucht einen entsperrten Agent — zuerst `bwenv unlock` ausführen")
			}
			if err := c.Sync(cmd.Context()); err != nil {
				return err
			}
			cmd.Println("synchronisiert")
			return nil
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
		Short: "Vaultwarden-/Bitwarden-Endpunkt setzen (Passthrough an `bw config server`)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bw := exec.CommandContext(cmd.Context(), "bw", "config", "server", args[0]) //nolint:gosec // G204: URL kommt bewusst vom User (CLI-Arg)
			bw.Stdout = cmd.OutOrStdout()
			bw.Stderr = cmd.ErrOrStderr()
			return bw.Run()
		},
	})
	return cmd
}
