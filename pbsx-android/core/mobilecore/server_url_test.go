package mobilecore

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"
	"testing"
)

func TestCanonicalServerOrigin(t *testing.T) {
	for _, item := range [][2]string{
		{"https://PBS.example/", "https://pbs.example"},
		{"https://pbs.example:443", "https://pbs.example"},
		{"https://pbs.example:0443/", "https://pbs.example"},
		{"https://pbs.example:8007/", "https://pbs.example:8007"},
		{"https://[::1]/", "https://[::1]"},
		{"https://[::1]:8007/", "https://[::1]:8007"},
	} {
		got, err := CanonicalServerURL(item[0])
		if err != nil || got != item[1] {
			t.Fatalf("%s: got %s, %v; want %s", item[0], got, err, item[1])
		}
	}
	for _, value := range []string{"http://pbs.example", "https://user@pbs.example", "https://pbs.example/path", "https://pbs.example?x=1", "https://pbs.example?", "https://pbs.example#secret", "https://pbs.example:0", "https://pbs.example:65536", "https://:443"} {
		if _, err := CanonicalServerURL(value); err == nil {
			t.Fatalf("accepted invalid origin %s", value)
		}
	}
}

func TestBackupReaderAndMetadataUseSameHTTPSPort(t *testing.T) {
	for _, port := range []string{"", ":443", ":8007"} {
		t.Run(port, func(t *testing.T) {
			c, err := NewClient(`{"server":"https://127.0.0.1` + port + `","host_id":"audit-phone","fingerprint":"` + strings.Repeat("00", 32) + `","auth_id":"audit@pbs!phone","secret":"dummy"}`)
			if err != nil {
				t.Fatal(err)
			}
			target := ""
			c.dialer = &net.Dialer{Control: func(_ string, address string, _ syscall.RawConn) error {
				target = address
				return errors.New("test stopped before connecting")
			}}
			_, _ = c.request(context.Background(), "GET", "/admin/datastore", nil, true)
			want := target
			if want == "" {
				t.Fatal("metadata did not attempt a connection")
			}
			target = ""
			_, _ = c.Backup("backup", "audit", &fsDocuments{root: t.TempDir()}, nil)
			if target != want {
				t.Fatalf("metadata uses %s, backup uses %s", want, target)
			}
			target = ""
			_, _ = c.ListArchive("backup", "audit", "audit-phone", 123, ".", 0, 10)
			if target != want {
				t.Fatalf("metadata uses %s, reader uses %s", want, target)
			}
		})
	}
}
