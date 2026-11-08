//go:build linux

package lifecycle

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kardianos/service"
)

type serviceProbeProgram struct{}

func (*serviceProbeProgram) Start(service.Service) error { return nil }
func (*serviceProbeProgram) Stop(service.Service) error  { return nil }

// Opt in only on a disposable Linux VM. This exercises the actual service
// template's capability set and filesystem view, rather than a root shell.
func TestSystemdBackupRestorePrivileges(t *testing.T) {
	if os.Getenv("PBSX_SYSTEMD_TEST") != "1" {
		t.Skip("set PBSX_SYSTEMD_TEST=1 on a disposable root/systemd VM")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the opted-in systemd test requires root")
	}
	probe, err := os.MkdirTemp("/var/tmp", "pbsx-service-probe-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(probe)
	hostTmp, err := os.MkdirTemp("/tmp", "pbsx-host-view-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(hostTmp)
	if err := os.WriteFile(filepath.Join(hostTmp, "source"), []byte("host-visible\n"), 0600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(probe, "probe.sh")
	body := `#!/bin/sh
set -eu
probe='` + probe + `'
host_tmp='` + hostTmp + `'
etc_probe=/etc/` + filepath.Base(probe) + `
trap 'umount "$probe/mount" 2>/dev/null || true; rm -rf "$etc_probe"' EXIT
test "$(cat "$host_tmp/source")" = host-visible
mkdir "$etc_probe"
printf restored > "$etc_probe/file"
cp /usr/bin/true "$probe/owned"
chown 65534:65534 "$probe/owned"
chmod 6755 "$probe/owned"
test "$(stat -c %u:%g:%a "$probe/owned")" = 65534:65534:6755
setcap cap_net_bind_service=ep "$probe/owned"
getcap "$probe/owned" | grep -q cap_net_bind_service=ep
mkdir "$probe/mount"
mount -t tmpfs -o size=1m tmpfs "$probe/mount"
printf snapshot > "$probe/mount/file"
test "$(cat "$probe/mount/file")" = snapshot
umount "$probe/mount"
grep -E '^(CapEff|CapBnd|NoNewPrivs):' /proc/self/status
printf 'service operations passed\n' > "$probe/result"
`
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	// Keep all filesystem/capability directives; make the probe run just once.
	contents := strings.ReplaceAll(strings.ReplaceAll(SYSTEMD_SCRIPT, "Type=simple", "Type=oneshot"), "Restart=always", "Restart=no")
	name := filepath.Base(probe)
	svc, err := service.New(&serviceProbeProgram{}, &service.Config{Name: name, Description: "PBS-X disposable service probe", Executable: script, UserName: "root", Option: service.KeyValue{"SystemdScript": contents}})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Install(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		svc.Stop()
		if err := svc.Uninstall(); err != nil {
			t.Errorf("remove probe service: %v", err)
		}
	}()
	for _, args := range [][]string{{"daemon-reload"}, {"start", name}} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			journal, _ := exec.Command("journalctl", "-u", name, "--no-pager", "-n", "25").CombinedOutput()
			t.Fatalf("systemctl %v: %v\n%s\n%s", args, err, out, journal)
		}
	}
	if out, err := os.ReadFile(filepath.Join(probe, "result")); err != nil || string(out) != "service operations passed\n" {
		t.Fatalf("probe did not complete: %q %v", out, err)
	}
}
