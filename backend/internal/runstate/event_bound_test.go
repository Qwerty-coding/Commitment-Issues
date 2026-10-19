package runstate

import (
	"testing"
	"time"

	"CommitIssues/internal/resolutions"
)

// TestResolutionEvents_Bounded verifies the audit log evicts oldest events
// once the cap is exceeded, so a long-lived server cannot grow unbounded,
// while sequence numbers keep increasing.
func TestResolutionEvents_Bounded(t *testing.T) {
	run := NewRun()
	run.SetResolutionEventLimit(5)

	for i := 0; i < 12; i++ {
		run.RecordResolutionEvent(resolutions.ResolutionEvent{
			ResolutionID: "res-1",
			Type:         resolutions.EventProposed,
			Timestamp:    time.Now().UTC(),
		})
	}

	events := run.AllResolutionEvents()
	if len(events) != 5 {
		t.Fatalf("expected 5 retained events, got %d", len(events))
	}
	// The most recent event must carry the highest sequence.
	if events[len(events)-1].Sequence != 12 {
		t.Errorf("expected last sequence 12, got %d", events[len(events)-1].Sequence)
	}
	// The retained window is the most recent 5 (sequences 8..12).
	if events[0].Sequence != 8 {
		t.Errorf("expected first retained sequence 8, got %d", events[0].Sequence)
	}
}
