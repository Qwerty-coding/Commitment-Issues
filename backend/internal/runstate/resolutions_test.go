package runstate

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"CommitIssues/internal/resolutions"
)

func resolution(file string, created time.Time) resolutions.Resolution {
	return resolutions.Resolution{
		ID:             resolutions.NewID(),
		SuggestionID:   "/repo|" + file + "|k1",
		RunID:          "run-1",
		Repository:     "/repo",
		File:           file,
		CollisionKey:   "k1",
		Revision:       1,
		RegionID:       "region-1",
		Status:         resolutions.StatusProposed,
		ApprovalStatus: resolutions.ApprovalNone,
		CreatedAt:      created,
	}
}

func TestSaveAndGetResolution(t *testing.T) {
	run := NewRun()
	stored := run.SaveResolution(resolution("a.js", time.Now().UTC()))

	if stored.ID == "" {
		t.Fatal("stored resolution must carry an ID")
	}
	got, ok := run.GetResolution(stored.ID)
	if !ok {
		t.Fatalf("resolution %q not stored", stored.ID)
	}
	if got.File != "a.js" || got.Repository != "/repo" {
		t.Errorf("resolution = %+v", got)
	}
	if _, ok := run.GetResolution("missing"); ok {
		t.Error("unknown resolution ID must not match")
	}
}

func TestSaveResolution_GeneratesMissingID(t *testing.T) {
	run := NewRun()
	res := resolution("a.js", time.Now().UTC())
	res.ID = ""
	stored := run.SaveResolution(res)

	if stored.ID == "" {
		t.Fatal("SaveResolution must generate an ID when none is supplied")
	}
	if _, ok := run.GetResolution(stored.ID); !ok {
		t.Error("resolution with generated ID was not stored")
	}
}

func TestSaveResolution_UpsertsWithoutDuplicates(t *testing.T) {
	run := NewRun()
	first := run.SaveResolution(resolution("a.js", time.Now().UTC()))
	updated := first
	updated.Status = resolutions.StatusPreviewed
	updated.PreviewDiff = "diff"
	run.SaveResolution(updated)

	all := run.AllResolutions()
	if len(all) != 1 {
		t.Fatalf("expected 1 resolution after upsert, got %d", len(all))
	}
	if all[0].Status != resolutions.StatusPreviewed || all[0].PreviewDiff != "diff" {
		t.Errorf("upsert did not replace the entry: %+v", all[0])
	}
}

func TestFindResolutionBySuggestion_ReturnsMostRecent(t *testing.T) {
	run := NewRun()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	older := resolution("a.js", base)
	newer := resolution("a.js", base.Add(time.Minute))
	run.SaveResolution(older)
	run.SaveResolution(newer)

	// An unrelated suggestion must not match.
	if _, ok := run.FindResolutionBySuggestion("/repo|a.js|other"); ok {
		t.Error("lookup must be scoped to the suggestion ID")
	}
	got, ok := run.FindResolutionBySuggestion("/repo|a.js|k1")
	if !ok {
		t.Fatal("expected a resolution for the suggestion")
	}
	if got.ID != newer.ID {
		t.Errorf("most recent resolution = %q, want %q", got.ID, newer.ID)
	}
}

func TestResolutionsFor_RepositoryAndFileScoped(t *testing.T) {
	run := NewRun()
	run.SaveResolution(resolution("a.js", time.Now().UTC()))
	other := resolution("a.js", time.Now().UTC().Add(time.Second))
	other.Repository = "/other"
	other.SuggestionID = "/other|a.js|k1"
	run.SaveResolution(other)
	run.SaveResolution(resolution("b.js", time.Now().UTC().Add(2*time.Second)))

	got := run.ResolutionsFor("/repo", "a.js")
	if len(got) != 1 || got[0].File != "a.js" {
		t.Fatalf("expected exactly the /repo a.js resolution, got %+v", got)
	}
	if got := run.ResolutionsFor("/other", "a.js"); len(got) != 1 || got[0].Repository != "/other" {
		t.Fatalf("expected exactly the /other a.js resolution, got %+v", got)
	}
	if got := run.ResolutionsFor("/repo", "missing.js"); len(got) != 0 {
		t.Errorf("unknown file must return no resolutions, got %+v", got)
	}
}

