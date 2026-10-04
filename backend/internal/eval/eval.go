// Package eval implements a golden-fixture harness for multi-language AST
// extraction. It loads per-language conflict fixtures (base/ours/theirs source
// triplets), runs the real parser + semantic diff, and projects the result to a
// stable, line-independent shape that can be compared against golden JSON.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	parser "CommitIssues/internal/parser"
	semantic "CommitIssues/internal/semantic"
)

// CollisionProjection is the stable projection of a DiffItem used for golden
// comparison. Line numbers, file paths and full code content are intentionally
// excluded so fixtures stay robust to incidental formatting changes while still
// pinning the exact symbol identity of every collision.
type CollisionProjection struct {
	Type string `json:"type"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// Fixture is one per-language conflict fixture: three complete source versions
// of the same file.
type Fixture struct {
	Language string `json:"language"`
	File     string `json:"file"`
	Base     string `json:"base"`
	Ours     string `json:"ours"`
	Theirs   string `json:"theirs"`
}

// Golden is the expected output of running a Fixture through the parser and
// semantic diff.
type Golden struct {
	Language     string                `json:"language"`
	File         string                `json:"file"`
	Collisions   []CollisionProjection `json:"collisions"`
	OurChanges   []CollisionProjection `json:"our_changes"`
	TheirChanges []CollisionProjection `json:"their_changes"`
}

// LoadFixtures reads the fixture manifest (a JSON array of Fixture).
func LoadFixtures(path string) ([]Fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read fixtures %s: %w", path, err)
	}
	var fixtures []Fixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		return nil, fmt.Errorf("parse fixtures %s: %w", path, err)
	}
	return fixtures, nil
}

// LoadGolden reads the golden JSON (a JSON array of Golden) and indexes it by
// fixture file name for deterministic lookups.
func LoadGolden(path string) (map[string]Golden, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read golden %s: %w", path, err)
	}
	var goldens []Golden
	if err := json.Unmarshal(data, &goldens); err != nil {
		return nil, fmt.Errorf("parse golden %s: %w", path, err)
	}
	index := make(map[string]Golden, len(goldens))
	for _, g := range goldens {
		index[g.File] = g
	}
	return index, nil
}

// WriteGolden serializes goldens to path in a stable, deterministic order.
func WriteGolden(path string, goldens []Golden) error {
	sorted := append([]Golden(nil), goldens...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].File < sorted[j].File })
	data, err := json.MarshalIndent(sorted, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// Run parses the three fixture versions and computes the semantic diff,
// projecting it into the comparable Golden shape.
func Run(f Fixture) (Golden, error) {
	base, err := parser.ParseAndExtract(f.File, []byte(f.Base))
	if err != nil {
		return Golden{}, fmt.Errorf("%s: parse base: %w", f.File, err)
	}
	ours, err := parser.ParseAndExtract(f.File, []byte(f.Ours))
	if err != nil {
		return Golden{}, fmt.Errorf("%s: parse ours: %w", f.File, err)
	}
	theirs, err := parser.ParseAndExtract(f.File, []byte(f.Theirs))
	if err != nil {
		return Golden{}, fmt.Errorf("%s: parse theirs: %w", f.File, err)
	}

	diff := semantic.GenerateSmartDiff(context.Background(), base, ours, theirs)
	return Golden{
		Language:     base.Language,
		File:         f.File,
		Collisions:   Project(diff.Collisions),
		OurChanges:   Project(diff.OurChanges),
		TheirChanges: Project(diff.TheirChanges),
	}, nil
}

// Project maps diff items to their stable projection.
func Project(items []semantic.DiffItem) []CollisionProjection {
	out := make([]CollisionProjection, 0, len(items))
	for _, item := range items {
		out = append(out, CollisionProjection{Type: item.Type, Kind: item.Kind, Name: item.Name})
	}
	return out
}
