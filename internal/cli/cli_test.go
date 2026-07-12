package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/rleusmann/bwenv/internal/config"
	"github.com/rleusmann/bwenv/internal/provider"
)

type fakeProv struct {
	values map[string]string // "<item>/<field>" → Wert
}

func (f *fakeProv) Fetch(_ context.Context, refs []provider.SecretRef) (map[string]provider.Secret, error) {
	out := map[string]provider.Secret{}
	for _, ref := range refs {
		item := ref.Item
		if ref.ItemID != "" {
			item = ref.ItemID
		}
		val, ok := f.values[item+"/"+ref.Field]
		if !ok {
			return nil, fmt.Errorf("nicht gefunden: %s/%s", item, ref.Field)
		}
		out[ref.Env] = provider.Secret{Value: val}
	}
	return out, nil
}

func (f *fakeProv) HealthCheck(context.Context) error { return nil }

// withFakeProvider ersetzt die Provider-Factory für die Dauer des Tests.
func withFakeProvider(t *testing.T, p provider.Provider, err error) {
	t.Helper()
	orig := openProvider
	openProvider = func(context.Context, *config.Config, bool) (provider.Provider, func(), error) {
		if err != nil {
			return nil, nil, err
		}
		return p, func() {}, nil
	}
	t.Cleanup(func() { openProvider = orig })
}

// inProjectDir legt eine bwenv.yaml in ein Temp-Verzeichnis und wechselt hinein.
// Der Agent-Socket zeigt dabei auf einen toten Pfad, damit Tests nie einen
// echten Agent des Users treffen.
func inProjectDir(t *testing.T, yaml string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bwenv.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("BWENV_AGENT_SOCKET", filepath.Join(dir, "kein-agent.sock"))
}

const testYAML = `
version: 1
secrets:
  - env: FOO
    item: "app"
    field: password
  - env: BAR
    item: "app"
    field: username
`

func execute(args ...string) (stdout, stderr string, code int) {
	root := newRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	code = runRoot(root)
	return out.String(), errBuf.String(), code
}

func TestExportPrintsExports(t *testing.T) {
	inProjectDir(t, testYAML)
	withFakeProvider(t, &fakeProv{values: map[string]string{
		"app/password": "geheim",
		"app/username": "user1",
	}}, nil)

	out, _, code := execute("export")
	if code != 0 {
		t.Fatalf("exit = %d, out=%q", code, out)
	}
	want := "export BAR='user1'\nexport FOO='geheim'\n"
	if out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
}

func TestExportSilentFailsafeOnProviderError(t *testing.T) {
	inProjectDir(t, testYAML)
	withFakeProvider(t, nil, errors.New("vault offline"))

	out, _, code := execute("export", "--silent")
	if code != 0 {
		t.Fatalf("Failsafe verletzt: exit = %d (muss 0 sein)", code)
	}
	if out != "" {
		t.Errorf("Failsafe verletzt: Ausgabe %q (muss leer sein)", out)
	}
}

func TestExportSilentFailsafeWithoutConfig(t *testing.T) {
	t.Chdir(t.TempDir()) // keine bwenv.yaml

	out, _, code := execute("export", "--silent")
	if code != 0 || out != "" {
		t.Fatalf("Failsafe verletzt: exit=%d out=%q", code, out)
	}
}

func TestExportLoudErrorWithoutSilent(t *testing.T) {
	inProjectDir(t, testYAML)
	withFakeProvider(t, nil, errors.New("vault offline"))

	_, stderr, code := execute("export")
	if code == 0 {
		t.Fatal("ohne --silent muss ein Fehler gemeldet werden")
	}
	if stderr == "" {
		t.Error("Fehlermeldung auf stderr erwartet")
	}
}

func TestShowMasksValues(t *testing.T) {
	inProjectDir(t, testYAML)
	withFakeProvider(t, &fakeProv{values: map[string]string{
		"app/password": "geheim",
		"app/username": "user1",
	}}, nil)

	out, _, code := execute("show")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if bytes.Contains([]byte(out), []byte("geheim")) || bytes.Contains([]byte(out), []byte("user1")) {
		t.Fatalf("show enthält Klartext: %q", out)
	}
	if !bytes.Contains([]byte(out), []byte("FOO")) || !bytes.Contains([]byte(out), []byte("BAR")) {
		t.Errorf("show enthält Namen nicht: %q", out)
	}
}

func TestRunInjectsEnvAndPassesExitCode(t *testing.T) {
	inProjectDir(t, testYAML)
	withFakeProvider(t, &fakeProv{values: map[string]string{
		"app/password": "geheim",
		"app/username": "user1",
	}}, nil)

	out, _, code := execute("run", "--", "sh", "-c", `printf '%s' "$FOO"`)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if out != "geheim" {
		t.Errorf("Kind sah FOO=%q, want geheim", out)
	}

	_, _, code = execute("run", "--", "sh", "-c", "exit 5")
	if code != 5 {
		t.Errorf("Exit-Code nicht durchgereicht: %d, want 5", code)
	}
}

func TestErrorsAreRedacted(t *testing.T) {
	inProjectDir(t, `
version: 1
secrets:
  - env: OK_VAR
    item: "app"
    field: password
  - env: FEHLT
    item: "missing"
    field: password
`)
	withFakeProvider(t, &fakeProv{values: map[string]string{ //nolint:gosec // G101: Test-Fixture, kein echtes Credential
		"app/password": "super-geheimer-wert",
	}}, nil)

	_, stderr, code := execute("show")
	if code == 0 {
		t.Fatal("Fehler erwartet (Item fehlt)")
	}
	if bytes.Contains([]byte(stderr), []byte("super-geheimer-wert")) {
		t.Fatalf("stderr enthält Klartext-Secret: %q", stderr)
	}
}
