package workerloop

// validNextRole preserves the internal v1 command-store validation surface.
// The closed role vocabulary is shared by both result protocol versions.
func validNextRole(role string) bool {
	return validRole(role)
}
