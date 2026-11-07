package mobilecore

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

var backupID = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)
var catalogueID = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$`)

func validateGroup(kind, id string) error {
	if kind != "host" && kind != "vm" && kind != "ct" {
		return errors.New("unsupported PBS backup type")
	}
	if !backupID.MatchString(id) || safePath(id) != nil {
		return errors.New("invalid backup group")
	}
	return nil
}

type backupCatalogueGroup struct {
	Store     string `json:"store"`
	Namespace string `json:"namespace"`
	Type      string `json:"backup_type"`
	ID        string `json:"backup_id"`
	Last      int64  `json:"last_backup"`
	Count     int64  `json:"backup_count"`
	Comment   string `json:"comment"`
}
type backupCatalogue struct {
	Groups   []backupCatalogueGroup `json:"groups"`
	Warnings []string               `json:"warnings"`
}

func decodeCatalogueList(raw []byte, target any) error {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	if len(envelope.Data) == 0 || envelope.Data[0] != '[' {
		return errors.New("server did not return a list")
	}
	return json.Unmarshal(envelope.Data, target)
}

// ListAccessibleBackups discovers datastores, namespaces and groups using only
// the storage credential. Local jobs and control-host identity do not filter it.
// Actual restore authorization remains enforced by the PBS reader.
func (c *Client) ListAccessibleBackups() (string, error) {
	ctx, done, err := c.begin()
	if err != nil {
		return "", err
	}
	defer done()
	out := backupCatalogue{Groups: []backupCatalogueGroup{}, Warnings: []string{}}
	raw, err := c.request(ctx, "GET", "/admin/datastore", nil, true)
	if err != nil {
		return "", err
	}
	var stores struct {
		Data []struct {
			Store string `json:"store"`
		} `json:"data"`
	}
	if err := decodeCatalogueList(raw, &stores.Data); err != nil {
		return "", fmt.Errorf("invalid datastore list: %w", err)
	}
	seenStores := map[string]bool{}
	seenGroups := map[string]bool{}
	locations := 0
	metadataBytes := 0
	tolerate := func(err error, label string) bool {
		var httpError *responseError
		if !errors.As(err, &httpError) || httpError.status == 401 {
			return false
		}
		if httpError.status != 403 {
			out.Warnings = append(out.Warnings, fmt.Sprintf("Could not list %s (HTTP %d)", label, httpError.status))
		}
		return true
	}
	for _, store := range stores.Data {
		if !catalogueID.MatchString(store.Store) || store.Store == "." {
			return "", errors.New("server returned an invalid datastore name")
		}
		if seenStores[store.Store] {
			continue
		}
		seenStores[store.Store] = true
		base := "/admin/datastore/" + url.PathEscape(store.Store)
		raw, err := c.request(ctx, "GET", base+"/namespace?max-depth=7", nil, true)
		namespaces := map[string]bool{"": true}
		if err != nil {
			if !tolerate(err, "namespaces in "+store.Store) {
				return "", err
			}
		} else {
			var listing struct {
				Data []struct {
					NS string `json:"ns"`
				} `json:"data"`
			}
			if err := decodeCatalogueList(raw, &listing.Data); err != nil {
				return "", fmt.Errorf("invalid namespace list: %w", err)
			}
			for _, ns := range listing.Data {
				if ns.NS != "" && (ns.NS == "." || safePath(ns.NS) != nil || strings.Count(ns.NS, "/") >= 7) {
					return "", errors.New("server returned an invalid namespace")
				}
				namespaces[ns.NS] = true
			}
		}
		names := make([]string, 0, len(namespaces))
		for name := range namespaces {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, ns := range names {
			locations++
			if locations > 4096 {
				return "", errors.New("backup catalogue exceeds 4,096 namespaces")
			}
			endpoint := base + "/groups"
			if ns != "" {
				endpoint += "?" + url.Values{"ns": {ns}}.Encode()
			}
			raw, err := c.request(ctx, "GET", endpoint, nil, true)
			if err != nil {
				if tolerate(err, store.Store+"/"+ns) {
					continue
				}
				return "", err
			}
			var listing struct {
				Data []struct {
					Type    string `json:"backup-type"`
					ID      string `json:"backup-id"`
					Last    int64  `json:"last-backup"`
					Count   int64  `json:"backup-count"`
					Comment string `json:"comment"`
				} `json:"data"`
			}
			if err := decodeCatalogueList(raw, &listing.Data); err != nil {
				return "", fmt.Errorf("invalid backup group list: %w", err)
			}
			for _, group := range listing.Data {
				if err := validateGroup(group.Type, group.ID); err != nil {
					return "", err
				}
				key := store.Store + "\x00" + ns + "\x00" + group.Type + "\x00" + group.ID
				if seenGroups[key] {
					continue
				}
				seenGroups[key] = true
				metadataBytes += len(key) + len(group.Comment) + 128
				if metadataBytes > 8<<20 {
					return "", errors.New("backup catalogue metadata exceeds 8 MiB")
				}
				out.Groups = append(out.Groups, backupCatalogueGroup{Store: store.Store, Namespace: ns, Type: group.Type, ID: group.ID, Last: group.Last, Count: group.Count, Comment: group.Comment})
				if len(out.Groups) > 20000 {
					return "", errors.New("backup catalogue exceeds 20,000 groups")
				}
			}
		}
	}
	sort.Slice(out.Groups, func(i, j int) bool {
		a, b := out.Groups[i], out.Groups[j]
		if a.Last != b.Last {
			return a.Last > b.Last
		}
		if a.Store != b.Store {
			return a.Store < b.Store
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		return a.ID < b.ID
	})
	raw, err = json.Marshal(out)
	return string(raw), err
}
