package enrollment

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestEnrollmentStorageCredentialOption(t *testing.T) {
	var payload map[string]any
	_ = json.Unmarshal([]byte(testPayload), &payload)
	payload["storage_auth_id"] = "restore@pbs!shared"
	payload["storage_secret"] = "example-secret"
	raw, _ := json.Marshal(payload)
	values, err := Decode(Prefix + base64.RawURLEncoding.EncodeToString(raw))
	if err != nil || values.StorageAuthID != "restore@pbs!shared" || values.StorageSecret != "example-secret" {
		t.Fatal("custom enrollment credential not preserved")
	}
	delete(payload, "storage_secret")
	raw, _ = json.Marshal(payload)
	if _, err := Decode(Prefix + base64.RawURLEncoding.EncodeToString(raw)); err == nil {
		t.Fatal("partial credential accepted")
	}
}

const testPayload = `{"v":1,"host_id":"test-host","server_url":"https://pbs.example:8007","bootstrap_token":"abc123","server_ca_fingerprint":"aa:bb"}`

func TestDecode(t *testing.T) {
	code := Prefix + base64.RawURLEncoding.EncodeToString([]byte(testPayload))
	values, err := Decode(code)
	if err != nil {
		t.Fatal(err)
	}
	if values.HostID != "test-host" || values.ServerURL != "https://pbs.example:8007" ||
		values.BootstrapToken != "abc123" || values.ServerCAFingerprint != "aa:bb" {
		t.Fatalf("unexpected values: %#v", values)
	}
}

func TestDecodeRejectsInvalidCodes(t *testing.T) {
	tests := []string{
		"not-an-enrollment-code",
		Prefix + "not-base64!",
		Prefix + base64.RawURLEncoding.EncodeToString([]byte(`{"v":2}`)),
		Prefix + base64.RawURLEncoding.EncodeToString([]byte(`{"v":1}`)),
		Prefix + base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"host_id":"test","server_url":"http://pbs.example","bootstrap_token":"abc","server_ca_fingerprint":"aa"}`)),
	}
	for _, code := range tests {
		if _, err := Decode(code); err == nil {
			t.Fatalf("expected error for %q", code)
		}
	}
}
