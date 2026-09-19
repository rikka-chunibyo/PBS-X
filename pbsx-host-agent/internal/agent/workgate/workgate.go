package workgate

import "sync"

// Mu serializes destructive/heavy host operations claimed from PBS-X.
// Keeping the lock process-local is sufficient because the service itself is
// single-instance and the server durably re-delivers claimed work after a crash.
var Mu sync.Mutex
