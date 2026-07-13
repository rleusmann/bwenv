// Package credstore speichert das opt-in Touch-ID-Unlock-Credential
// (das Master-Passwort) in der macOS-Keychain, gated durch eine
// biometrische Prüfung (Plan §5.2).
//
// Abweichung vom Plan (dokumentiert): Secure-Enclave-gated Keychain-Items
// (kSecUseDataProtectionKeychain + SecAccessControl) verlangen ein mit
// Entitlements signiertes Binary. Für ein unsigniertes CLI verwendet die
// darwin-Implementierung stattdessen ein LocalAuthentication-Gate
// (Touch-ID-Prüfung im Prozess) vor dem Zugriff auf ein Item in der
// Login-Keychain. Signierte Builds können später auf SEP-Gating upgraden.
package credstore

import "errors"

// ErrNotEnrolled: es wurde noch kein Credential hinterlegt.
var ErrNotEnrolled = errors.New("kein Touch-ID-Credential hinterlegt — `bwenv unlock --enroll-touchid`")

// ErrUnavailable: Plattform ohne Touch-ID-Support (z. B. Linux, CGO aus).
var ErrUnavailable = errors.New("diese Plattform unterstützt kein Touch ID")

// Store verwaltet das Unlock-Credential.
type Store interface {
	// Available meldet, ob Touch ID auf dieser Plattform nutzbar ist.
	Available() bool
	// Enrolled meldet, ob ein Credential hinterlegt ist.
	Enrolled() bool
	// Enroll hinterlegt das Credential (löst eine biometrische Prüfung aus).
	Enroll(secret string) error
	// Retrieve liefert das Credential nach erfolgreicher biometrischer
	// Prüfung; reason erscheint im Touch-ID-Dialog.
	Retrieve(reason string) (string, error)
	// Erase entfernt das Credential.
	Erase() error
}
