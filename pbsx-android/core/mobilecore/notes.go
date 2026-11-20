package mobilecore

import (
	"context"
	"encoding/json"
	"net/url"
)

// Snapshot notes are plaintext server metadata, even when the archives are
// encrypted. Keep job labels and IDs on the device for encrypted backups.
func (c *Client) saveJobNotes(ctx context.Context, store, namespace, backupID string, stamp int64, jobID, jobName string) error {
	if c.crypt != nil || jobID == "" {
		return nil
	}
	values := map[string]any{"backup-type": "host", "backup-id": backupID, "backup-time": stamp, "notes": "PBS-X Android: " + jobName + "\nJob ID: " + jobID}
	if namespace != "" {
		values["ns"] = namespace
	}
	body, _ := json.Marshal(values)
	_, err := c.request(ctx, "PUT", "/admin/datastore/"+url.PathEscape(store)+"/notes", body, true)
	return err
}
