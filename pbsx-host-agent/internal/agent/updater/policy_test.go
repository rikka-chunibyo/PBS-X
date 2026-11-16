package updater

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"errors"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/workgate"
	"strings"
	"testing"
)

func TestPBSXReleaseVersionPolicy(t *testing.T) {
	for _, tc := range []struct {
		current, offered string
		valid, unchanged bool
	}{
		{"0.6.0-preview", "0.6.1-preview", true, false},
		{"0.6.0-preview", "0.6.0", true, false},
		{"v0.6.0", "0.6.1", true, false},
		{"0.6.1", "0.6.0", false, true},
		{"0.6.1", "0.6.1+different-build", false, true},
		{"0.6.1", "", false, true},
		{"unknown", "0.6.2", false, false},
		{"0.5.0", "0.5.1", false, false},
		{"0.6.0", "invalid", false, false},
	} {
		err := validateCandidate(tc.current, tc.offered, "")
		if (err == nil) != tc.valid || errors.Is(err, ErrAlreadyLatest) != tc.unchanged {
			t.Errorf("%s -> %s: %v", tc.current, tc.offered, err)
		}
	}
}

func TestUpdateSignaturesFailClosed(t *testing.T) {
	oldECDSA, oldEd := ECDSAPublicKeyB64, Ed25519PublicKeyB64
	t.Cleanup(func() { ECDSAPublicKeyB64, Ed25519PublicKeyB64 = oldECDSA, oldEd })
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("a release payload")
	signature := ed25519.Sign(private, payload)
	ECDSAPublicKeyB64, Ed25519PublicKeyB64 = "", ""
	if verifyUpdatePayload(payload, nil, signature) == nil {
		t.Fatal("unconfigured signing identity was trusted")
	}
	Ed25519PublicKeyB64 = base64.StdEncoding.EncodeToString(public)
	if err := verifyUpdatePayload(payload, nil, signature); err != nil {
		t.Fatal(err)
	}
	if verifyUpdatePayload([]byte("tampered"), nil, signature) == nil || verifyUpdatePayload(payload, nil, nil) == nil {
		t.Fatal("missing signature or tampered payload was accepted")
	}
	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	Ed25519PublicKeyB64 = base64.StdEncoding.EncodeToString(other)
	if verifyUpdatePayload(payload, nil, signature) == nil {
		t.Fatal("signature from another publisher was accepted")
	}
}

func TestECDSASignatureRejectsTrailingData(t *testing.T) {
	old := ECDSAPublicKeyB64
	t.Cleanup(func() { ECDSAPublicKeyB64 = old })
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	ECDSAPublicKeyB64 = base64.StdEncoding.EncodeToString(public)
	payload := []byte("a release")
	hash := sha256.Sum256(payload)
	r, s, err := ecdsa.Sign(rand.Reader, key, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	der, err := asn1.Marshal(ecdsaSig{R: r, S: s})
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyWithECDSA(payload, []byte(base64.StdEncoding.EncodeToString(der))); err != nil {
		t.Fatal(err)
	}
	der = append(der, 0)
	if verifyWithECDSA(payload, []byte(base64.StdEncoding.EncodeToString(der))) == nil {
		t.Fatal("signature trailing bytes were ignored")
	}
}

func TestUpdateDefersWhileNativeWorkIsActive(t *testing.T) {
	workgate.Mu.Lock()
	defer workgate.Mu.Unlock()
	// With no server/configuration this must still return the busy error before
	// attempting network access, staging a binary or stopping a service.
	u := &Updater{}
	if err := u.CheckNow(); err == nil || !strings.Contains(err.Error(), "backup/restore is active") {
		t.Fatalf("update did not defer active work: %v", err)
	}
}
