//go:build windows

package main

import (
	"fmt"

	"github.com/kardianos/service"
	"golang.org/x/sys/windows"
)

func requireSetupPermissions() error {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return fmt.Errorf("agent setup requires administrator approval")
	}
	return nil
}

func agentServiceConfig() *service.Config {
	return &service.Config{Name: "PBSXAgent", DisplayName: "PBS-X Agent", Description: "PBS-X host backup agent",
		Option: service.KeyValue{"OnFailure": "restart", "OnFailureDelayDuration": "5s"}}
}
