//go:build windows

package main

import (
	"context"
	"fmt"
	stdlog "log"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"github.com/kardianos/service"
	"github.com/pbs-x/pbsx-host-agent/internal/agent"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/binswap"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/cli"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/lifecycle"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/nativebackup"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/nativerestore"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/updater"
	"github.com/pbs-x/pbsx-host-agent/internal/conf"
	"github.com/pbs-x/pbsx-host-agent/internal/log"
	"golang.org/x/sys/windows"

	_ "net/http/pprof"
)

var Version = "v0.0.0"

type pbsService struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func (p *pbsService) Start(s service.Service) error {
	if conf.Env.EnableAutoUpdate {
		_, _ = updater.New(updater.Config{
			MinConstraint: ">= 0.52.0", PollInterval: 2 * time.Minute, FetchOnStart: true,
			UpgradeConfirm: func(v string) bool { return true },
			Exit:           func(err error) {},
			Service:        s,
			Context:        p.ctx,
			BootstrapReady: lifecycle.BootstrapReady(),
		})
	} else {
		_, _ = updater.New(updater.Config{
			MinConstraint:  ">= 0.52.0",
			PollInterval:   0,
			FetchOnStart:   false,
			UpgradeConfirm: func(v string) bool { return true },
			Exit:           func(err error) {},
			Service:        s,
			Context:        p.ctx,
		})
	}
	go p.run()
	return nil
}

func (p *pbsService) run() {
	_ = log.L.SetServiceLogger()
	_ = windows.SetPriorityClass(windows.CurrentProcess(), 0x00000040)
	if err := persistEnrollmentValue("ServerURL", conf.Env.InitServerURL); err != nil {
		log.Error(err, "failed to save PBS-X server URL")
		return
	}
	if err := persistEnrollmentValue("BootstrapToken", conf.Env.InitBootstrapToken); err != nil {
		log.Error(err, "failed to save PBS-X bootstrap token")
		return
	}
	if _, err := agent.InitializeHostID(); err != nil {
		log.Error(err, "failed to initialize PBS-X host identity")
		return
	}
	fingerprint := os.Getenv("PBSX_INIT_SERVER_CA_FINGERPRINT")
	if fingerprint == "" {
		fingerprint = os.Getenv("PBS_PLUS_INIT_SERVER_CA_FINGERPRINT")
	}
	if err := persistEnrollmentValue("ServerCAFingerprint", fingerprint); err != nil {
		log.Error(err, "failed to save PBS-X server fingerprint")
		return
	}

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
			_ = os.WriteFile("panic.log", []byte(fmt.Sprintf("Panic: %v\n%s", r, debug.Stack())), 0644)
			os.Exit(1)
		}
	}()
	conf.Version = Version
	if requested, err := storageKeyCommand(os.Args[1:]); requested {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if code, requested, err := enrollmentCodeFromArgs(os.Args[1:]); requested {
		if err == nil {
			err = runEnrollmentCode(code)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "Enrollment failed:", err)
			os.Exit(1)
		}
		return
	}

	if pending {
		go func() {
			select {
			case <-time.After(binswap.CommitGrace):
				updater.CommitUpdate()
			}
		}()
	}

	cli.Entry()

	svcConfig := &service.Config{
		Name: "PBSXAgent", DisplayName: "PBS-X Agent",
		Option: service.KeyValue{"OnFailure": "restart", "OnFailureDelayDuration": "5s"},
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
		_ = service.Control(s, os.Args[1])
		return
	}

	if service.Interactive() {
		prg.run()
		return
	}

	err = s.Run()
	if err != nil {
		stdlog.Fatal(err)
	}
}
