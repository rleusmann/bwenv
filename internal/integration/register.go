//go:build integration

// Package integration enthält den End-to-End-Test gegen eine echte
// Vaultwarden-Instanz (Plan §10). Dieses File implementiert die
// clientseitige Bitwarden-Registrierungs-Krypto — nur für das Seeding
// des Test-Accounts, nicht Teil des Produkt-Codes.
package integration

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/pbkdf2"
)

const kdfIterations = 600_000

// registerAccount legt einen Account per /identity/register an —
// mit derselben Krypto, die der Bitwarden-Client verwendet:
// masterKey = PBKDF2(pw, email), Auth-Hash = PBKDF2(masterKey, pw, 1),
// Sym-Key (64 B) verschlüsselt mit HKDF-gestrecktem masterKey (EncString Typ 2),
// RSA-Keypair verschlüsselt mit dem Sym-Key.
func registerAccount(serverURL, email, password string) error {
	masterKey := pbkdf2.Key([]byte(password), []byte(strings.ToLower(email)), kdfIterations, 32, sha256.New)
	authHash := pbkdf2.Key(masterKey, []byte(password), 1, 32, sha256.New)

	encKey, err := hkdfExpand(masterKey, "enc", 32)
	if err != nil {
		return err
	}
	macKey, err := hkdfExpand(masterKey, "mac", 32)
	if err != nil {
		return err
	}

	symKey := make([]byte, 64)
	if _, err := rand.Read(symKey); err != nil {
		return err
	}
	protectedSymKey, err := encStringType2(symKey, encKey, macKey)
	if err != nil {
		return err
	}

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&rsaKey.PublicKey)
	if err != nil {
		return err
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	if err != nil {
		return err
	}
	encPrivKey, err := encStringType2(privDER, symKey[:32], symKey[32:])
	if err != nil {
		return err
	}

	payload := map[string]any{
		"email":              email,
		"name":               "bwenv integration",
		"masterPasswordHash": base64.StdEncoding.EncodeToString(authHash),
		"masterPasswordHint": nil,
		"key":                protectedSymKey,
		"kdf":                0, // PBKDF2-SHA256
		"kdfIterations":      kdfIterations,
		"keys": map[string]any{
			"publicKey":           base64.StdEncoding.EncodeToString(pubDER),
			"encryptedPrivateKey": encPrivKey,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// Endpunkt variiert je nach Vaultwarden-Version.
	var lastErr error
	for _, path := range []string{"/identity/accounts/register", "/api/accounts/register"} {
		resp, err := insecureClient.Post(serverURL+path, "application/json", bytes.NewReader(body))
		if err != nil {
			return err
		}
		status := resp.StatusCode
		msg, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if status/100 == 2 {
			return nil
		}
		lastErr = fmt.Errorf("register %s: HTTP %d: %.300s", path, status, msg)
		if status != http.StatusNotFound {
			break
		}
	}
	return lastErr
}

func hkdfExpand(key []byte, info string, length int) ([]byte, error) {
	out := make([]byte, length)
	if _, err := io.ReadFull(hkdf.Expand(sha256.New, key, []byte(info)), out); err != nil {
		return nil, err
	}
	return out, nil
}

// encStringType2 erzeugt einen Bitwarden-EncString Typ 2:
// "2.<b64 iv>|<b64 ct>|<b64 hmac(iv|ct)>", AES-256-CBC + HMAC-SHA256.
func encStringType2(plaintext, encKey, macKey []byte) (string, error) {
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return "", err
	}
	padded := pkcs7Pad(plaintext, aes.BlockSize)
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, padded)

	mac := hmac.New(sha256.New, macKey)
	mac.Write(iv)
	mac.Write(ct)

	b64 := base64.StdEncoding.EncodeToString
	return "2." + b64(iv) + "|" + b64(ct) + "|" + b64(mac.Sum(nil)), nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	pad := blockSize - len(data)%blockSize
	return append(data, bytes.Repeat([]byte{byte(pad)}, pad)...)
}
