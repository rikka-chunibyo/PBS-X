package mobilecore

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/buzhash"
	"github.com/pbs-plus/pxar/datastore"
)

type configuration struct {
	DefaultStore      string   `json:"default_store,omitempty"`
	DefaultNamespace  *string  `json:"default_namespace,omitempty"`
	Server            string   `json:"server"`
	HostID            string   `json:"host_id"`
	Fingerprint       string   `json:"fingerprint"`
	AuthID            string   `json:"auth_id"`
	Secret            string   `json:"secret"`
	StorageAuthID     string   `json:"storage_auth_id,omitempty"`
	StorageSecret     string   `json:"storage_secret,omitempty"`
	Certificate       string   `json:"certificate,omitempty"`
	PrivateKey        string   `json:"private_key,omitempty"`
	EncryptionEnabled bool     `json:"encryption_enabled,omitempty"`
	EncryptionKey     string   `json:"encryption_key,omitempty"`
	DecryptionKeys    []string `json:"decryption_keys,omitempty"`
}

func init() {
	if runtime.GOOS == "android" {
		runtime.GOMAXPROCS(2)
		debug.SetMemoryLimit(192 << 20)
	}
}

type Client struct {
	cfg       configuration
	tls       *tls.Config
	mu        sync.Mutex
	cancel    context.CancelFunc
	operation uint64
	control   int // 0 running, 1 pause, 2 cancel
	resumable bool
	dialer    *net.Dialer
	details   ProgressDetails
	crypt     *datastore.CryptConfig
	keys      map[string]*datastore.CryptConfig
}

type responseError struct{ status int }

func (e *responseError) Error() string {
	return fmt.Sprintf("PBS request failed (%d); check credentials and datastore/namespace permissions", e.status)
}

// SocketBinder binds an unconnected TCP socket to the selected Android network.
// Java must duplicate the borrowed descriptor, bind it, then close the duplicate.
type SocketBinder interface{ BindSocket(fd int64) error }

// SetSocketBinder must be called before starting an operation. A failed binding
// aborts the connection, so a Wi-Fi-only operation cannot fall back to cellular.
func (c *Client) SetSocketBinder(binder SocketBinder) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		return errors.New("an operation is running")
	}
	if binder == nil {
		c.dialer = nil
		return nil
	}
	c.dialer = &net.Dialer{Timeout: 30 * time.Second, Control: func(network, address string, raw syscall.RawConn) error {
		var bindErr error
		if err := raw.Control(func(fd uintptr) { bindErr = binder.BindSocket(int64(fd)) }); err != nil {
			return err
		}
		return bindErr
	}}
	return nil
}

// CanonicalServerURL is the HTTPS origin identity shared with Android history
// validation. Explicit/default HTTPS ports and host casing must compare equally.
func CanonicalServerURL(server string) (string, error) {
	u, err := url.Parse(server)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("server must be an HTTPS origin, e.g. https://pbs:8007")
	}
	host := strings.ToLower(u.Hostname())
	port := 443
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return "", errors.New("invalid HTTPS port")
		}
	}
	if port == 443 {
		u.Host = host
		if strings.Contains(host, ":") {
			u.Host = "[" + host + "]"
		}
	} else {
		u.Host = net.JoinHostPort(host, strconv.Itoa(port))
	}
	u.Path, u.RawPath = "", ""
	return u.String(), nil
}

