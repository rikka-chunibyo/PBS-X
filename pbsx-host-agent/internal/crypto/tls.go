package crypto

import (
	"crypto/fips140"
	"crypto/tls"
)

func FIPSServerTLSConfig() *tls.Config {
	cfg := &tls.Config{
		MinVersion:               tls.VersionTLS12,
		PreferServerCipherSuites: true,
	}
	if !fips140.Enabled() {
		cfg.CipherSuites = FIPSAllowedCipherSuites
		cfg.CurvePreferences = FIPSAllowedCurvePreferences
	}
	return cfg
}

func FIPSClientTLSConfig(minVersion uint16) *tls.Config {
	if minVersion < tls.VersionTLS12 {
		minVersion = tls.VersionTLS12
	}
	cfg := &tls.Config{
		MinVersion: minVersion,
	}
	if !fips140.Enabled() {
		cfg.CipherSuites = FIPSAllowedCipherSuites
		cfg.CurvePreferences = FIPSAllowedCurvePreferences
	}
	return cfg
}

var FIPSAllowedCipherSuites = []uint16{
	tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
}

var FIPSAllowedCurvePreferences = []tls.CurveID{
	tls.X25519,
	tls.CurveP256,
	tls.CurveP384,
}
