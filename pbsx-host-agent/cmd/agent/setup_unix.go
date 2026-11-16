//go:build agent && unix

package main

import (
	"fmt"
	"os"

	"github.com/kardianos/service"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/lifecycle"
)

func requireSetupPermissions() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("run agent setup with sudo or through the desktop's administrative prompt")
	}
	return nil
}

func agentServiceConfig() *service.Config {
	return &service.Config{Name: "pbsx-agent", DisplayName: "PBS-X Agent", Description: "PBS-X host backup agent", UserName: "root",
		Option: service.KeyValue{"SystemdScript": lifecycle.SYSTEMD_SCRIPT, "OpenRCScript": lifecycle.OPENRC_SCRIPT}}
}