func TestAllResolutions_DeterministicOrdering(t *testing.T) {
	build := func() []resolutions.Resolution {
		run := NewRun()
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		// Deliberately inserted out of order and across repositories.
		run.SaveResolution(resolution("b.js", base.Add(2*time.Minute)))
		other := resolution("a.js", base.Add(time.Minute))
		other.Repository = "/other"
		other.SuggestionID = "/other|a.js|k1"
		run.SaveResolution(other)
		run.SaveResolution(resolution("a.js", base))
		return run.AllResolutions()
	}

	first := build()
	second := build()
	if len(first) != 3 {
		t.Fatalf("expected 3 resolutions, got %d", len(first))
	}
	// IDs are opaque and random, so determinism is asserted on
	// the ordering keys (repository, file, creation time), not
	// on the identifiers themselves.
	for i := range first {
		if first[i].Repository != second[i].Repository ||
			first[i].File != second[i].File ||
			!first[i].CreatedAt.Equal(second[i].CreatedAt) {
			t.Fatalf("ordering is not deterministic at index %d:\n got %s/%s@%s\nwant %s/%s@%s",
				i,
				first[i].Repository, first[i].File, first[i].CreatedAt,
				second[i].Repository, second[i].File, second[i].CreatedAt)
		}
	}
	// Repository, then file, then creation time.
	if first[0].Repository != "/other" {
		t.Errorf("first resolution should belong to /other, got %+v", first[0])
	}
	if first[1].File != "a.js" || first[2].File != "b.js" {
		t.Errorf("expected a.js before b.js within /repo, got %q, %q",
			first[1].File, first[2].File)
	}
}

func TestAllResolutions_IDTieBreak(t *testing.T) {
	run := NewRun()
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sameTime := func(id string) resolutions.Resolution {
		res := resolution("a.js", ts)
		res.ID = id
		return res
	}
	run.SaveResolution(sameTime("zzz"))
	run.SaveResolution(sameTime("aaa"))

	got := run.AllResolutions()
	if len(got) != 2 {
		t.Fatalf("expected 2 resolutions, got %d", len(got))
	}
	if got[0].ID != "aaa" || got[1].ID != "zzz" {
		t.Errorf("equal timestamps must tie-break by ID, got %q, %q",
			got[0].ID, got[1].ID)
	}
}

func TestRecordResolutionEvent_MonotonicSequence(t *testing.T) {
	run := NewRun()
	res := run.SaveResolution(resolution("a.js", time.Now().UTC()))

	e1 := run.RecordResolutionEvent(resolutions.ResolutionEvent{
		ResolutionID: res.ID, RunID: run.ID, Repository: res.Repository,
		File: res.File, Actor: resolutions.ActorUser,
		Type: resolutions.EventPreviewed, NewStatus: resolutions.StatusPreviewed,
	})
	e2 := run.RecordResolutionEvent(resolutions.ResolutionEvent{
		ResolutionID: res.ID, RunID: run.ID, Repository: res.Repository,
		File: res.File, Actor: resolutions.ActorUser,
		Type: resolutions.EventApproved, PreviousStatus: resolutions.StatusPreviewed,
		NewStatus: resolutions.StatusApproved,
	})

	if e1.Sequence != 1 || e2.Sequence != 2 {
		t.Fatalf("sequences = %d, %d; want 1, 2", e1.Sequence, e2.Sequence)
	}
	if e1.Timestamp.IsZero() || e2.Timestamp.IsZero() {
		t.Error("events must always carry a timestamp")
	}
	if e2.Timestamp.Before(e1.Timestamp) {
		t.Error("event timestamps must not go backwards")
	}
}

func TestResolutionEvents_FilteredByResolution(t *testing.T) {
	run := NewRun()
	base := time.Now().UTC()
	resA := run.SaveResolution(resolution("a.js", base))
	resB := run.SaveResolution(resolution("b.js", base.Add(time.Second)))

	run.RecordResolutionEvent(resolutions.ResolutionEvent{
		ResolutionID: resA.ID, Repository: resA.Repository, File: resA.File,
		Type: resolutions.EventPreviewed, NewStatus: resolutions.StatusPreviewed,
	})
	run.RecordResolutionEvent(resolutions.ResolutionEvent{
		ResolutionID: resB.ID, Repository: resB.Repository, File: resB.File,
		Type: resolutions.EventPreviewed, NewStatus: resolutions.StatusPreviewed,
	})
	run.RecordResolutionEvent(resolutions.ResolutionEvent{
		ResolutionID: resA.ID, Repository: resA.Repository, File: resA.File,
		Type: resolutions.EventApproved, NewStatus: resolutions.StatusApproved,
	})

	eventsA := run.ResolutionEvents(resA.ID)
	if len(eventsA) != 2 {
		t.Fatalf("resolution A should have 2 events, got %d", len(eventsA))
	}
	if eventsA[0].Type != resolutions.EventPreviewed ||
		eventsA[1].Type != resolutions.EventApproved {
		t.Errorf("events must stay in sequence order: %+v", eventsA)
	}
	if got := run.ResolutionEvents("missing"); len(got) != 0 {
		t.Errorf("unknown resolution must have no events, got %+v", got)
	}
	if all := run.AllResolutionEvents(); len(all) != 3 {
		t.Errorf("run should have 3 events, got %d", len(all))
	}
}

