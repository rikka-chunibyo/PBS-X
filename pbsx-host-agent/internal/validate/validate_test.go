package validate

import (
	"testing"

	"github.com/pbs-x/pbsx-host-agent/internal/conf"
)

func TestDefaultExclusionsPassValidation(t *testing.T) {
	for _, path := range conf.DefaultExclusions {
		if err := ValidateExclusionPath(path); err != nil {
			t.Errorf("default exclusion %q fails validation: %v", path, err)
		}
	}
}
