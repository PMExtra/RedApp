package spool

// FileStatus describes one stored or in-flight file of either engine for the
// capacity and disk metrics. Path is the local file the status refers to.
type FileStatus struct {
	// Scope is the metrics scope of the owning application.
	Scope string
	Path  string
	Bytes int64
	// State is a generation state such as downloading or complete.
	State        string
	Current      bool
	Retired      bool
	ActiveWriter bool
	Readers      int
}
