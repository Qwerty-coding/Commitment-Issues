package toon

import (
	"strings"
	"testing"

	semantic "CommitIssues/internal/semantic"
)

func TestMarshal_DiffItem_EscapesAndEmptyFields(t *testing.T) {
	item := semantic.DiffItem{
		Type:         "COLLISION",
		Kind:         "Function",
		Name:         "calc",
		Line:         3,
		BaseContent:  "return 1",
		OurContent:   "line1\nline2, with \"quotes\" and: colon {braces}",
		TheirContent: "",
		File:         "a.go",
		Identity:     "",
	}

	got, err := Marshal(item)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	want := strings.Join([]string{
		"type: COLLISION",
		"kind: Function",
		"name: calc",
		"line: 3",
		"base_content: return 1",
		`our_content: "line1\nline2, with \"quotes\" and: colon {braces}"`,
		"file: a.go",
		"",
	}, "\n")

	if got != want {
		t.Errorf("TOON mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestMarshal_SmartDiffResult_TabularHeaderAndEmptySlices(t *testing.T) {
	diff := semantic.SmartDiffResult{
		Collisions: []semantic.DiffItem{
			{
				Type:         "COLLISION",
				Kind:         "Function",
				Name:         "calculate",
				Line:         3,
				BaseContent:  "return x + 1",
				OurContent:   "return x + 2",
				TheirContent: "return x * 2",
				File:         "conflict.js",
				Identity:     "conflict.js||Function|calculate|(x)",
			},
		},
		OurChanges:   []semantic.DiffItem{},
		TheirChanges: nil,
	}

	got, err := Marshal(diff)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	want := strings.Join([]string{
		"collisions[1]{type,kind,name,line,base_content,our_content,their_content,file,identity}:",
		`  COLLISION,Function,calculate,3,return x + 1,return x + 2,return x * 2,conflict.js,conflict.js||Function|calculate|(x)`,
		"our_changes[0]:",
		"their_changes[0]:",
		"",
	}, "\n")

	if got != want {
		t.Errorf("TOON mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestMarshal_UnicodeStaysPlain(t *testing.T) {
	item := semantic.DiffItem{Type: "COLLISION", Kind: "Function", Name: "café ☕", Line: 1}
	got, err := Marshal(item)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(got, "name: café ☕\n") {
		t.Errorf("unicode should stay plain, got:\n%s", got)
	}
}

func TestMarshal_PrimitiveSlices(t *testing.T) {
	type payload struct {
		Calls []string `json:"calls"`
		Lines []int    `json:"lines"`
		Flags []bool   `json:"flags"`
		None  []string `json:"none"`
	}
	got, err := Marshal(payload{Calls: []string{"a", "b,c"}, Lines: []int{1, 2}, Flags: []bool{true, false}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := strings.Join([]string{
		"calls[2]: a,\"b,c\"",
		"lines[2]: 1,2",
		"flags[2]: true,false",
		"none[0]:",
		"",
	}, "\n")
	if got != want {
		t.Errorf("TOON mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestMarshal_NilAndPointerFields(t *testing.T) {
	type inner struct {
		V string `json:"v"`
	}
	type payload struct {
		Ptr   *inner  `json:"ptr"`
		Str   *string `json:"str"`
		Valid *inner  `json:"valid"`
	}
	s := "x"
	got, err := Marshal(payload{Str: &s, Valid: &inner{V: "ok"}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := strings.Join([]string{
		"ptr:",
		"str: x",
		"valid:",
		"  v: ok",
		"",
	}, "\n")
	if got != want {
		t.Errorf("TOON mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestMarshal_Deterministic pins that repeated encoding is byte-identical.
func TestMarshal_Deterministic(t *testing.T) {
	diff := semantic.SmartDiffResult{
		Collisions: []semantic.DiffItem{
			{Type: "COLLISION", Kind: "Function", Name: "a", Line: 1},
			{Type: "COLLISION", Kind: "Function", Name: "b", Line: 2},
		},
		OurChanges:   []semantic.DiffItem{{Type: "ADDED", Kind: "Variable", Name: "x", Line: 9}},
		TheirChanges: []semantic.DiffItem{},
	}
	first, err := Marshal(diff)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for i := 0; i < 5; i++ {
		again, err := Marshal(diff)
		if err != nil {
			t.Fatalf("Marshal run %d: %v", i, err)
		}
		if again != first {
			t.Fatalf("encoding is not deterministic (run %d):\n%s\n---\n%s", i, first, again)
		}
	}
}

func TestMarshal_TopLevelScalarAndNil(t *testing.T) {
	if got, _ := Marshal(42); got != "42\n" {
		t.Errorf("scalar = %q", got)
	}
	// TOON arrays always carry a key, so a bare top-level slice is a caller
	// error rather than silently invalid output.
	if _, err := Marshal([]string{"a", "b"}); err == nil {
		t.Error("top-level slice should return an error")
	}
	if got, _ := Marshal(nil); got != "" {
		t.Errorf("nil = %q", got)
	}
}
