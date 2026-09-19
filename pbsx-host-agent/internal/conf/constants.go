package conf

import "time"

const (
	ProxyTargetURL = "https://127.0.0.1:8007"

	ARPCServerPort   = ":8008"
	ARPCQuicPort     = ":8008"
	AgentAPIPort     = ":8018"
	ServerAPIExtPort = ":8017"

	ModifiedFilePath       = "/js/proxmox-backup-gui.js"
	CertFile               = "/etc/proxmox-backup/proxy.pem"
	KeyFile                = "/etc/proxmox-backup/proxy.key"
	AgentTLSCACertFile     = "/etc/proxmox-backup/pbs-x/certs/ca.crt"
	AgentTLSCAKeyFile      = "/etc/proxmox-backup/pbs-x/certs/ca.key"
	AgentTLSPrevCACertFile = "/etc/proxmox-backup/pbs-x/certs/ca-prev.crt"
	AgentTLSPrevCAKeyFile  = "/etc/proxmox-backup/pbs-x/certs/ca-prev.key"
	TLSCARotationGraceDays = 30
	AuthTokenExpiration    = 24 * time.Hour

	HTTPReadTimeout    = 10 * time.Second
	HTTPWriteTimeout   = 5 * time.Minute
	HTTPIdleTimeout    = 5 * time.Minute
	HTTPMaxHeaderBytes = 1 << 20
	HTTPRateLimit      = 100.0
	HTTPRateBurst      = 200

	TimerBasePath        = "/lib/systemd/system"
	DbBasePath           = "/var/lib/proxmox-backup"
	AgentMountBasePath   = "/mnt/pbsx-host-mounts"
	RestoreMountBasePath = "/mnt/pbsx-host-restores"
	LogsBasePath         = "/var/log/proxmox-backup"
	TaskLogsBasePath     = LogsBasePath + "/tasks"
	ActiveLogsPath       = TaskLogsBasePath + "/active"
	ArchivedLogsPath     = TaskLogsBasePath + "/archive"
	PBSAuthKeyPath       = "/etc/proxmox-backup/authkey.key"
)

var (
	StatePrefix         = "/var/lib/pbsx-agent"
	ScriptsBasePath     = StatePrefix + "/scripts"
	SecretsKeyPath      = StatePrefix + "/.secret.key"
	BackupLogsBasePath  = "/var/log/pbsx-agent"
	RestoreLogsBasePath = "/var/log/pbsx-agent/restores"
	MountSocketPath     = "/run/proxmox-backup/pbsx-host-mount.sock"
	JobMutateSocketPath = "/run/proxmox-backup/pbsx-host-job.sock"
	RestoreSocketPath   = "/run/proxmox-backup/pbsx-host-restore.sock"
	LockSocketPath      = "/run/proxmox-backup/pbsx-host-lock.sock"
	MemcachedSocketPath = "/run/proxmox-backup/pbsx-host-cache.sock"
)
