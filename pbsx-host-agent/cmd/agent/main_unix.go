//go:build agent && unix

package main

import (
	"context"
	"fmt"
	stdlog "log"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"github.com/kardianos/service"
	"github.com/pbs-x/pbsx-host-agent/internal/agent"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/binswap"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/cli"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/lifecycle"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/migration"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/nativebackup"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/nativerestore"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/registry"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/updater"
	"github.com/pbs-x/pbsx-host-agent/internal/conf"
	"github.com/pbs-x/pbsx-host-agent/internal/crypto"
	"github.com/pbs-x/pbsx-host-agent/internal/log"

	_ "net/http/pprof"
)

var Version = "v0.0.0"

type pbsService struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func (p *pbsService) Start(s service.Service) error {
	if conf.Env.EnableAutoUpdate && !conf.Env.InsideContainer {
		_, _ = updater.New(updater.Config{
			MinConstraint:  ">= 0.52.0",
			PollInterval:   2 * time.Minute,
			FetchOnStart:   true,
			UpgradeConfirm: func(v string) bool { return true },
			Exit: func(err error) {
				if err != nil {
					log.Error(err, "updater exiting with error")
				}
				log.Info("exiting for service manager to restart with updated binary")
				os.Exit(1)
			},
			Service: s,
			Context: p.ctx,
		})
	} else {
		// Register a singleton updater (no polling) so the server can still
		// push updates on demand via aRPC.
		_, _ = updater.New(updater.Config{
			MinConstraint:  ">= 0.52.0",
			PollInterval:   0,
			FetchOnStart:   false,
			UpgradeConfirm: func(v string) bool { return true },
			Exit: func(err error) {
				if err != nil {
					log.Error(err, "updater exiting with error")
				}
				log.Info("exiting for service manager to restart with updated binary")
				os.Exit(1)
			},
			Service: s,
			Context: p.ctx,
		})
	}
	go p.run()
	return nil
}

func (p *pbsService) run() {
	_ = log.L.SetServiceLogger()
	_ = registry.CreateEntryIfNotExists(&registry.RegistryEntry{Path: registry.CONFIG, Key: "ServerURL", Value: conf.Env.InitServerURL})
	_ = registry.CreateEntryIfNotExists(&registry.RegistryEntry{Path: registry.CONFIG, Key: "BootstrapToken", Value: conf.Env.InitBootstrapToken})
	if _, err := agent.InitializeHostID(); err != nil {
		log.Error(err, "failed to initialize PBS-X host identity")
		return
	}
	fingerprint := os.Getenv("PBSX_INIT_SERVER_CA_FINGERPRINT")
	if fingerprint == "" {
		fingerprint = os.Getenv("PBS_PLUS_INIT_SERVER_CA_FINGERPRINT")
	}
	_ = registry.CreateEntryIfNotExists(&registry.RegistryEntry{Path: registry.CONFIG, Key: "ServerCAFingerprint", Value: fingerprint})

	if err := lifecycle.WaitForServerURL(p.ctx); err != nil {
		return
	}
	log.Info("waiting for PBS-X enrollment")
	if err := lifecycle.WaitForBootstrap(p.ctx); err != nil {
		return
	}
	log.Info("PBS-X enrollment complete; native backup service active")

	if store, err := agent.NewBackupStore(); err == nil {
		_ = store.ClearAll()
	}

	innerCtx, innerCancel := context.WithCancel(p.ctx)
	var innerWg sync.WaitGroup

	innerWg.Go(func() {
		nativebackup.Poll(innerCtx, 15*time.Second)
	})

	innerWg.Go(func() {
		nativerestore.Poll(innerCtx, 15*time.Second)
	})

	innerWg.Go(func() {
		// Publish inventory immediately, then at the normal adaptive heartbeat interval.
		for {
			if err := lifecycle.UpdateDrives(); err != nil && innerCtx.Err() == nil {
				log.Warn("PBS-X heartbeat failed", "error", err.Error())
			}
			timer := time.NewTimer(agent.ComputeDelay())
			select {
			case <-innerCtx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	})

	if conf.Env.EnableLegacyARPC {
		if certErrCh, err := lifecycle.ConnectARPC(innerCtx, Version); err != nil {
			log.Warn("legacy aRPC control channel could not start", "error", err.Error())
		} else {
			innerWg.Go(func() {
				select {
				case <-innerCtx.Done():
					return
				case err, ok := <-certErrCh:
					if ok && err != nil {
						log.Warn("legacy aRPC control channel stopped", "error", err.Error())
					}
				}
			})
		}
	}

	<-p.ctx.Done()
	innerCancel()
	innerWg.Wait()
}

func (p *pbsService) Stop(s service.Service) error {
	if p.cancel != nil {
		p.cancel()
	}
	done := make(chan struct{})
	go func() { p.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
	}
	return nil
}

func main() {
	pending := updater.CheckPendingOnBoot()

	defer func() {
		if r := recover(); r != nil {
			_ = os.WriteFile("panic.log", fmt.Appendf(nil, "Panic: %v\n%s", r, debug.Stack()), 0644)
			os.Exit(1)
		}
	}()
	conf.Version = Version

	if pending {
		go func() {
			select {
			case <-time.After(binswap.CommitGrace):
				updater.CommitUpdate()
			}
		}()
	}

	if err := crypto.AssertFIPS(); err != nil {
		log.Error(err, "FIPS assertion failed")
		os.Exit(1)
	}

	// This is best-effort; if it fails due to permissions, the agent will fall back
	// to using the legacy paths automatically via initPaths() in conf package.
	_ = migration.TryMigrate()
	crypto.SetSealKeyPath(filepath.Join(conf.StatePrefix, ".seal.key"))

	cli.Entry()

	svcConfig := &service.Config{
		Name:        "pbsx-agent",
		DisplayName: "PBS-X Agent",
		Description: "PBS-X host backup agent",
		UserName:    "root",
		Option: service.KeyValue{
			"SystemdScript": lifecycle.SYSTEMD_SCRIPT,
			"OpenRCScript":  lifecycle.OPENRC_SCRIPT,
		},
	}
	prg := &pbsService{}
	prg.ctx, prg.cancel = context.WithCancel(context.Background())

	s, err := service.New(prg, svcConfig)
	if err != nil {
		stdlog.Fatal(err)
	}

	if len(os.Args) > 1 {
		if os.Args[1] == "version" {
			fmt.Print(Version)
			return
		}
		if err := service.Control(s, os.Args[1]); err != nil {
			log.Error(err,

				"service control failed", "action", os.Args[1])

			stdlog.Fatal(err)
		}
		return
	}

	dockerEnv := os.Getenv("PBSX_AGENT_CONTAINER")

	if service.Interactive() || dockerEnv == "true" {
		prg.run()
		return
	}

	err = s.Run()
	if err != nil {
		stdlog.Fatal(err)
	}
}
