//go:build integration

package integration

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rleusmann/bwenv/internal/agent"
)

const (
	testEmail    = "it@bwenv.test"
	testPassword = "bwenv-integration-master-pw"
)

// startVaultwarden startet den Container mit selbstsigniertem TLS
// (die bw-CLI verweigert http://) und liefert URL + Zertifikatspfad.
// Mit VAULTWARDEN_URL/VAULTWARDEN_CERT lässt sich eine externe Instanz
// vorgeben.
func startVaultwarden(t *testing.T) (url, certPath string) {
	t.Helper()
	if url := os.Getenv("VAULTWARDEN_URL"); url != "" {
		return url, os.Getenv("VAULTWARDEN_CERT")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("weder VAULTWARDEN_URL noch docker verfügbar")
	}

	// /tmp ist bei Docker Desktop standardmäßig freigegeben (Bind-Mount).
	sslDir, err := os.MkdirTemp("/tmp", "bwssl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sslDir) })
	certPath = genSelfSignedCert(t, sslDir)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	out, err := exec.Command("docker", "run", "-d", "--rm",
		"-p", fmt.Sprintf("127.0.0.1:%d:80", port),
		"-v", sslDir+":/ssl:ro",
		"-e", "SIGNUPS_ALLOWED=true",
		"-e", "I_REALLY_WANT_VOLATILE_STORAGE=true", // ephemerer Test-Container, bewusst ohne Volume
		"-e", `ROCKET_TLS={certs="/ssl/cert.pem",key="/ssl/key.pem"}`,
		"vaultwarden/server:latest").Output()
	if err != nil {
		t.Fatalf("vaultwarden-Container starten: %v", err)
	}
	containerID := strings.TrimSpace(string(out))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", containerID).Run() })

	return fmt.Sprintf("https://127.0.0.1:%d", port), certPath
}

// insecureClient akzeptiert das selbstsignierte Test-Zertifikat.
var insecureClient = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // nur Test
	},
}

func waitAlive(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := insecureClient.Get(url + "/alive")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("vaultwarden wurde nicht rechtzeitig erreichbar")
}

// bwCmd führt die bw-CLI mit isoliertem Appdata-Verzeichnis aus.
func bwCmd(t *testing.T, appdata string, extraEnv []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("bw", append(args, "--nointeraction")...)
	cmd.Env = append(os.Environ(), "BITWARDENCLI_APPDATA_DIR="+appdata)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bw %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func buildBwenv(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "bwenv")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/rleusmann/bwenv/cmd/bwenv")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bwenv bauen: %v\n%s", err, out)
	}
	return bin
}

func TestEndToEndAgainstVaultwarden(t *testing.T) {
	if _, err := exec.LookPath("bw"); err != nil {
		t.Skip("bw-CLI nicht installiert")
	}

	url, certPath := startVaultwarden(t)
	waitAlive(t, url)

	// Account per Client-Krypto registrieren; bw login verifiziert sie E2E.
	if err := registerAccount(url, testEmail, testPassword); err != nil {
		t.Fatalf("registerAccount: %v", err)
	}

	// bw (Node) vertraut dem Test-Zertifikat über NODE_EXTRA_CA_CERTS.
	caEnv := []string{"NODE_EXTRA_CA_CERTS=" + certPath}

	appdata := t.TempDir()
	bwCmd(t, appdata, caEnv, "config", "server", url)
	bwCmd(t, appdata, caEnv, "login", testEmail, testPassword)
	session := bwCmd(t, appdata, caEnv, "unlock", testPassword, "--raw")

	// Test-Item seeden: Login mit Passwort, URI und Custom-Field.
	itemJSON := `{"type":1,"name":"prod/api","notes":null,` +
		`"login":{"username":"svc-user","password":"s3cret-db","uris":[{"match":null,"uri":"postgres://db.example.com"}]},` +
		`"fields":[{"name":"API_KEY","value":"xyz-789","type":0}]}`
	bwCmd(t, appdata, append(caEnv, "BW_SESSION="+session), "create", "item",
		base64.StdEncoding.EncodeToString([]byte(itemJSON)))
	bwCmd(t, appdata, append(caEnv, "BW_SESSION="+session), "lock")

	// bwenv-Binary + Agent mit isoliertem Socket und bw-Appdata.
	bin := buildBwenv(t)
	sockDir, err := os.MkdirTemp("/tmp", "bwe")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "agent.sock")

	agentEnv := append(os.Environ(),
		"BWENV_AGENT_SOCKET="+sock,
		"BITWARDENCLI_APPDATA_DIR="+appdata,
		"NODE_EXTRA_CA_CERTS="+certPath, // für das vom Agent gestartete bw serve
	)
	agentCmd := exec.Command(bin, "agent", "run", "--ttl", "0")
	agentCmd.Env = agentEnv
	if err := agentCmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = agentCmd.Process.Kill()
		_, _ = agentCmd.Process.Wait()
	})

	// Agent entsperren (über den Socket, wie es `bwenv unlock` täte).
	client := agent.NewAgentClient(sock)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	deadline := time.Now().Add(10 * time.Second)
	for !client.Available(ctx) {
		if time.Now().After(deadline) {
			t.Fatal("agent antwortet nicht")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := client.Unlock(ctx, testPassword); err != nil {
		t.Fatalf("agent unlock gegen vaultwarden: %v", err)
	}

	// Projekt mit bwenv.yaml + Trust, dann export über den Agent.
	project := t.TempDir()
	yaml := `version: 1
secrets:
  - env: DATABASE_URL
    item: "prod/api"
    field: uri
  - env: DB_PASS
    item: "prod/api"
    field: password
  - env: API_KEY
    item: "prod/api"
    field: API_KEY
`
	if err := os.WriteFile(filepath.Join(project, "bwenv.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	xdg := t.TempDir()
	cliEnv := append(agentEnv, "XDG_CONFIG_HOME="+xdg)

	allow := exec.Command(bin, "allow")
	allow.Dir, allow.Env = project, cliEnv
	if out, err := allow.CombinedOutput(); err != nil {
		t.Fatalf("bwenv allow: %v\n%s", err, out)
	}

	export := exec.Command(bin, "export")
	export.Dir, export.Env = project, cliEnv
	out, err := export.CombinedOutput()
	if err != nil {
		t.Fatalf("bwenv export: %v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{
		"export API_KEY='xyz-789'",
		"export DATABASE_URL='postgres://db.example.com'",
		"export DB_PASS='s3cret-db'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("export-Ausgabe ohne %q:\n%s", want, got)
		}
	}

	// run: Env-Injection + Exit-Code-Passthrough gegen echtes Backend.
	run := exec.Command(bin, "run", "--", "sh", "-c", `printf '%s' "$DB_PASS"`)
	run.Dir, run.Env = project, cliEnv
	out, err = run.CombinedOutput()
	if err != nil {
		t.Fatalf("bwenv run: %v\n%s", err, out)
	}
	if string(out) != "s3cret-db" {
		t.Errorf("bwenv run lieferte %q, want s3cret-db", out)
	}

	// lock → export --silent muss leise leer bleiben (Failsafe).
	if err := client.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	silent := exec.Command(bin, "export", "--silent")
	silent.Dir, silent.Env = project, cliEnv
	out, err = silent.Output()
	if err != nil {
		t.Fatalf("export --silent nach lock: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("Failsafe verletzt: %q", out)
	}
}
