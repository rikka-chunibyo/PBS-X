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
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/buzhash"
	"github.com/pbs-plus/pxar/datastore"
)

type configuration struct {
	Server        string `json:"server"`
	HostID        string `json:"host_id"`
	Fingerprint   string `json:"fingerprint"`
	AuthID        string `json:"auth_id"`
	Secret        string `json:"secret"`
	StorageAuthID string `json:"storage_auth_id,omitempty"`
	StorageSecret string `json:"storage_secret,omitempty"`
	Certificate   string `json:"certificate,omitempty"`
	PrivateKey    string `json:"private_key,omitempty"`
}

func init() {
	if runtime.GOOS == "android" {
		runtime.GOMAXPROCS(2)
		debug.SetMemoryLimit(192 << 20)
	}
}

type Client struct {
	cfg    configuration
	tls    *tls.Config
	mu     sync.Mutex
	cancel context.CancelFunc
	dialer *net.Dialer
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

func NewClient(configJSON string) (*Client, error) {
	var c configuration
	if err := json.Unmarshal([]byte(configJSON), &c); err != nil {
		return nil, errors.New("invalid connection settings")
	}
	u, err := url.Parse(c.Server)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("server must be an HTTPS origin, e.g. https://pbs:8007")
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
	pin, err := hex.DecodeString(strings.ReplaceAll(c.Fingerprint, ":", ""))
	if err != nil || len(pin) != 32 {
		return nil, errors.New("a SHA-256 server certificate fingerprint is required")
	}
	c.Server = strings.TrimRight(c.Server, "/")
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
	return &Client{cfg: c, tls: tlsConfig}, nil
}

func (c *Client) begin() (context.Context, func(), error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		return nil, nil, errors.New("another operation is running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	return ctx, func() { cancel(); c.mu.Lock(); c.cancel = nil; c.mu.Unlock() }, nil
}
func (c *Client) Cancel() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
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
		return nil, fmt.Errorf("PBS request failed (%d); check credentials, datastore, namespace and the host backup job", response.StatusCode)
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
	body, _ := json.Marshal(map[string]any{"host-id": e.HostID, "token": e.Token, "client-certificate": cert, "heartbeat": map[string]any{"hostname": hostname, "operating-system": "other", "os-version": "Android", "architecture": runtime.GOARCH, "agent-version": "android-0.1.0", "capabilities": []string{"android-documents", "manual-backup", "restore"}}})
	response, err := c.request(context.Background(), "POST", "/pbsx/agent/bootstrap", body, false)
	if err != nil {
		return "", err
	}
	var result struct {
		Data struct {
			ID     string `json:"backup-auth-id"`
			Secret string `json:"backup-secret"`
		} `json:"data"`
	}
	if json.Unmarshal(response, &result) != nil || result.Data.ID == "" || result.Data.Secret == "" {
		return "", errors.New("enrollment did not return an API key")
	}
	cfg.AuthID, cfg.Secret = result.Data.ID, result.Data.Secret
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
	if _, err := c.pbsConfig(store, namespace); err != nil {
		return "", err
	}
	if sourceHost == "" {
		sourceHost = c.cfg.HostID
	}
	ctx, done, err := c.begin()
	if err != nil {
		return "", err
	}
	defer done()
	q := url.Values{"backup-type": {"host"}, "backup-id": {sourceHost}}
	if namespace != "" {
		q.Set("ns", namespace)
	}
	raw, err := c.request(ctx, "GET", "/admin/datastore/"+url.PathEscape(store)+"/snapshots?"+q.Encode(), nil, true)
	return string(raw), err
}

func (c *Client) Backup(store, namespace string, docs Documents, progress Progress) (string, error) {
	cfg, err := c.pbsConfig(store, namespace)
	if err != nil {
		return "", err
	}
	ctx, done, err := c.begin()
	if err != nil {
		return "", err
	}
	defer done()
	stamp := time.Now().Unix()
	client := backupproxy.NewLocalClient(&documentFS{docs: docs, ctx: ctx})
	server := backupproxy.NewServer(client, backupproxy.NewPBSStore(cfg, buzhash.DefaultConfig(), true))
	_, err = server.RunBackupWithMode(ctx, ".", backupproxy.BackupConfig{BackupType: datastore.BackupHost, BackupID: c.cfg.HostID, Namespace: namespace, BackupTime: stamp, ChunkConfig: buzhash.DefaultConfig(), DetectionMode: backupproxy.DetectionData, Compress: true, StrictMetadata: true, OnUploadProgress: func(p backupproxy.UploadProgress) {
		if progress != nil {
			progress.Update("Uploading", int64(p.UploadedChunks), int64(p.UploadedBytes))
		}
	}})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("host/%s/%s", c.cfg.HostID, time.Unix(stamp, 0).UTC().Format("2006-01-02T15:04:05Z")), nil
}
