package desktopcontrol

import (
	"context"
	"testing"
)

func TestRejectsUnsupportedPrivilegedActions(t *testing.T) {
	for _, value := range []string{"", "start; rm -rf /", "setup", "/bin/sh", "restore arbitrary"} {
		if ValidAction(value) {
			t.Fatalf("accepted action %q", value)
		}
		if (Controller{}).Service(context.Background(), value) == nil {
			t.Fatalf("executed action %q", value)
		}
	}
}

func TestInvalidEnrollmentNeverInvokesAgent(t *testing.T) {
	if (Controller{}).Enroll(context.Background(), "not-an-enrollment-code") == nil {
		t.Fatal("invalid code accepted")
	}
}
