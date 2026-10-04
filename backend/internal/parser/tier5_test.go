package parser

import "testing"

// TestTier5_NativeGrammarsParse documents and verifies the Tier-5 language
// decision: Kotlin, Swift, Shell (bash), and the common data/config formats
// below ship natively with the pinned github.com/smacker/go-tree-sitter
// (v0.0.0-20240827094217) and are therefore supported without vendoring.
func TestTier5_NativeGrammarsParse(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		source   string
		language string
	}{
		{"kotlin", "App.kt", "fun greet(name: String): String {\n    return \"hi\"\n}\n", "kotlin"},
		{"swift", "App.swift", "func greet(name: String) -> String {\n    return \"hi\"\n}\n", "swift"},
		{"bash", "deploy.sh", "greet() {\n  echo hi\n}\n", "bash"},
		{"toml", "config.toml", "[server]\nport = 8080\n", "toml"},
		{"yaml", "config.yaml", "server:\n  port: 8080\n", "yaml"},
		{"css", "styles.css", "body { color: red; }\n", "css"},
		{"dockerfile", "Dockerfile", "FROM alpine:3.19\nRUN echo hi\n", "dockerfile"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !IsSupportedLanguage(tc.file) {
				t.Fatalf("%s (%s) should be supported by the pinned grammar set", tc.name, tc.file)
			}
			if got := GetLanguageTier(tc.file); got != Tier5 {
				t.Errorf("%s tier = %d, want Tier5", tc.file, got)
			}

			ctx, err := ParseAndExtract(tc.file, []byte(tc.source))
			if err != nil {
				t.Fatalf("ParseAndExtract(%s) failed: %v", tc.file, err)
			}
			if ctx.Language != tc.language {
				t.Errorf("%s language = %q, want %q", tc.file, ctx.Language, tc.language)
			}
		})
	}
}

// TestTier5_DeferredLanguagesUnsupported pins the deferred half of the Tier-5
// decision. Dart, SQL and JSON grammars are NOT bundled with the pinned
// tree-sitter module, and Markdown ships a bespoke two-parse-tree API that does
// not fit the generic *sitter.Language pipeline. Rather than silently
// vendoring/upgrading we defer them: they must fall back to the text-diff path,
// not claim AST support.
func TestTier5_DeferredLanguagesUnsupported(t *testing.T) {
	for _, file := range []string{"main.dart", "query.sql", "data.json", "README.md"} {
		if IsSupportedLanguage(file) {
			t.Errorf("%s must remain unsupported until a grammar is bundled", file)
		}
		if name := GetLanguageName(file); name != "" {
			t.Errorf("%s language = %q, want empty (deferred)", file, name)
		}
	}
}
