package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/rleusmann/bwenv/internal/agent"
	"github.com/rleusmann/bwenv/internal/credstore"
	"github.com/rleusmann/bwenv/internal/provider"
)

// tryAgent liefert einen Client + Status, wenn ein Agent auf dem Socket antwortet.
func tryAgent(ctx context.Context) (*agent.Client, string, bool) {
	path, err := agent.SocketPath()
	if err != nil {
		return nil, "", false
	}
	c := agent.NewAgentClient(path)
	probe, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	st, err := c.Status(probe)
	if err != nil {
		return nil, "", false
	}
	return c, st, true
}

// openBwBackend öffnet das echte Backend für den Agent: bw serve starten
// und mit dem Master-Passwort entsperren.
func openBwBackend(ctx context.Context, password string) (provider.Provider, func(), error) {
	s, err := provider.StartServe(ctx, provider.ServeOptions{})
	if err != nil {
		return nil, nil, err
	}
	c := s.Client()
	st, err := c.Status(ctx)
	if err != nil {
		_ = s.Close()
		return nil, nil, err
	}
	switch st {
	case provider.StatusUnlocked:
	case provider.StatusLocked:
		if err := c.Unlock(ctx, password); err != nil {
			_ = s.Close()
			return nil, nil, err
		}
	default:
		_ = s.Close()
		return nil, nil, errors.New("nicht eingeloggt — zuerst `bw login` (ggf. `bw config server <url>`) ausführen")
	}
	return c, func() { _ = s.Close() }, nil
}

// spawnAgent startet `bwenv agent run` als losgelösten Hintergrundprozess.
func spawnAgent() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "agent", "run") //nolint:gosec // G204: exe ist os.Executable(), kein Fremd-Input
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// awaitAgent wartet, bis der Agent auf dem Socket antwortet.
func awaitAgent(ctx context.Context, timeout time.Duration) (*agent.Client, string, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if c, st, ok := tryAgent(ctx); ok {
			return c, st, true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, "", false
}

func newAgentRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Agent im Vordergrund laufen lassen (wird von `bwenv unlock` gestartet)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ttl, _ := cmd.Flags().GetDuration("ttl")

			// Keine Core-Dumps, sobald Secrets im Speicher liegen können.
			_ = syscall.Setrlimit(syscall.RLIMIT_CORE, &syscall.Rlimit{Cur: 0, Max: 0})

			path, err := agent.SocketPath()
			if err != nil {
				return err
			}
			l, err := agent.Listen(path)
			if err != nil {
				return err
			}
			srv := agent.NewServer(openBwBackend, ttl)

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				select {
				case <-sigCh:
					_ = srv.Shutdown()
				case <-srv.Done():
				}
			}()

			err = srv.Serve(l)
			_ = os.Remove(path)
			return err
		},
	}
	cmd.Flags().Duration("ttl", 15*time.Minute, "Idle-TTL bis zum Auto-Lock (0 = aus)")
	return cmd
}

func newAgentStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Agent-Status anzeigen",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, st, ok := tryAgent(cmd.Context()); ok {
				cmd.Println(st)
			} else {
				cmd.Println("kein agent")
			}
			return nil
		},
	}
}

func newAgentStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Agent beenden",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, ok := tryAgent(cmd.Context())
			if !ok {
				cmd.Println("kein agent")
				return nil
			}
			return c.StopAgent(cmd.Context())
		},
	}
}

// newCredStore liefert den Touch-ID-Credstore; in Tests austauschbar.
var newCredStore = credstore.New

func newUnlockCmdImpl() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unlock",
		Short: "Session entsperren (Master-Passwort-Prompt oder Touch ID)",
		RunE: func(cmd *cobra.Command, args []string) error {
			enroll, _ := cmd.Flags().GetBool("enroll-touchid")
			ctx := cmd.Context()

			c, st, ok := tryAgent(ctx)
			if !ok {
				if err := spawnAgent(); err != nil {
					return fmt.Errorf("agent starten: %w", err)
				}
				c, st, ok = awaitAgent(ctx, 5*time.Second)
				if !ok {
					return errors.New("agent wurde gestartet, antwortet aber nicht")
				}
			}

			if enroll {
				store := newCredStore()
				if !store.Available() {
					return errors.New("diese Plattform unterstützt kein Touch ID")
				}
				pw, err := readPassword("Master-Passwort: ")
				if err != nil {
					return err
				}
				// Verifikation vor dem Enrollment: sperren und mit dem
				// eingegebenen Passwort frisch entsperren — ein falsches
				// Passwort landet nie in der Keychain.
				if err := c.Lock(ctx); err != nil {
					return err
				}
				if err := c.Unlock(ctx, pw); err != nil {
					return fmt.Errorf("passwort-verifikation fehlgeschlagen: %w", err)
				}
				if err := store.Enroll(pw); err != nil {
					return err
				}
				cmd.Println("Touch-ID-Unlock eingerichtet — `bwenv unlock` nutzt ab jetzt Touch ID")
				return nil
			}

			if st == "unlocked" {
				cmd.Println("bereits entsperrt")
				return nil
			}

			// Touch ID bevorzugen, wenn eingerichtet; jeder Fehler fällt
			// sauber auf den Passwort-Prompt zurück.
			store := newCredStore()
			if store.Available() && store.Enrolled() {
				if pw, err := store.Retrieve("bwenv entsperren"); err == nil {
					if err := c.Unlock(ctx, pw); err == nil {
						cmd.Println("entsperrt (Touch ID)")
						return nil
					}
				}
			}

			pw, err := readPassword("Master-Passwort: ")
			if err != nil {
				return err
			}
			if err := c.Unlock(ctx, pw); err != nil {
				return err
			}
			cmd.Println("entsperrt")
			return nil
		},
	}
	cmd.Flags().Bool("enroll-touchid", false, "Touch-ID-Unlock einrichten (macOS)")
	return cmd
}

func newLockCmdImpl() *cobra.Command {
	return &cobra.Command{
		Use:   "lock",
		Short: "Session sofort sperren",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, ok := tryAgent(cmd.Context())
			if !ok {
				cmd.Println("kein agent — nichts zu sperren")
				return nil
			}
			return c.Lock(cmd.Context())
		},
	}
}
