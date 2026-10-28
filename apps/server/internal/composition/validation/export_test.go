package validation

// SetMaxAffected lowers the affected-set limit for a test and returns the restore function.
func SetMaxAffected(n int) func() {
	old := maxAffected
	maxAffected = n
	return func() { maxAffected = old }
}
