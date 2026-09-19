package nativerestore

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-x/pbsx-host-agent/internal/agent"
	restorepxar "github.com/pbs-x/pbsx-host-agent/internal/pxar"
)

type Job struct {
	ID          string                    `json:"run-id"`
	HostID      string                    `json:"host-id"`
	Datastore   string                    `json:"store"`
	Namespace   string                    `json:"ns,omitempty"`
	BackupTime  int64                     `json:"backup-time"`
	Sources     []string                  `json:"sources"`
	Destination string                    `json:"destination"`
	Mode        string                    `json:"mode,omitempty"`
	OnProgress  func(files, bytes uint64) `json:"-"`
}

func Run(ctx context.Context, job Job) error {
	enrolled, err := agent.AgentHostID()
	if err != nil {
		return fmt.Errorf("read enrolled host ID: %w", err)
	}
	if job.HostID != enrolled {
		return fmt.Errorf("restore host %q does not match enrolled host %q", job.HostID, enrolled)
	}
	if strings.TrimSpace(job.Datastore) == "" || job.BackupTime <= 0 {
		return fmt.Errorf("restore datastore and backup time are required")
	}
	if strings.TrimSpace(job.Destination) == "" {
		return fmt.Errorf("restore destination is required")
	}
	if !filepath.IsAbs(job.Destination) {
		return fmt.Errorf("restore destination must be an absolute host path")
	}
	if len(job.Sources) == 0 {
		job.Sources = []string{"/"}
	}
	for i, source := range job.Sources {
		normalized, err := normalizeArchivePath(source)
		if err != nil {
			return fmt.Errorf("restore source %q: %w", source, err)
		}
		job.Sources[i] = normalized
	}

	baseURL, authToken, tlsConfig, err := agent.PBSConnectionInfo()
	if err != nil {
		return err
	}
	reader, err := restorepxar.NewPBSArchiveReader(ctx, backupproxy.PBSConfig{
		BaseURL: baseURL, Datastore: job.Datastore, AuthToken: authToken,
		Namespace: job.Namespace, TLSConfig: tlsConfig,
	}, job.HostID, job.BackupTime)
	if err != nil {
		return err
	}
	client, errs := restorepxar.NewLocalClient(reader, job.ID)
	defer client.Close()

	mode := restorepxar.RestoreModeNormal
	switch strings.ToLower(strings.TrimSpace(job.Mode)) {
	case "", "normal":
	case "noattr", "no-attr":
		mode = restorepxar.RestoreModeNoAttr
	default:
		return fmt.Errorf("unsupported native restore mode %q", job.Mode)
	}
	restoreErr := restorepxar.RestoreWithOptions(ctx, client, job.Sources, restorepxar.RestoreOptions{
		Mode:    mode,
		DestDir: job.Destination,
		OnProgress: func(progress restorepxar.RestoreProgress) {
			if job.OnProgress != nil {
				job.OnProgress(progress.Files, progress.Bytes)
			}
		},
	})
	var reported []error
	for {
		select {
		case err := <-errs:
			if err != nil {
				reported = append(reported, err)
			}
		default:
			if restoreErr != nil {
				reported = append(reported, restoreErr)
			}
			if len(reported) != 0 {
				return fmt.Errorf("native PBS restore completed with errors: %w", errors.Join(reported...))
			}
			return nil
		}
	}
}

func normalizeArchivePath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
	if value == "" || value == "." {
		return "/", nil
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return "", fmt.Errorf("parent traversal is not allowed")
		}
	}
	cleaned := path.Clean("/" + strings.TrimPrefix(value, "/"))
	if cleaned == "/." {
		cleaned = "/"
	}
	return cleaned, nil
}
