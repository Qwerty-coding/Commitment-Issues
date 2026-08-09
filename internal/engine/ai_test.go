package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFallbackResolveUsesDeterministicSnippet(t *testing.T) {
	payload := ConflictPayload{Operations: []ASTOperation{{
		Action:     "UPDATE",
		LocalCode:  "let a = 1;",
		RemoteCode: "let a = 2;",
	}}}

	fullLocalCode := "fn main() { let a = 1; }"

	resolved, err := fallbackResolve(payload, fullLocalCode)
	if err != nil {
		t.Fatalf("fallbackResolve returned error: %v", err)
	}

	if resolved == fullLocalCode {
		t.Fatalf("expected fallback to replace the local snippet, got %q", resolved)
	}

	if resolved != "fn main() { let a = 2; }" {
		t.Fatalf("unexpected resolved content: %q", resolved)
	}
}

func TestOllamaProviderResolvesThroughLocalEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/api/chat" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		var request ollamaChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.Model != defaultQwenModel {
			t.Fatalf("unexpected model: %s", request.Model)
		}
		if len(request.Messages) != 2 {
			t.Fatalf("unexpected message count: %d", len(request.Messages))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"fn main() { let a = 2; }"}}`))
	}))
	defer server.Close()

	t.Setenv("OLLAMA_HOST", server.URL)

	provider, err := NewOllamaProvider(defaultQwenModel)
	if err != nil {
		t.Fatalf("NewOllamaProvider returned error: %v", err)
	}

	resolved, err := provider.Resolve(ConflictPayload{Operations: []ASTOperation{{Action: "UPDATE", LocalCode: "let a = 1;", RemoteCode: "let a = 2;"}}}, "fn main() { let a = 1; }")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if strings.TrimSpace(resolved) != "fn main() { let a = 2; }" {
		t.Fatalf("unexpected resolved content: %q", resolved)
	}
}

func TestProviderFactorySelectsLocalModels(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "http://localhost:11434")

	t.Setenv("AI_PROVIDER", "qwen")
	provider, err := activeProviderFactory()
	if err != nil {
		t.Fatalf("qwen provider creation failed: %v", err)
	}
	qwenProvider, ok := provider.(*ollamaProvider)
	if !ok {
		t.Fatalf("expected qwen ollama provider, got %T", provider)
	}
	if qwenProvider.model != defaultQwenModel {
		t.Fatalf("unexpected qwen model: %s", qwenProvider.model)
	}

	t.Setenv("AI_PROVIDER", "llama")
	provider, err = activeProviderFactory()
	if err != nil {
		t.Fatalf("llama provider creation failed: %v", err)
	}
	llamaProvider, ok := provider.(*ollamaProvider)
	if !ok {
		t.Fatalf("expected llama ollama provider, got %T", provider)
	}
	if llamaProvider.model != defaultLlamaModel {
		t.Fatalf("unexpected llama model: %s", llamaProvider.model)
	}
}
