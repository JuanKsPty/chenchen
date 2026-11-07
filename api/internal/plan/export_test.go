package plan

import "time"

// SetNow stands the service at a moment of a test's choosing, for the tests
// outside the package that decide which month has passed.
func (s *Service) SetNow(now func() time.Time) { s.now = now }
