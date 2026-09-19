package conf

import "os"

func env(primary, legacy string) string {
	if value := os.Getenv(primary); value != "" {
		return value
	}
	if legacy != "" {
		return os.Getenv(legacy)
	}
	return ""
}

func envTrue(primary, legacy string) bool {
	return env(primary, legacy) == "true"
}

type EnvConfig struct {
	Debug              bool
	Hostname           string
	HostID             string
	StdoutOnly         bool
	ClientNofile       string
	EnableAutoUpdate   bool
	EnableLegacyARPC   bool
	InsideContainer    bool
	UpdateIntervalMin  string
	ForceDisableIRM    bool
	PprofEnabled       bool
	LogDedupWindow     string
	InitServerURL      string
	InitBootstrapToken string
}

var Env = loadEnvConfig()

func loadEnvConfig() EnvConfig {
	return EnvConfig{
		Debug:        os.Getenv("DEBUG") == "true",
		Hostname:     env("PBSX_HOSTNAME", "PBS_PLUS_HOSTNAME"),
		HostID:       env("PBSX_HOST_ID", ""),
		StdoutOnly:   envTrue("PBSX_STDOUT_ONLY", "PBS_PLUS_STDOUT_ONLY"),
		ClientNofile: env("PBSX_CLIENT_NOFILE", "PBS_PLUS_CLIENT_NOFILE"),
		// PBS-X owns its update channel. Keep automatic updates off until that API exists.
		EnableAutoUpdate: envTrue("PBSX_ENABLE_AUTO_UPDATE", ""),
		// Native backup dispatch and data transfer use PBS itself. The old PBS Plus aRPC
		// channel is optional compatibility plumbing for restore/control while those paths
		// are being ported.
		EnableLegacyARPC:   envTrue("PBSX_ENABLE_LEGACY_ARPC", ""),
		InsideContainer:    envTrue("PBSX_AGENT_CONTAINER", ""),
		UpdateIntervalMin:  env("PBSX_UPDATE_INTERVAL_MINUTES", "PBS_PLUS_UPDATE_INTERVAL_MINUTES"),
		ForceDisableIRM:    envTrue("PBSX_FORCE_DISABLE_IRM_GENERATE", "PBS_PLUS_FORCE_DISABLE_IRM_GENERATE"),
		PprofEnabled:       envTrue("PBSX_PPROF", "PBS_PLUS_PPROF"),
		LogDedupWindow:     os.Getenv("LOG_DEDUP_WINDOW"),
		InitServerURL:      env("PBSX_INIT_SERVER_URL", "PBS_PLUS_INIT_SERVER_URL"),
		InitBootstrapToken: env("PBSX_INIT_BOOTSTRAP_TOKEN", "PBS_PLUS_INIT_BOOTSTRAP_TOKEN"),
	}
}
