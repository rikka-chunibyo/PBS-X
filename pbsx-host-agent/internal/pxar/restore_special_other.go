//go:build !linux

package pxar

import (
	"context"
	"fmt"
)

func restoreSpecial(_ context.Context, _ *restoreState, job restoreJob) error {
	return fmt.Errorf("special-file restore is unsupported on this platform: %s", job.dest)
}