func TestAllResolutionEvents_ReturnsCopy(t *testing.T) {
	run := NewRun()
	res := run.SaveResolution(resolution("a.js", time.Now().UTC()))
	run.RecordResolutionEvent(resolutions.ResolutionEvent{
		ResolutionID: res.ID, Type: resolutions.EventPreviewed,
		NewStatus: resolutions.StatusPreviewed,
	})

	all := run.AllResolutionEvents()
	all[0].Type = "mutated"

	if stored := run.AllResolutionEvents(); stored[0].Type == "mutated" {
		t.Error("AllResolutionEvents must return a copy")
	}
}

func TestTryBeginResolutionApply_ExclusiveAndReleasable(t *testing.T) {
	run := NewRun()
	res := run.SaveResolution(resolution("a.js", time.Now().UTC()))

	if !run.TryBeginResolutionApply(res.ID) {
		t.Fatal("first acquisition should succeed")
	}
	if run.TryBeginResolutionApply(res.ID) {
		t.Fatal("second acquisition must be refused while in flight")
	}
	run.EndResolutionApply(res.ID)
	if !run.TryBeginResolutionApply(res.ID) {
		t.Fatal("resolution should be reusable after release")
	}
	run.EndResolutionApply(res.ID)
}

func TestResolutionInFlight_IsRunScoped(t *testing.T) {
	runA := NewRun()
	runB := NewRun()
	res := resolution("a.js", time.Now().UTC())
	res.ID = "same-id"
	runA.SaveResolution(res)

	if !runA.TryBeginResolutionApply(res.ID) {
		t.Fatal("runA acquire failed")
	}
	if !runB.TryBeginResolutionApply(res.ID) {
		t.Fatal("runB must not be blocked by runA's in-flight apply")
	}
}

func TestRun_ResolutionsAreIsolatedBetweenRuns(t *testing.T) {
	runA := NewRun()
	runB := NewRun()
	res := runA.SaveResolution(resolution("a.js", time.Now().UTC()))

	if _, ok := runB.GetResolution(res.ID); ok {
		t.Error("resolution leaked between runs")
	}
	if got := runB.AllResolutions(); len(got) != 0 {
		t.Errorf("runB should have no resolutions, got %d", len(got))
	}
	if got := runB.AllResolutionEvents(); len(got) != 0 {
		t.Errorf("runB should have no events, got %d", len(got))
	}
}

func TestRun_ConcurrentResolutionAccessIsRaceSafe(t *testing.T) {
	run := NewRun()
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			file := fmt.Sprintf("file%02d.js", n%10)
			res := run.SaveResolution(resolution(file, time.Now().UTC()))
			run.GetResolution(res.ID)
			run.FindResolutionBySuggestion(res.SuggestionID)
			run.AllResolutions()
			run.RecordResolutionEvent(resolutions.ResolutionEvent{
				ResolutionID: res.ID, Type: resolutions.EventPreviewed,
				NewStatus: resolutions.StatusPreviewed,
			})
			run.TryBeginResolutionApply(res.ID)
			run.EndResolutionApply(res.ID)
		}(i)
	}
	wg.Wait()

	// Ten distinct files, each with its own resolutions.
	for _, file := range []string{
		"file00.js", "file01.js", "file02.js", "file03.js", "file04.js",
		"file05.js", "file06.js", "file07.js", "file08.js", "file09.js",
	} {
		if got := run.ResolutionsFor("/repo", file); len(got) == 0 {
			t.Errorf("expected resolutions for %s", file)
		}
	}
	if got := len(run.AllResolutionEvents()); got < 10 {
		t.Errorf("expected at least 10 events, got %d", got)
	}
}
