package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/rleusmann/bwenv/internal/config"
)

// Beide Templates sind valide und inert: sie laden nichts, bis die
// Platzhalter-Beispiele einkommentiert und angepasst werden.
const localConfigTemplate = `# bwenv.yaml — Secret-Referenzen für dieses Projekt.
# Enthält nur Item-Referenzen, niemals Werte — gefahrlos commit-bar.
# Auto-Load im Terminal: einmal ` + "`bwenv allow`" + ` in diesem Verzeichnis.
#
# Eintrag aktivieren: Beispiel einkommentieren und "secrets: []" entfernen.
#
# secrets:
#   # Einzelnes Secret: Item-Name (oder item_id:) + Feld.
#   - env: DATABASE_URL
#     item: "mein-item"            # exakter Item-Name im Vault
#     field: password              # uri | username | password | <custom-field>
#
#   # Bulk: jedes Custom-Field aller Items eines Folders → gleichnamige Env-Var.
#   - from:
#       folder: "dev-env"
#     strategy: field-name-as-env

version: 1
secrets: []
`

const globalConfigTemplate = `# Globale bwenv-Config — Secrets, die in JEDER Shell verfügbar sein sollen
# (einmal beim Shell-Init geladen, nicht pro Verzeichnis).
# Enthält nur Item-Referenzen, niemals Werte.
#
# Eintrag aktivieren: Beispiel einkommentieren und "global: []" entfernen.
#
# global:
#   - env: GITHUB_TOKEN
#     item: "mein-item"            # exakter Item-Name im Vault (oder item_id:)
#     field: password              # uri | username | password | <custom-field>

version: 1
global: []
`

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Config mit Platzhaltern anlegen (./bwenv.yaml oder --global)",
		RunE: func(cmd *cobra.Command, args []string) error {
			global, _ := cmd.Flags().GetBool("global")

			var path, template string
			if global {
				p, err := config.GlobalPath()
				if err != nil {
					return err
				}
				path, template = p, globalConfigTemplate
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					return err
				}
			} else {
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				path, template = filepath.Join(cwd, config.FileName), localConfigTemplate
			}

			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s existiert bereits — nichts überschrieben", path)
			}
			if err := os.WriteFile(path, []byte(template), 0o600); err != nil {
				return err
			}

			cmd.Printf("angelegt: %s\n", path)
			if global {
				cmd.Println("Einträge unter global: einkommentieren — geladen wird beim Shell-Init (Hook) bzw. via `bwenv export --global`.")
			} else {
				cmd.Println("Einträge unter secrets: einkommentieren; Auto-Load mit `bwenv allow` freigeben.")
			}
			return nil
		},
	}
	cmd.Flags().Bool("global", false, "globale Config (~/.config/bwenv/config.yaml) statt ./bwenv.yaml anlegen")
	return cmd
}
