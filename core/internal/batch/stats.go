package batch

// ProcessStats contains statistics from a batch processing operation.
type ProcessStats struct {
	// Processed is the number of entries successfully written to the sink.
	Processed int

	// Skipped is the number of entries skipped (ShouldProcess returned false).
	Skipped int

	// TotalBytes is the sum of OriginalSize for all processed entries.
	TotalBytes uint64
}

// add accumulates stats from another ProcessStats into this one.
func (s *ProcessStats) add(other ProcessStats) {
	s.Processed += other.Processed
	s.Skipped += other.Skipped
	s.TotalBytes += other.TotalBytes
}
