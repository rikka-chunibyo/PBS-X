package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/pbs-x/pbsx-host-agent/internal/agent"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/cli"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/registry"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/sync"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/updater"
	"github.com/pbs-x/pbsx-host-agent/internal/arpc"
	"github.com/pbs-x/pbsx-host-agent/internal/conf"
	"github.com/pbs-x/pbsx-host-agent/internal/log"
)

var ErrHostNotExpected = errors.New("host not expected by server - requires re-bootstrap")

var bootstrapDone chan struct{}

func init() {
	bootstrapDone = make(chan struct{})
}

func BootstrapReady() <-chan struct{} {
	return bootstrapDone
}

func isCertError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "x509:") ||
		strings.Contains(msg, "tls:") ||
		strings.Contains(msg, "certificate")
}

// IsHostNotExpected checks if an error indicates the agent host was
// because ConnectARPC uses plain hostname without X-PBSX-BackupID
func IsHostNotExpected(err error) bool {
	if errors.Is(err, ErrHostNotExpected) {
		return true
	}
	msg := err.Error()
	// Server-side rejection from AgentsManager.isExpected:
	// HTTP middleware rejection from checkAgentAuth when
	//   "CheckAgentAuth: certificate not trusted"
	return strings.Contains(msg, "is not expected by server") ||
		strings.Contains(msg, "CheckAgentAuth: certificate not trusted")
}

func ClearCertificates() {
	if err := registry.DeleteEntry(registry.AUTH, "ServerCA"); err != nil {
		log.Error(err, "")
	}
	if err := registry.DeleteEntry(registry.AUTH, "Cert"); err != nil {
		log.Error(err, "")
	}
	if err := registry.DeleteEntry(registry.AUTH, "Priv"); err != nil {
		log.Error(err, "")
	}
	agent.InvalidateTLSConfigCache()
}

func UpdateDrives() error {
	hostID, err := agent.AgentHostID()
	if err != nil {
		return err
	}
	heartbeat, err := agent.CurrentHeartbeat()
	if err != nil {
		return err
	}
	payload := struct {
		HostID string `json:"host-id"`
		agent.AgentHeartbeat
	}{HostID: hostID, AgentHeartbeat: heartbeat}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return agent.PBSAPIRequest(ctx, http.MethodPost, "/pbsx/agent/status", payload, nil)
}

