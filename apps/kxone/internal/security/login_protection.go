package security

import "context"

// LoginAttempt atomically checks, records a failure, or clears successful login
// state. It returns remaining cooldown seconds. No usernames reach storage.
type LoginProtection interface {
	LoginAttempt(context.Context, string, string) (int64, error)
}
