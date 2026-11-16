//go:build windows

package registry

import (
	"fmt"
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
	winregistry "golang.org/x/sys/windows/registry"
)

func TestCredentialRegistryExcludesOrdinaryReaders(t *testing.T) {
	if !windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("requires an Administrator test process")
	}
	path := fmt.Sprintf(`Software\PBS-X\ACL-test-%d`, os.Getpid())
	if err := protectCredentialKey(path, true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = winregistry.DeleteKey(winregistry.LOCAL_MACHINE, path) })
	key, err := winregistry.OpenKey(winregistry.LOCAL_MACHINE, path, winregistry.READ|winregistry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Close()
	if err := key.SetStringValue("fixture", "not-a-credential"); err != nil {
		t.Fatal(err)
	}
	// Hardening a pre-existing key must retain its values as well.
	if err := protectCredentialKey(path, false); err != nil {
		t.Fatal(err)
	}
	value, _, err := key.GetStringValue("fixture")
	if err != nil || value != "not-a-credential" {
		t.Fatalf("hardening changed existing values: %v", err)
	}
	sd, err := windows.GetSecurityInfo(windows.Handle(key), windows.SE_REGISTRY_KEY, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("vault inherits parent permissions: %v", err)
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount != 2 {
		t.Fatalf("vault does not have exactly two restricted ACEs: %v", err)
	}
	seen := map[string]bool{}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			t.Fatal(err)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || uint32(ace.Mask) != winregistry.ALL_ACCESS ||
			(sid != "S-1-5-18" && sid != "S-1-5-32-544") {
			t.Fatalf("unexpected vault ACL entry for %s", sid)
		}
		seen[sid] = true
	}
	if !seen["S-1-5-18"] || !seen["S-1-5-32-544"] {
		t.Fatal("SYSTEM or Administrators missing from vault ACL")
	}
}

func TestLegacySecretCannotWriteArbitraryRegistryPaths(t *testing.T) {
	if err := writeSecretDPAPIToRegistry(`Software\Microsoft\Windows\CurrentVersion\Run`, "fixture", "not-a-secret"); err == nil {
		t.Fatal("legacy secret import accepted an arbitrary registry location")
	}
}
