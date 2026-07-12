package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/rleusmann/bwenv/internal/config"
	"github.com/rleusmann/bwenv/internal/format"
	"github.com/rleusmann/bwenv/internal/provider"
	"github.com/rleusmann/bwenv/internal/resolver"
)

// exitCodeError transportiert den Exit-Code eines Kind-Prozesses durch cobra.
type exitCodeError int

func (e exitCodeError) Error() string { return fmt.Sprintf("exit code %d", int(e)) }

// openProvider öffnet das Secret-Backend: startet bw serve, prüft den
// Vault-Status und entsperrt bei Bedarf interaktiv. In Tests austauschbar.
var openProvider = func(ctx context.Context, cfg *config.Config, allowPrompt bool) (provider.Provider, func(), error) {
	s, err := provider.StartServe(ctx, provider.ServeOptions{})
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = s.Close() }
	c := s.Client()

	st, err := c.Status(ctx)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	switch st {
	case provider.StatusUnlocked:
	case provider.StatusLocked:
		if !allowPrompt {
			cleanup()
			return nil, nil, errors.New("vault ist gesperrt — `bwenv unlock` ausführen")
		}
		pw, err := readPassword("Master-Passwort: ")
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		if err := c.Unlock(ctx, pw); err != nil {
			cleanup()
			return nil, nil, err
		}
	default:
		cleanup()
		return nil, nil, errors.New("nicht eingeloggt — zuerst `bw login` (ggf. `bw config server <url>`) ausführen")
	}
	return c, cleanup, nil
}

// readPassword liest das Master-Passwort ohne Echo vom Terminal.
var readPassword = func(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("kein Terminal für den Passwort-Prompt verfügbar")
	}
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// resolveProject lädt die nächstgelegene bwenv.yaml und löst deren
// secrets-Einträge auf. Fehlertexte sind um bekannte Werte redigiert.
func resolveProject(ctx context.Context, allowPrompt bool) (resolver.EnvMap, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	cfgPath, err := config.Find(cwd)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}

	p, cleanup, err := openProvider(ctx, cfg, allowPrompt)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	env, err := resolver.Resolve(ctx, p, cfg.Secrets)
	if err != nil {
		return nil, redactError(env, err)
	}
	return env, nil
}

// redactError ersetzt bereits aufgelöste Secret-Werte im Fehlertext.
func redactError(env resolver.EnvMap, err error) error {
	values := make([]string, 0, len(env))
	for _, v := range env {
		values = append(values, v)
	}
	return errors.New(format.NewRedactor(values).Redact(err.Error()))
}

// runRoot führt den Command-Tree aus und mappt Fehler auf Exit-Codes.
func runRoot(root *cobra.Command) int {
	err := root.Execute()
	if err == nil {
		return 0
	}
	var ec exitCodeError
	if errors.As(err, &ec) {
		return int(ec)
	}
	_, _ = fmt.Fprintln(root.ErrOrStderr(), "bwenv:", err)
	return 1
}
