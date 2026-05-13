// Package tenant defines Tabula tenant metadata and filesystem layout.
//
// A tenant is an isolation namespace for config, runtime state, cache, logs,
// and installed bundle surfaces. It is not an authorization, billing, or quota
// object.
package tenant

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const DefaultID = "default"

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

var reservedIDs = map[string]struct{}{
	"admin":   {},
	"kernel":  {},
	"runtime": {},
	"system":  {},
}

var (
	ErrInvalidID  = errors.New("invalid tenant id")
	ErrReservedID = errors.New("reserved tenant id")
)

// Tenant is the persisted metadata for one Tabula tenant.
type Tenant struct {
	ID          string
	DisplayName string
	CreatedAt   time.Time
}

// ValidateID enforces the canonical tenant id grammar.
func ValidateID(id string) error {
	id = strings.TrimSpace(id)
	if !idPattern.MatchString(id) {
		return fmt.Errorf("%w: %q must match %s", ErrInvalidID, id, idPattern.String())
	}
	if _, ok := reservedIDs[id]; ok {
		return fmt.Errorf("%w: %q", ErrReservedID, id)
	}
	return nil
}

func normalize(t Tenant, now time.Time) (Tenant, error) {
	t.ID = strings.TrimSpace(t.ID)
	t.DisplayName = strings.TrimSpace(t.DisplayName)
	if err := ValidateID(t.ID); err != nil {
		return Tenant{}, err
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now.UTC()
	} else {
		t.CreatedAt = t.CreatedAt.UTC()
	}
	return t, nil
}
