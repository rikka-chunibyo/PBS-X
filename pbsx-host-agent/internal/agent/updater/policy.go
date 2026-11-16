package updater

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver"
)

// DefaultMinConstraint belongs to PBS-X's version line. The -0 lower bound
// deliberately permits our explicitly published preview builds.
const DefaultMinConstraint = ">= 0.6.0-0"

func validateCandidate(current, offered, constraint string) error {
	if strings.TrimSpace(offered) == "" {
		return fmt.Errorf("%w: server returned no release", ErrAlreadyLatest)
	}
	latest, err := semver.NewVersion(offered)
	if err != nil {
		return fmt.Errorf("parse offered version: %w", err)
	}
	installed, err := semver.NewVersion(current)
	if err != nil {
		return fmt.Errorf("cannot safely update unknown installed version %q: %w", current, err)
	}
	if !latest.GreaterThan(installed) {
		return fmt.Errorf("%w: offered version %s is not newer than %s", ErrAlreadyLatest, offered, current)
	}
	if constraint == "" {
		constraint = DefaultMinConstraint
	}
	minimum, err := semver.NewConstraint(constraint)
	if err != nil {
		return fmt.Errorf("invalid release constraint: %w", err)
	}
	if !minimum.Check(latest) {
		return fmt.Errorf("version %s does not meet constraint %s", offered, constraint)
	}
	return nil
}