func NewClient(configJSON string) (*Client, error) {
	var c configuration
	if err := json.Unmarshal([]byte(configJSON), &c); err != nil {
		return nil, errors.New("invalid connection settings")
	}
	var err error
	c.Server, err = CanonicalServerURL(c.Server)
	if err != nil {
		return nil, err
	}

	if !regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{1,63}$`).MatchString(c.HostID) {
		return nil, errors.New("invalid host ID")
	}
	if (c.AuthID == "") != (c.Secret == "") || (c.StorageAuthID == "") != (c.StorageSecret == "") {
		return nil, errors.New("API key requires both auth ID and secret")
	}
	for _, s := range []string{c.AuthID, c.Secret, c.StorageAuthID, c.StorageSecret} {
		if strings.ContainsAny(s, "\r\n") {
			return nil, errors.New("invalid API credential")
		}
	}
	if c.DefaultStore != "" && !regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`).MatchString(c.DefaultStore) {
		return nil, errors.New("invalid default datastore")
	}
	if c.DefaultNamespace != nil && *c.DefaultNamespace != "" && safePath(*c.DefaultNamespace) != nil {
		return nil, errors.New("invalid default namespace")
	}
	pin, err := hex.DecodeString(strings.ReplaceAll(c.Fingerprint, ":", ""))
	if err != nil || len(pin) != 32 {
		return nil, errors.New("a SHA-256 server certificate fingerprint is required")
	}
	// Exact leaf pinning deliberately replaces CA/hostname trust, matching the
	// out-of-band PBS-X enrollment fingerprint. Never accept an unpinned peer.
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true, VerifyConnection: func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return errors.New("server did not present a certificate")
		}
		cert := cs.PeerCertificates[0]
		sum := sha256.Sum256(cert.Raw)
		if subtle.ConstantTimeCompare(sum[:], pin) != 1 {
			return errors.New("server certificate fingerprint does not match")
		}
		now := time.Now()
		if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
			return errors.New("server certificate has expired or is not valid yet")
		}
		return nil
	}}
	crypt, keys, err := connectionKeys(c)
	if err != nil {
		return nil, err
	}
	return &Client{cfg: c, tls: tlsConfig, crypt: crypt, keys: keys}, nil
}

func (c *Client) begin() (context.Context, func(), error) { return c.beginMode(false) }

func (c *Client) beginMode(resumable bool) (context.Context, func(), error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		return nil, nil, errors.New("another operation is running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.operation++
	c.control, c.resumable = 0, resumable
	return ctx, func() { cancel(); c.mu.Lock(); c.cancel = nil; c.resumable = false; c.mu.Unlock() }, nil
}
func (c *Client) Cancel() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.control = 2
		c.cancel()
	}
}

func (c *Client) request(ctx context.Context, method, endpoint string, body []byte, storage bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.Server+"/api2/json"+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	id, secret := c.cfg.AuthID, c.cfg.Secret
	if storage && c.cfg.StorageAuthID != "" {
		id, secret = c.cfg.StorageAuthID, c.cfg.StorageSecret
	}
	if id != "" {
		req.Header.Set("Authorization", "PBSAPIToken="+id+":"+secret)
	}
	tr := &http.Transport{TLSClientConfig: c.tls}
	if c.dialer != nil {
		tr.DialContext = c.dialer.DialContext
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("server redirect refused") }}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, (16<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 16<<20 {
		return nil, errors.New("server response too large")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &responseError{status: response.StatusCode}
	}
	return raw, nil
}

