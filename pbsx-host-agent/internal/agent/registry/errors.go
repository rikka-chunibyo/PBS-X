package registry

import "errors"

// ErrNotFound distinguishes an absent optional value from registry I/O failures.
var ErrNotFound = errors.New("registry entry not found")
