package verification

import (
	"testing"

	git "CommitIssues/internal/git"
)

func TestEvaluate_CleanValidReplacement(t *testing.T) {
	region := git.ConflictRegion{
		Ours:   "package main\n\nfunc Hello() string {\n\treturn \"ours\"\n}\n",
		Theirs: "package main\n\nfunc Hello() string {\n\treturn \"theirs\"\n}\n",
	}
	proposed := "package main\n\nfunc Hello() string {\n\treturn \"merged\"\n}\n"

	res := Evaluate("main.go", proposed, region)
	if res.Score != 100 {
		t.Fatalf("expected score 100, got %d with findings %+v", res.Score, res.Findings)
	}
	if res.Status != StatusVerified {
		t.Fatalf("expected status %s, got %s", StatusVerified, res.Status)
	}
	if len(res.Findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(res.Findings))
	}
}

func TestEvaluate_ConflictMarkersSurvive(t *testing.T) {
	region := git.ConflictRegion{
		Ours:   "func A() {}",
		Theirs: "func B() {}",
	}
	proposed := "<<<<<<< HEAD\nfunc A() {}\n=======\nfunc B() {}\n>>>>>>> branch\n"

	res := Evaluate("main.go", proposed, region)
	if res.Score > 85 {
		t.Fatalf("expected score <= 85 due to markers penalty, got %d", res.Score)
	}
	foundMarkerCheck := false
	for _, f := range res.Findings {
		if f.Check == "markers" && f.Weight == 15 {
			foundMarkerCheck = true
			break
		}
	}
	if !foundMarkerCheck {
		t.Fatalf("expected marker finding, got %+v", res.Findings)
	}
}

func TestEvaluate_EmptyReplacement(t *testing.T) {
	region := git.ConflictRegion{
		Ours:   "func Compute() int { return 42; }",
		Theirs: "func Compute() int { return 99; }",
	}
	proposed := ""

	res := Evaluate("main.go", proposed, region)
	if res.Score >= 100 {
		t.Fatalf("expected score penalty for empty replacement, got %d", res.Score)
	}
	foundEmptyCheck := false
	for _, f := range res.Findings {
		if f.Check == "sanity" && f.Weight == 20 {
			foundEmptyCheck = true
			break
		}
	}
	if !foundEmptyCheck {
		t.Fatalf("expected sanity finding for empty replacement, got %+v", res.Findings)
	}
}

func TestEvaluate_SyntaxError(t *testing.T) {
	region := git.ConflictRegion{
		Ours:   "package main\n\nfunc Foo() {}",
		Theirs: "package main\n\nfunc Foo() {}",
	}
	proposed := "package main\n\nfunc Foo( { syntax error here !!!"

	res := Evaluate("main.go", proposed, region)
	if res.Score > 65 {
		t.Fatalf("expected syntax error penalty (-35 pts), got score %d", res.Score)
	}
	if res.Status != StatusNeedsReview {
		t.Fatalf("expected status %s for low score, got %s", StatusNeedsReview, res.Status)
	}
}