// Enroll consumes a one-time PBS-X code and returns settings to encrypt on-device.
// A new identity is generated locally; its private key never leaves the device.
func Enroll(code, hostname string) (string, error) {
	code = strings.TrimSpace(code)
	if !strings.HasPrefix(code, "pbsx1.") || len(code) > 24*1024 {
		return "", errors.New("invalid PBS-X enrollment code")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, "pbsx1."))
	if err != nil {
		return "", errors.New("invalid enrollment encoding")
	}
	var e struct {
		Version       int    `json:"v"`
		HostID        string `json:"host_id"`
		Server        string `json:"server_url"`
		Token         string `json:"bootstrap_token"`
		Fingerprint   string `json:"server_ca_fingerprint"`
		StorageAuthID string `json:"storage_auth_id"`
		StorageSecret string `json:"storage_secret"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Version != 1 || e.Token == "" {
		return "", errors.New("incomplete or unsupported enrollment code")
	}
	cfg := configuration{Server: e.Server, HostID: e.HostID, Fingerprint: e.Fingerprint, StorageAuthID: e.StorageAuthID, StorageSecret: e.StorageSecret}
	initial, _ := json.Marshal(cfg)
	c, err := NewClient(string(initial))
	if err != nil {
		return "", err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", err
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: e.HostID}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(10, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return "", err
	}
	cert := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", err
	}
	cfg.Certificate = cert
	cfg.PrivateKey = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}))
	body, _ := json.Marshal(map[string]any{"host-id": e.HostID, "token": e.Token, "client-certificate": cert, "heartbeat": map[string]any{"hostname": hostname, "operating-system": "other", "os-version": "Android", "architecture": runtime.GOARCH, "agent-version": "android-0.5.3-preview", "capabilities": []string{"android-documents", "manual-backup", "restore"}}})
	response, err := c.request(context.Background(), "POST", "/pbsx/agent/bootstrap", body, false)
	if err != nil {
		return "", err
	}
	var result struct {
		Data struct {
			ID        string  `json:"backup-auth-id"`
			Store     string  `json:"store"`
			Namespace *string `json:"ns"`
			Secret    string  `json:"backup-secret"`
		} `json:"data"`
	}
	if json.Unmarshal(response, &result) != nil || result.Data.ID == "" || result.Data.Secret == "" {
		return "", errors.New("enrollment did not return an API key")
	}
	cfg.AuthID, cfg.Secret = result.Data.ID, result.Data.Secret
	// Destination comes from the authenticated server response, never from
	// editable enrollment-code metadata.
	cfg.DefaultStore, cfg.DefaultNamespace = result.Data.Store, result.Data.Namespace
	check, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	if _, err := NewClient(string(check)); err != nil {
		return "", err
	}
	out, err := json.Marshal(cfg)
	return string(out), err
}

func (c *Client) pbsConfig(store, namespace string) (backupproxy.PBSConfig, error) {
	if store == "" || strings.ContainsAny(store, "/\\\x00") {
		return backupproxy.PBSConfig{}, errors.New("invalid datastore name")
	}
	if namespace != "" && safePath(namespace) != nil {
		return backupproxy.PBSConfig{}, errors.New("invalid namespace")
	}
	id, secret := c.cfg.AuthID, c.cfg.Secret
	if c.cfg.StorageAuthID != "" {
		id, secret = c.cfg.StorageAuthID, c.cfg.StorageSecret
	}
	if id == "" {
		return backupproxy.PBSConfig{}, errors.New("enroll or enter an API key first")
	}
	return backupproxy.PBSConfig{BaseURL: c.cfg.Server + "/api2/json", Datastore: store, Namespace: namespace, AuthToken: id + ":" + secret, TLSConfig: c.tls, Dialer: c.dialer}, nil
}

func (c *Client) ListSnapshots(store, namespace, sourceHost string) (string, error) {
	if sourceHost == "" {
		sourceHost = c.cfg.HostID
	}
	return c.ListGroupSnapshots(store, namespace, "host", sourceHost)
}

// ListGroupSnapshots lists any PBS group visible to the selected storage key.
func (c *Client) ListGroupSnapshots(store, namespace, backupType, source string) (string, error) {
	if _, err := c.pbsConfig(store, namespace); err != nil {
		return "", err
	}
	if err := validateGroup(backupType, source); err != nil {
		return "", err
	}
	ctx, done, err := c.begin()
	if err != nil {
		return "", err
	}
	defer done()
	q := url.Values{"backup-type": {backupType}, "backup-id": {source}}
	if namespace != "" {
		q.Set("ns", namespace)
	}
	raw, err := c.request(ctx, "GET", "/admin/datastore/"+url.PathEscape(store)+"/snapshots?"+q.Encode(), nil, true)
	return string(raw), err
}

func (c *Client) Backup(store, namespace string, docs Documents, progress Progress) (string, error) {
	result, err := c.backupNamed(store, namespace, c.cfg.HostID, "", "", docs, progress)
	return result.Snapshot, err
}

type backupResult struct {
	Snapshot string `json:"snapshot"`
	Warning  string `json:"warning,omitempty"`
}

func (c *Client) BackupJob(store, namespace, jobID, jobName string, docs Documents, progress Progress) (string, error) {
	return c.BackupGroupJob(store, namespace, c.cfg.HostID, jobID, jobName, docs, progress)
}

// BackupGroupJob keeps storage history separate from device and local job IDs.
// Equal store/namespace/backupID values share snapshots; their contents are not merged.
func (c *Client) BackupGroupJob(store, namespace, backupID, jobID, jobName string, docs Documents, progress Progress) (string, error) {
	if err := validateGroup("host", backupID); err != nil {
		return "", err
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(jobID) || len(jobName) == 0 || len(jobName) > 400 || strings.ContainsAny(jobName, "\r\n") {
		return "", errors.New("invalid backup job label")
	}
	result, err := c.backupNamed(store, namespace, backupID, jobID, jobName, docs, progress)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(result)
	return string(raw), err
}
func (c *Client) backupNamed(store, namespace, backupID, jobID, jobName string, docs Documents, progress Progress) (backupResult, error) {
	cfg, err := c.pbsConfig(store, namespace)
	if err != nil {
		return backupResult{}, err
	}
	ctx, done, err := c.begin()
	if err != nil {
		return backupResult{}, err
	}
	defer done()
	stamp := time.Now().Unix()
	previous, previousErr := c.previousForBackup(ctx, store, namespace, backupID, stamp)
	report := newTransferReporter(ctx, c.details)
	defer report.close()
	client := backupproxy.NewLocalClient(&documentFS{docs: docs, ctx: ctx, report: report})
	server := backupproxy.NewServer(client, backupproxy.NewPBSStore(cfg, buzhash.DefaultConfig(), true))
	mode := datastore.CryptModeNone
	if c.crypt != nil {
		mode = datastore.CryptModeEncrypt
	}
	_, err = server.RunBackupWithMode(ctx, ".", backupproxy.BackupConfig{PreviousBackup: previous, CryptMode: mode, CryptConfig: c.crypt, BackupType: datastore.BackupHost, BackupID: backupID, Namespace: namespace, BackupTime: stamp, ChunkConfig: buzhash.DefaultConfig(), DetectionMode: backupproxy.DetectionData, Compress: true, StrictMetadata: true, OnUploadProgress: func(p backupproxy.UploadProgress) {
		report.uploaded(int64(p.UploadedBytes))
		if progress != nil {
			progress.Update("Uploading", int64(p.UploadedChunks), int64(p.UploadedBytes))
		}
	}})
	if err != nil {
		return backupResult{}, backupFailure(err, c.cfg.HostID, store, namespace)
	}
	result := backupResult{Snapshot: fmt.Sprintf("host/%s/%s", backupID, time.Unix(stamp, 0).UTC().Format("2006-01-02T15:04:05Z"))}
	if previousErr != nil {
		result.Warning = "Could not inspect the previous snapshot; this run used a full upload"
	}
	if jobID != "" {
		report.phase("Saving snapshot notes", "", -1)
		values := map[string]any{"backup-type": "host", "backup-id": backupID, "backup-time": stamp, "notes": "PBS-X Android: " + jobName + "\nJob ID: " + jobID}
		if namespace != "" {
			values["ns"] = namespace
		}
		body, _ := json.Marshal(values)
		if _, err := c.request(ctx, "PUT", "/admin/datastore/"+url.PathEscape(store)+"/notes", body, true); err != nil {
			if result.Warning != "" {
				result.Warning += "; "
			}
			result.Warning += "Snapshot saved; job label could not be attached"
		}
	}
	return result, nil
}

func backupFailure(err error, host, store, namespace string) error {
	legacy := strings.Contains(err.Error(), "has no enabled backup job targeting this datastore/namespace")
	missingACL := strings.Contains(err.Error(), "HTTP 403") &&
		(strings.Contains(err.Error(), "permission check failed") || strings.Contains(err.Error(), "missing permissions 'Datastore.Backup'"))
	if !legacy && !missingACL {
		return err
	}
	if namespace == "" {
		namespace = "(root)"
	}
	return fmt.Errorf("Backup destination needs setup. Check the storage key's Permissions for datastore %s, namespace %s. For automatic keys, open Hosts → %s → Agent and generate enrollment for that namespace, or grant the key access in PBS Permissions. Then retry Back up; your enrollment is already valid", store, namespace, host)
}
