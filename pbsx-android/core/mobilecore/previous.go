package mobilecore

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/datastore"
)

func (c *Client) previousForBackup(ctx context.Context, store, namespace, id string, before int64) (*backupproxy.PreviousBackupRef, error) {
	q := url.Values{"backup-type": {"host"}, "backup-id": {id}}
	if namespace != "" {
		q.Set("ns", namespace)
	}
	raw, err := c.request(ctx, "GET", "/admin/datastore/"+url.PathEscape(store)+"/snapshots?"+q.Encode(), nil, true)
	if err != nil {
		return nil, err
	}
	return previousSnapshot(raw, namespace, id, before)
}

func previousSnapshot(raw []byte, namespace, id string, before int64) (*backupproxy.PreviousBackupRef, error) {
	var response struct {
		Data []struct {
			Type  string  `json:"backup-type"`
			ID    string  `json:"backup-id"`
			Time  int64   `json:"backup-time"`
			Size  *uint64 `json:"size"`
			Files []struct {
				Name string `json:"filename"`
			} `json:"files"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	var chosen *backupproxy.PreviousBackupRef
	for _, item := range response.Data {
		if item.Type != "host" || item.ID != id || item.Time <= 0 || item.Time >= before || item.Size == nil {
			continue
		}
		archive := false
		for _, file := range item.Files {
			if file.Name == "root.ppxar.didx" || file.Name == "root.pxar.didx" {
				archive = true
			}
		}
		if archive && (chosen == nil || item.Time > chosen.BackupTime) {
			chosen = &backupproxy.PreviousBackupRef{BackupType: datastore.BackupHost, BackupID: id, Namespace: namespace, BackupTime: item.Time}
		}
	}
	return chosen, nil
}
