//go:build agent || windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/kardianos/service"
	"github.com/pbs-x/pbsx-host-agent/internal/agent"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/enrollment"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/lifecycle"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/registry"
	"github.com/pbs-x/pbsx-host-agent/internal/agentstatus"
	"github.com/pbs-x/pbsx-host-agent/internal/conf"
)

func setupCommand(args []string) (bool, error) {
	if len(args) == 0 || args[0] != "setup" {
		return false, nil
	}
	if err := requireSetupPermissions(); err != nil {
		return true, err
	}
	var reader io.Reader
	if len(args) == 2 && args[1] == "--code-stdin" {
		reader = os.Stdin
	} else if len(args) == 3 && args[1] == "--code-file" {
		file, err := os.Open(args[2])
		if err != nil {
			return true, fmt.Errorf("open enrollment input: %w", err)
		}
		defer file.Close()
		reader = file
	} else {
		return true, fmt.Errorf("usage: pbsx-agent setup --code-stdin | --code-file PATH")
	}
	data, err := io.ReadAll(io.LimitReader(reader, (64<<10)+1))
	if err != nil {
		return true, err
	}
	if len(data) > 64<<10 {
		return true, fmt.Errorf("enrollment input exceeds size limit")
	}
	values, err := enrollment.Decode(strings.TrimSpace(string(data)))
	if err != nil {
		return true, fmt.Errorf("invalid enrollment input: %w", err)
	}
	if err := agent.ValidateHostID(values.HostID); err != nil {
		return true, err
	}
	if status, err := agentstatus.Read(); err == nil && status.Running(time.Now()) && status.Operation != nil {
		return true, fmt.Errorf("backup/restore is active; retry enrollment when idle")
	}
	prg := &pbsService{}
	prg.ctx, prg.cancel = context.WithCancel(context.Background())
	defer prg.cancel()
	s, err := service.New(prg, agentServiceConfig())
	if err != nil {
		return true, err
	}
	status, err := s.Status()
	exists := err == nil
	if err != nil && !errors.Is(err, service.ErrNotInstalled) {
		return true, err
	}
	if exists && status == service.StatusRunning {
		if err := s.Stop(); err != nil {
			return true, fmt.Errorf("stop agent before enrollment: %w", err)
		}
		deadline := time.Now().Add(30 * time.Second)
		for {
			status, err = s.Status()
			if err != nil {
				return true, err
			}
			if status == service.StatusStopped {
				break
			}
			if time.Now().After(deadline) {
				return true, fmt.Errorf("timed out stopping agent; enrollment was not changed")
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	if err := agent.SetStorageCredentials(agent.StorageCredentials{AuthID: values.StorageAuthID, Secret: values.StorageSecret}); err != nil {
		return true, err
	}
	// Stop the service first and write the bootstrap code only after validating
	// the complete input. The desktop never reads the resulting vault.
	for _, entry := range []struct{ key, value string }{
		{"HostID", values.HostID}, {"ServerCAFingerprint", values.ServerCAFingerprint},
		{"BootstrapToken", values.BootstrapToken}, {"ServerURL", values.ServerURL},
	} {
		if err := registry.UpsertEntry(&registry.RegistryEntry{Path: registry.CONFIG, Key: entry.key, Value: entry.value}); err != nil {
			return true, err
		}
	}
	conf.Env.HostID = values.HostID
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := lifecycle.WaitForBootstrap(ctx); err != nil {
		return true, fmt.Errorf("enrollment did not complete; the service remains stopped: %w", err)
	}
	if exists {
		if err := s.Uninstall(); err != nil {
			return true, fmt.Errorf("refresh agent service: %w", err)
		}
	}
	if err := s.Install(); err != nil {
		return true, fmt.Errorf("install agent service: %w", err)
	}
	if err := s.Start(); err != nil {
		return true, fmt.Errorf("start agent service: %w", err)
	}
	fmt.Fprintln(os.Stdout, "PBS-X enrollment complete; background service started.")
	return true, nil
}
