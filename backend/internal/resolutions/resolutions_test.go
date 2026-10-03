package resolutions

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNewID_IsOpaqueAndURLSafe(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id := NewID()
		if id == "" {
			t.Fatal("NewID returned an empty identifier")
		}
		if len(id) < 16 {
			t.Fatalf("ID %q is too short to be opaque", id)
		}
		for _, c := range id {
			if c == '|' || c == '/' || c == '\\' || c == ':' ||
				c == '?' || c == '#' || c == '%' || c == ' ' {
				t.Fatalf("ID %q contains URL-unsafe character %q", id, c)
			}
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate ID %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestNewID_DoesNotLeakWindowsPaths(t *testing.T) {
	// Even the time-based fallback path must stay URL-safe.
	for i := 0; i < 100; i++ {
		id := NewID()
		if strings.ContainsAny(id, `C:\`) || strings.Contains(id, "..") {
			t.Fatalf("ID %q leaks a filesystem path", id)
		}
	}
}

func TestLifecycleStatuses_AreStable(t *testing.T) {
	want := []string{
		"proposed", "previewed", "approved", "applied",
		"reverted", "stale", "failed", "validation_failed",
	}
	got := []string{
		StatusProposed, StatusPreviewed, StatusApproved, StatusApplied,
		StatusReverted, StatusStale, StatusFailed, StatusValidationFailed,
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("lifecycle status %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestValidationStatuses_AreStable(t *testing.T) {
	want := []string{"not_run", "running", "passed", "failed", "timed_out", "cancelled"}
	got := []string{
		ValidationNotRun, ValidationRunning, ValidationPassed,
		ValidationFailed, ValidationTimedOut, ValidationCancelled,
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("validation status %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestApprovalStatuses_AreStable(t *testing.T) {
	want := []string{"none", "approved", "expired"}
	got := []string{ApprovalNone, ApprovalApproved, ApprovalExpired}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("approval status %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestEventTypes_AreStable(t *testing.T) {
	want := []string{
		"previewed", "approved", "apply_started", "applied",
		"validation_started", "validation_passed", "validation_failed",
		"revert_started", "reverted", "stale", "failed",
	}
	got := []string{
		EventPreviewed, EventApproved, EventApplyStarted, EventApplied,
		EventValidationStarted, EventValidationPassed, EventValidationFailed,
		EventRevertStarted, EventReverted, EventStale, EventFailed,
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event type %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestErrorCodes_AreStable(t *testing.T) {
	want := []string{
		"RESOLUTION_NOT_FOUND", "NOT_APPROVED", "ALREADY_APPLIED",
		"STALE_FILE", "INVALID_PATCH", "PATH_ESCAPE",
		"REPOSITORY_MISMATCH", "APPROVAL_EXPIRED", "VALIDATION_FAILURE",
		"ROLLBACK_FAILURE", "APPLY_IN_PROGRESS",
	}
	got := []string{
		string(CodeResolutionNotFound), string(CodeNotApproved),
		string(CodeAlreadyApplied), string(CodeStaleFile),
		string(CodeInvalidPatch), string(CodePathEscape),
		string(CodeRepositoryMismatch), string(CodeApprovalExpired),
		string(CodeValidationFailure), string(CodeRollbackFailure),
		string(CodeApplyInProgress),
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("error code %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolution_JSONRoundTrip(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	approved := created.Add(time.Minute)
	res := Resolution{
		ID:               "0123456789abcdef0123456789abcdef",
		SuggestionID:     "/repo|a.js|main.js||Function|login|()",
		RunID:            "run-1",
		Repository:       "/repo",
		File:             "a.js",
		CollisionKey:     "main.js||Function|login|()",
		Revision:         2,
		RegionID:         "region-1",
		StartLine:        10,
		EndLine:          20,
		StartOffset:      240,
		EndOffset:        520,
		ContentHash:      "hash-current",
		ContextHash:      "hash-context",
		Base:             "base",
		Ours:             "ours",
		Theirs:           "theirs",
		Replacement:      "merged",
		PreviewDiff:      "diff",
		Status:           StatusApproved,
		ApprovalStatus:   ApprovalApproved,
		ValidationStatus: ValidationPassed,
		ApprovedAt:       approved,
		AppliedAt:        approved.Add(time.Minute),
		CreatedAt:        created,
		ErrorCode:        "",
		ErrorMessage:     "",
	}

	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Resolution
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != res {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", decoded, res)
	}
}

func TestResolutionEvent_JSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	event := ResolutionEvent{
		Sequence:       7,
		ResolutionID:   "res-1",
		RunID:          "run-1",
		Repository:     "/repo",
		File:           "a.js",
		Timestamp:      ts,
		Actor:          ActorUser,
		Type:           EventApplied,
		PreviousStatus: StatusApproved,
		NewStatus:      StatusApplied,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ResolutionEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != event {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", decoded, event)
	}
}
