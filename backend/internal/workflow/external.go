package workflow

// ExternalResult overlays one independently validated result onto the matching
// durable GitHub comment. The raw comment remains the display/audit source; this
// projection only supplies the typed terminal meaning established outside the
// legacy supervisor:event parser.
type ExternalResult struct {
	CommentID      int64
	Event          *WorkerEvent
	TransitionSafe bool
	Warnings       []Warning
	HardError      *ProtocolError
}
