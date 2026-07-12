// Package provider definiert das Provider-Interface und die
// Bitwarden-Implementierung via `bw serve`.
package provider

import "context"

// SecretRef referenziert ein einzelnes Secret im Vault.
// Entweder ItemID oder Item (Name) muss gesetzt sein.
type SecretRef struct {
	Env    string // Ziel-Umgebungsvariable
	ItemID string // Vault-Item-ID (hat Vorrang vor Item)
	Item   string // Vault-Item-Name
	Field  string // uri | username | password | <custom-field-name>
}

// Secret ist ein aufgelöster Secret-Wert.
type Secret struct {
	Value string
}

// Provider löst SecretRefs gegen ein Backend auf.
type Provider interface {
	Fetch(ctx context.Context, refs []SecretRef) (map[string]Secret, error)
	HealthCheck(ctx context.Context) error
}

// VaultStatus ist der Sperr-Zustand des Vaults.
type VaultStatus string

const (
	StatusLocked          VaultStatus = "locked"
	StatusUnlocked        VaultStatus = "unlocked"
	StatusUnauthenticated VaultStatus = "unauthenticated"
)
