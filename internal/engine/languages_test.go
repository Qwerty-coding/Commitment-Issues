package engine

import "testing"

func TestGetLanguageForFileExtension(t *testing.T) {
	cases := []struct {
		name      string
		filePath  string
		wantFound bool
	}{
		{name: "rust", filePath: "main.rs", wantFound: true},
		{name: "python", filePath: "script.py", wantFound: true},
		{name: "javascript", filePath: "app.js", wantFound: true},
		{name: "go", filePath: "main.go", wantFound: true},
		{name: "unknown", filePath: "file.xyz", wantFound: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lang, err := GetLanguageForFile(tc.filePath)
			if tc.wantFound {
				if err != nil {
					t.Fatalf("expected parser for %s, got error: %v", tc.filePath, err)
				}
				if lang == nil {
					t.Fatalf("expected non-nil language for %s", tc.filePath)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error for unsupported extension %s", tc.filePath)
			}
		})
	}
}