func WaitForServerURL(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		entry, err := registry.GetEntry(registry.CONFIG, "ServerURL", false)
		if err == nil && entry != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func WaitForBootstrap(ctx context.Context) error {
	if err := waitForBootstrap(ctx, bootstrapActions{
		lookup: registry.GetEntry,
		renew:  agent.RenewCertificateIfExpiring,
		enroll: agent.Bootstrap,
	}); err != nil {
		return err
	}
	select {
	case <-bootstrapDone:
	default:
		close(bootstrapDone)
	}
	return nil
}

type bootstrapActions struct {
	lookup func(string, string, bool) (*registry.RegistryEntry, error)
	renew  func() error
	enroll func() error
}

func waitForBootstrap(ctx context.Context, actions bootstrapActions) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		token, err := actions.lookup(registry.CONFIG, "BootstrapToken", false)
		if err != nil && !errors.Is(err, registry.ErrNotFound) {
			return fmt.Errorf("read pending enrollment: %w", err)
		}
		pending := token != nil && strings.TrimSpace(token.Value) != ""
		// A newly supplied enrollment code takes precedence over a still-valid
		// old identity. Keep the old credentials until bootstrap succeeds, but
		// never start backup polling with them while enrollment is pending.
		if !pending {
			cert, errCert := actions.lookup(registry.AUTH, "Cert", true)
			priv, errPriv := actions.lookup(registry.AUTH, "Priv", true)
			backupAuthID, errAuthID := actions.lookup(registry.AUTH, "BackupAuthID", false)
			backupSecret, errSecret := actions.lookup(registry.AUTH, "BackupSecret", true)

			if errCert == nil && errPriv == nil && errAuthID == nil && errSecret == nil &&
				cert != nil && priv != nil && backupAuthID != nil && backupSecret != nil && actions.renew() == nil {
				return nil
			}
		}

		if err := actions.enroll(); err != nil {
			log.Error(err, "")
		} else {
			continue // Verify the new persisted identity without another timer delay.
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func ConnectARPC(
	ctx context.Context,
	version string,
) (<-chan error, error) {
	serverUrl, err := registry.GetEntry(registry.CONFIG, "ServerURL", false)
	if err != nil {
		return nil, err
	}
	uri, err := agent.ParseURI(serverUrl.Value)
	if err != nil {
		return nil, err
	}
	tlsConfig, err := agent.GetTLSConfig()
	if err != nil {
		return nil, err
	}
	clientId, err := agent.AgentHostID()
	if err != nil {
		return nil, err
	}

	address := fmt.Sprintf(
		"%s%s",
		strings.TrimSuffix(uri.Hostname(), ":"),
		conf.ARPCServerPort,
	)
	headers := http.Header{}
	headers.Add("X-PBS-Agent", clientId)
	headers.Add("X-PBSX-Version", version)

	certErrCh := make(chan error, 1)

	signalCertError := func(err error) {
		select {
		case certErrCh <- err:
		default:
		}
	}

	go func() {
		defer close(certErrCh)

		base := 500 * time.Millisecond
		maxWait := 30 * time.Second
		factor := 2.0
		jitter := 0.2
		backoff := base
		connectionFailureLogged := false
		var session arpc.Session

		for {
			select {
			case <-ctx.Done():
				if session != nil {
					session.Close()
				}
				return
			default:
			}

			var connErr error
			var quicErr error
			transport := "QUIC"
			session, connErr = arpc.DialQuic(
				ctx, address, tlsConfig, headers,
			)
			if connErr != nil {
				quicErr = connErr
				if isCertError(connErr) {
					log.Error(connErr,
						"certificate error on connect, requesting re-bootstrap")

					signalCertError(connErr)
					return
				}

				if IsHostNotExpected(connErr) {
					log.Warn("host not expected by server, stopping retries until re-bootstrap",
						"error", connErr.Error())

					signalCertError(ErrHostNotExpected)
					return
				}
				var tcpPipe *arpc.StreamPipe
				tcpPipe, connErr = arpc.ConnectToServer(ctx, address, headers, tlsConfig)
				if connErr == nil {
					session = tcpPipe
					transport = "TCP/mTLS"
				}
			}

			if connErr != nil {
				if isCertError(connErr) {
					log.Error(connErr,
						"certificate error on connect, requesting re-bootstrap")

					signalCertError(connErr)
					return
				}

				if IsHostNotExpected(connErr) {
					log.Warn("host not expected by server, stopping retries until re-bootstrap",
						"error", connErr.Error())

					signalCertError(ErrHostNotExpected)
					return
				}
				if !connectionFailureLogged {
					attrs := []any{"error", connErr.Error()}
					if quicErr != nil {
						attrs = append(attrs, "quic_error", quicErr.Error())
					}
					log.Warn("server connection unavailable; retrying", attrs...)
					connectionFailureLogged = true
				}

				sleep := min(
					time.Duration(
						float64(backoff)*(1+jitter*(2*rand.Float64()-1)),
					),
					maxWait,
				)
				select {
				case <-ctx.Done():
					return
				case <-time.After(sleep):
					backoff = min(
						time.Duration(float64(backoff)*factor),
						maxWait,
					)
					continue
				}
			}

			session.SetHeaders(headers)

			router := arpc.NewRouter()
			router.Handle(
				"ping",
				func(req *arpc.Request) (arpc.Response, error) {
					b, err := cbor.Marshal(map[string]string{
						"version":  version,
						"hostname": clientId,
					})
					if err != nil {
						return arpc.Response{}, err
					}
					return arpc.Response{Status: 200, Data: b}, nil
				},
			)
			router.Handle(
				"backup",
				func(req *arpc.Request) (arpc.Response, error) {
					return sync.BackupStartHandler(req, nil)
				},
			)
			router.Handle(
				"restore",
				func(req *arpc.Request) (arpc.Response, error) {
					return sync.RestoreStartHandler(req, nil)
				},
			)
			router.Handle(
				"filetree",
				func(req *arpc.Request) (arpc.Response, error) {
					return sync.FileTreeHandler(req, nil)
				},
			)
			router.Handle("target_status", sync.StatusHandler)
			router.Handle("target_status_batch", sync.StatusBatchHandler)
			router.Handle("cleanup", sync.BackupCloseHandler)
			router.Handle("cleanup_restore", sync.RestoreCloseHandler)
			router.Handle("verify_start", cli.VerifyStartHandler)
			router.Handle("update", func(req *arpc.Request) (arpc.Response, error) {
				if err := updater.TriggerUpdate(); err != nil {
					return arpc.Response{}, arpc.WrapError(err)
				}
				data, err := cbor.Marshal(map[string]string{
					"status": "update triggered",
				})
				if err != nil {
					return arpc.Response{}, arpc.WrapError(err)
				}
				return arpc.Response{Status: 200, Data: data}, nil
			})
			session.SetRouter(router)
			backoff = base
			if connectionFailureLogged {
				log.Info("server connection restored", "transport", transport)
				connectionFailureLogged = false
			} else if quicErr != nil {
				log.Info("connected using TCP/mTLS fallback", "quic_error", quicErr.Error())
			}

			updater.CheckOnReconnect()

			serveErr := session.Serve()
			session.Close()
			session = nil

			if serveErr != nil {
				if isCertError(serveErr) {
					log.Error(serveErr,
						"certificate error during session, requesting re-bootstrap")

					signalCertError(serveErr)
					return
				}
				// QUIC closes the connection without a rejection frame
				tcpPipe, probeErr := arpc.ConnectToServer(ctx, address, headers, tlsConfig)
				if probeErr != nil {
					tcpPipe = nil
					if IsHostNotExpected(probeErr) {
						log.Warn("host not expected by server (detected via probe), stopping retries until re-bootstrap",
							"error", probeErr.Error())

						signalCertError(ErrHostNotExpected)
						return
					}
				}
				if tcpPipe != nil {
					tcpPipe.Close()
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(1 * time.Second):
				}
			}
		}
	}()

	return certErrCh, nil
}
