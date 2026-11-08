package lifecycle

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/pbs-x/pbsx-host-agent/internal/agent/registry"
)

func TestPendingEnrollmentReplacesExistingIdentity(t *testing.T) {
	pending := true
	identity := "old"
	enrollments, renewals := 0, 0
	actions := bootstrapActions{
		lookup: func(path, key string, _ bool) (*registry.RegistryEntry, error) {
			if path == registry.CONFIG {
				if pending {
					return &registry.RegistryEntry{Value: "new-enrollment-code"}, nil
				}
				return nil, registry.ErrNotFound
			}
			return &registry.RegistryEntry{Value: identity}, nil
		},
		renew: func() error {
			renewals++
			if identity != "new" {
				t.Fatal("old identity was reused while new enrollment was pending")
			}
			return nil
		},
		enroll: func() error { enrollments++; identity = "new"; pending = false; return nil },
	}
	if err := waitForBootstrap(t.Context(), actions); err != nil {
		t.Fatal(err)
	}
	if enrollments != 1 || renewals != 1 {
		t.Fatalf("enrollments=%d renewals=%d", enrollments, renewals)
	}
}

func TestFailedPendingEnrollmentDoesNotReuseOldIdentity(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	attempted := false
	actions := bootstrapActions{
		lookup: func(path, key string, _ bool) (*registry.RegistryEntry, error) {
			if path == registry.CONFIG {
				return &registry.RegistryEntry{Value: "invalid-enrollment-code"}, nil
			}
			return &registry.RegistryEntry{Value: "old"}, nil
		},
		renew:  func() error { t.Fatal("failed enrollment fell back to the old identity"); return nil },
		enroll: func() error { attempted = true; cancel(); return errors.New("invalid enrollment token") },
	}
	if err := waitForBootstrap(ctx, actions); !errors.Is(err, context.Canceled) || !attempted {
		t.Fatalf("attempted=%t, err=%v", attempted, err)
	}
}

func TestBootstrapRegistryFailureIsNotTreatedAsMissingToken(t *testing.T) {
	actions := bootstrapActions{
		lookup: func(string, string, bool) (*registry.RegistryEntry, error) { return nil, io.ErrUnexpectedEOF },
		renew:  func() error { t.Fatal("unreadable enrollment state allowed old credentials"); return nil },
		enroll: func() error { t.Fatal("unreadable enrollment state triggered a network request"); return nil },
	}
	if err := waitForBootstrap(t.Context(), actions); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
}

func TestExistingIdentityWithoutPendingEnrollmentIsReused(t *testing.T) {
	actions := bootstrapActions{
		lookup: func(path, key string, _ bool) (*registry.RegistryEntry, error) {
			if path == registry.CONFIG {
				return nil, registry.ErrNotFound
			}
			return &registry.RegistryEntry{Value: "existing"}, nil
		},
		renew:  func() error { return nil },
		enroll: func() error { t.Fatal("existing identity unexpectedly re-enrolled"); return nil },
	}
	if err := waitForBootstrap(t.Context(), actions); err != nil {
		t.Fatal(err)
	}
}
