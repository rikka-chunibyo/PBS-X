package lifecycle

const SYSTEMD_SCRIPT = `[Unit]
Description={{Description}}
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=300
StartLimitBurst=20

[Service]
Type=simple
User=root
Group=root
WorkingDirectory=/var/lib/pbsx-agent
ExecStart={{Path}}
Restart=always
RestartSec=3
EnvironmentFile=-/etc/pbsx-agent/agent.env

RuntimeDirectory=pbsx-agent
RuntimeDirectoryMode=0755

CapabilityBoundingSet=CAP_DAC_READ_SEARCH CAP_DAC_OVERRIDE
AmbientCapabilities=CAP_DAC_READ_SEARCH CAP_DAC_OVERRIDE
NoNewPrivileges=yes
LimitNOFILE=1048576

ProtectSystem=full
ProtectHome=no
PrivateTmp=yes

ReadWritePaths=/opt/pbsx-agent /var/lib/pbsx-agent /var/log/pbsx-agent /etc/pbsx-agent

[Install]
WantedBy=multi-user.target
`

const OPENRC_SCRIPT = `#!/sbin/openrc-run
name="{{DisplayName}}"
description="{{Description}}"

command="{{Path}}"
command_user="root:root"
directory="/var/lib/pbsx-agent"
agent_env="/etc/pbsx-agent/agent.env"

output_log="/var/log/pbsx-agent/agent.log"
error_log="/var/log/pbsx-agent/agent.error.log"

supervisor=supervise-daemon
respawn_delay=3
respawn_max=0

depend() {
    need net
    after firewall
}

start_pre() {
    checkpath --directory --owner root:root --mode 0755 /run/pbsx-agent
    checkpath --directory --owner root:root --mode 0750 /var/log/pbsx-agent
    checkpath --directory --owner root:root --mode 0750 /var/lib/pbsx-agent
    
    if [ -f "$agent_env" ]; then
        while IFS= read -r line || [ -n "$line" ]; do
            case "$line" in
                ''|#*) continue ;;
                *=*) 
                    key=$(echo "${line%%=*}" | tr -d ' ')
                    val=$(echo "${line#*=}" | sed "s/^['\"]//;s/['\"]$//")
                    export "$key=$val"
                    ;;
            esac
        done < "$agent_env"
    fi

    if command -v getcap >/dev/null 2>&1; then
        if ! getcap "$command" | grep -q "cap_dac_read_search"; then
            ewarn "Warning: Binary lacks CAP_DAC_READ_SEARCH. Backups may fail."
        fi
    fi
}
`
