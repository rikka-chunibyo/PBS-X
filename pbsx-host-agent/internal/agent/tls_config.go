package agent

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pbs-x/pbsx-host-agent/internal/agent/registry"
	"github.com/pbs-x/pbsx-host-agent/internal/log"
	"github.com/pbs-x/pbsx-host-agent/internal/mtls"
)

var (
	tlsConfigCache *tls.Config
	tlsConfigMutex sync.RWMutex
	cacheExpiry    time.Time
	cacheTTL       = 30 * time.Minute
)

// GetTLSConfig returns the TLS configuration used by the PBS-X control plane.
//
// Agent identity is a self-signed client certificate whose exact fingerprint is
// bound to the Host ID during enrollment. Server authentication is independent:
// a configured PBS fingerprint pins the leaf certificate, otherwise normal
// platform trust roots and hostname validation are used.
func GetTLSConfig() (*tls.Config, error) {
	tlsConfigMutex.RLock()
	if tlsConfigCache != nil && time.Now().Before(cacheExpiry) {
		cfg := tlsConfigCache
		tlsConfigMutex.RUnlock()
		return cfg, nil
	}
	tlsConfigMutex.RUnlock()

	tlsConfigMutex.Lock()
	defer tlsConfigMutex.Unlock()
	if tlsConfigCache != nil && time.Now().Before(cacheExpiry) {
		return tlsConfigCache, nil
	}

	certReg, err := registry.GetEntry(registry.AUTH, "Cert", true)
	if err != nil {
		return nil, fmt.Errorf("GetTLSConfig: client certificate not found: %w", err)
	}
	keyReg, err := registry.GetEntry(registry.AUTH, "Priv", true)
	if err != nil {
		return nil, fmt.Errorf("GetTLSConfig: client key not found: %w", err)
	}
	cert, err := tls.X509KeyPair([]byte(certReg.Value), []byte(keyReg.Value))
	if err != nil {
		return nil, fmt.Errorf("GetTLSConfig: invalid client identity: %w", err)
	}

	serverURL, err := registry.GetEntry(registry.CONFIG, "ServerURL", false)
	if err != nil {
		return nil, fmt.Errorf("GetTLSConfig: server URL not found: %w", err)
	}
	_, serverName, err := pbsAPIEndpoint(serverURL.Value, "/")
	if err != nil {
		return nil, fmt.Errorf("GetTLSConfig: invalid server URL: %w", err)
	}

	cfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		ServerName:   serverName,
		Certificates: []tls.Certificate{cert},
	}
	pin, pinErr := registry.GetEntry(registry.CONFIG, "ServerCAFingerprint", false)
	if pinErr == nil && pin != nil && strings.TrimSpace(pin.Value) != "" {
		expected := normalizeFingerprint(pin.Value)
		if len(expected) != 64 {
			return nil, fmt.Errorf("GetTLSConfig: invalid ServerCAFingerprint")
		}
		cfg.InsecureSkipVerify = true // exact pinned-certificate verification below
		cfg.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return fmt.Errorf("PBS-X control server supplied no certificate")
			}
			digest := sha256.Sum256(cs.PeerCertificates[0].Raw)
			if hex.EncodeToString(digest[:]) != expected {
				return fmt.Errorf("PBS-X server certificate fingerprint mismatch")
			}
			return nil
		}
	}

	tlsConfigCache = cfg
	cacheExpiry = time.Now().Add(cacheTTL)
	return cfg, nil
}

func InvalidateTLSConfigCache() {
	tlsConfigMutex.Lock()
	defer tlsConfigMutex.Unlock()
	tlsConfigCache = nil
	cacheExpiry = time.Time{}
}

// RenewCertificateIfExpiring currently reports when the self-owned identity needs
// rotation. PBS-X does not silently replace the host's private key. A future
// authenticated control-plane rotation endpoint can rotate it without another
// administrator enrollment token.
func RenewCertificateIfExpiring() error {
	certReg, err := registry.GetEntry(registry.AUTH, "Cert", true)
	if err != nil {
		return fmt.Errorf("RenewCertificateIfExpiring: failed to retrieve certificate: %w", err)
	}
	cert, err := mtls.ParseCertInfo([]byte(certReg.Value))
	if err != nil {
		return fmt.Errorf("RenewCertificateIfExpiring: invalid client certificate: %w", err)
	}

	now := time.Now()
	if !cert.NotAfter.After(now) {
		return fmt.Errorf("PBS-X client certificate expired at %s; host must be re-enrolled", cert.NotAfter)
	}
	remaining := time.Until(cert.NotAfter)
	if remaining < 30*24*time.Hour {
		return fmt.Errorf("PBS-X client certificate expires in %s; rotate or re-enroll the host", remaining.Round(time.Hour))
	}
	log.Debug("PBS-X client identity is valid", "expires", cert.NotAfter, "days_left", int(remaining.Hours()/24))
	return nil
}

func clearAuthEntries() {
	for _, key := range []string{"Cert", "Priv", "BackupAuthID", "BackupSecret", "ServerCA", "LegacyServerCA"} {
		if err := registry.DeleteEntry(registry.AUTH, key); err != nil {
			log.Error(err, "failed to clear PBS-X auth entry", "key", key)
		}
	}
	InvalidateTLSConfigCache()
}
