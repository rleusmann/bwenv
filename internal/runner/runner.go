// Package runner startet Subprozesse mit injizierten Umgebungsvariablen.
//
// Designentscheidung (Plan §11): Default ist exec.Command mit
// Signal-Forwarding statt syscall.Exec — portabler und nötig, sobald der
// Agent Teardown-Arbeit nach dem Kind-Exit übernimmt. Ein späterer
// --exec-Modus (syscall.Exec, kein bwenv-Parent in ps) bleibt möglich.
package runner

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
)

// Options konfiguriert einen Lauf.
type Options struct {
	Env    map[string]string // zusätzlich zur geerbten Umgebung; überschreibt sie
	Stdin  io.Reader         // default os.Stdin
	Stdout io.Writer         // default os.Stdout
	Stderr io.Writer         // default os.Stderr
	// Signals ersetzt im Test die per signal.Notify abonnierten Signale.
	Signals <-chan os.Signal
}

// Run führt argv mit injizierter Env aus, leitet SIGINT/SIGTERM ans Kind
// weiter und liefert dessen Exit-Code.
func Run(ctx context.Context, argv []string, opts Options) (int, error) {
	if len(argv) == 0 {
		return 0, errors.New("kein Kommando angegeben")
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // G204: argv kommt bewusst vom Aufrufer (bwenv run -- <cmd>)
	cmd.Env = mergedEnv(opts.Env)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = opts.Stdin, opts.Stdout, opts.Stderr
	if cmd.Stdin == nil {
		cmd.Stdin = os.Stdin
	}
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Start(); err != nil {
		return 0, err
	}

	sigCh := opts.Signals
	var stop func()
	if sigCh == nil {
		ch := make(chan os.Signal, 2)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		stop = func() { signal.Stop(ch) }
		sigCh = ch
	}

	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig, ok := <-sigCh:
				if !ok {
					return
				}
				_ = cmd.Process.Signal(sig)
			case <-done:
				return
			}
		}
	}()

	err := cmd.Wait()
	close(done)
	if stop != nil {
		stop()
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	if err != nil {
		return 0, err
	}
	return 0, nil
}

// mergedEnv legt extra über die geerbte Umgebung (extra gewinnt).
func mergedEnv(extra map[string]string) []string {
	if len(extra) == 0 {
		return os.Environ()
	}
	env := os.Environ()
	out := make([]string, 0, len(env)+len(extra))
	for _, kv := range env {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		if _, shadowed := extra[key]; !shadowed {
			out = append(out, kv)
		}
	}
	for k, v := range extra {
		out = append(out, k+"="+v)
	}
	return out
}
