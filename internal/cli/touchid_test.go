package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rleusmann/bwenv/internal/credstore"
)

// mockStore ist ein credstore.Store für Tests.
type mockStore struct {
	available bool
	secret    string // "" = nicht enrolled
	enrollErr error
	retErr    error

	enrolledWith string // was Enroll bekam
	retrieved    bool
	erased       bool
}

func (m *mockStore) Available() bool { return m.available }
func (m *mockStore) Enrolled() bool  { return m.secret != "" }

func (m *mockStore) Enroll(secret string) error {
	if m.enrollErr != nil {
		return m.enrollErr
	}
	m.enrolledWith = secret
	m.secret = secret
	return nil
}

func (m *mockStore) Retrieve(reason string) (string, error) {
	m.retrieved = true
	if m.retErr != nil {
		return "", m.retErr
	}
	if m.secret == "" {
		return "", credstore.ErrNotEnrolled
	}
	return m.secret, nil
}

func (m *mockStore) Erase() error {
	m.erased = true
	m.secret = ""
	return nil
}

func withMockStore(t *testing.T, m *mockStore) {
	t.Helper()
	orig := newCredStore
	newCredStore = func() credstore.Store { return m }
	t.Cleanup(func() { newCredStore = orig })
}

// readPassword darf im Touch-ID-Pfad nie aufgerufen werden.
func forbidPasswordPrompt(t *testing.T) {
	t.Helper()
	orig := readPassword
	readPassword = func(string) (string, error) {
		t.Error("readPassword wurde aufgerufen, obwohl Touch ID greifen muss")
		return "", errors.New("kein Prompt erlaubt")
	}
	t.Cleanup(func() { readPassword = orig })
}

func TestEnrollTouchIDVerifiesAndStoresPassword(t *testing.T) {
	inProjectDir(t, testYAML)
	ag := startCLITestAgent(t)
	withPassword(t, "correct horse")
	m := &mockStore{available: true}
	withMockStore(t, m)

	_, stderr, code := execute("unlock", "--enroll-touchid")
	if code != 0 {
		t.Fatalf("enroll: exit=%d stderr=%q", code, stderr)
	}
	if m.enrolledWith != "correct horse" {
		t.Errorf("Enroll bekam %q, want das verifizierte Master-Passwort", m.enrolledWith)
	}
	st, _ := ag.Status(context.Background())
	if st != "unlocked" {
		t.Errorf("Agent nach Enrollment nicht entsperrt: %q", st)
	}
}

func TestEnrollTouchIDWrongPasswordNotStored(t *testing.T) {
	inProjectDir(t, testYAML)
	startCLITestAgent(t)
	withPassword(t, "falsch")
	m := &mockStore{available: true}
	withMockStore(t, m)

	_, _, code := execute("unlock", "--enroll-touchid")
	if code == 0 {
		t.Fatal("Enrollment mit falschem Passwort muss fehlschlagen")
	}
	if m.enrolledWith != "" {
		t.Error("falsches Passwort darf nicht in der Keychain landen")
	}
}

func TestEnrollTouchIDUnavailablePlatform(t *testing.T) {
	inProjectDir(t, testYAML)
	startCLITestAgent(t)
	m := &mockStore{available: false}
	withMockStore(t, m)

	_, stderr, code := execute("unlock", "--enroll-touchid")
	if code == 0 {
		t.Fatal("Enrollment ohne Touch-ID-Support muss fehlschlagen")
	}
	if !strings.Contains(stderr, "Touch ID") {
		t.Errorf("Fehlermeldung ohne Touch-ID-Hinweis: %q", stderr)
	}
}

func TestUnlockUsesTouchIDWhenEnrolled(t *testing.T) {
	inProjectDir(t, testYAML)
	ag := startCLITestAgent(t)
	forbidPasswordPrompt(t)
	m := &mockStore{available: true, secret: "correct horse"}
	withMockStore(t, m)

	_, stderr, code := execute("unlock")
	if code != 0 {
		t.Fatalf("unlock: exit=%d stderr=%q", code, stderr)
	}
	if !m.retrieved {
		t.Error("Retrieve (Touch ID) wurde nicht verwendet")
	}
	st, _ := ag.Status(context.Background())
	if st != "unlocked" {
		t.Errorf("Agent-Status = %q, want unlocked", st)
	}
}

func TestUnlockFallsBackToPromptWhenTouchIDFails(t *testing.T) {
	inProjectDir(t, testYAML)
	ag := startCLITestAgent(t)
	withPassword(t, "correct horse")
	m := &mockStore{available: true, secret: "x", retErr: errors.New("Touch ID abgebrochen")}
	withMockStore(t, m)

	_, stderr, code := execute("unlock")
	if code != 0 {
		t.Fatalf("unlock: exit=%d stderr=%q", code, stderr)
	}
	st, _ := ag.Status(context.Background())
	if st != "unlocked" {
		t.Errorf("Fallback-Prompt hat nicht entsperrt: %q", st)
	}
}

func TestUnlockIgnoresStoreWhenNotEnrolled(t *testing.T) {
	inProjectDir(t, testYAML)
	ag := startCLITestAgent(t)
	withPassword(t, "correct horse")
	m := &mockStore{available: true} // nicht enrolled
	withMockStore(t, m)

	_, _, code := execute("unlock")
	if code != 0 {
		t.Fatal("unlock ohne Enrollment muss per Prompt funktionieren")
	}
	st, _ := ag.Status(context.Background())
	if st != "unlocked" {
		t.Errorf("Agent-Status = %q", st)
	}
}
