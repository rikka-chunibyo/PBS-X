package backupproxy

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
)

func TestHTTPSDefaultPortForBackupAndReader(t *testing.T) {
	for _, item := range [][2]string{
		{"https://127.0.0.1/api2/json", "127.0.0.1:443"},
		{"https://127.0.0.1:8007/api2/json", "127.0.0.1:8007"},
		{"https://[::1]/api2/json", "[::1]:443"},
	} {
		t.Run(item[0], func(t *testing.T) {
			target := ""
			dialer := &net.Dialer{Control: func(_ string, address string, _ syscall.RawConn) error {
				target = address
				return errors.New("test stopped before connecting")
			}}
			_, _ = dialPBSH2(context.Background(), item[0], "backup", "dummy", BackupConfig{}, nil, false, dialer)
			if target != item[1] {
				t.Fatalf("backup dialed %s; want %s", target, item[1])
			}
			target = ""
			_, _ = dialPBSReaderH2(context.Background(), PBSConfig{BaseURL: item[0], Datastore: "backup", Dialer: dialer}, "host", "phone", 123)
			if target != item[1] {
				t.Fatalf("reader dialed %s; want %s", target, item[1])
			}
		})
	}
}
