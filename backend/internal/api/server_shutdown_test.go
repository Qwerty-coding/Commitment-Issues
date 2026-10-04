package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"CommitIssues/internal/cache"
	"CommitIssues/internal/runstate"
)

func TestRepository_IncludesCacheStats(t *testing.T) {
	run := runstate.NewRun()
	run.SetCacheStats(cache.Stats{Hits: 7, Misses: 3, Stores: 10})

	handler := NewHandler(run, runstate.NewHistory(runstate.DefaultHistoryLimit))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/repository", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var payload struct {
		Success    bool `json:"success"`
		CacheStats struct {
			Hits   int64 `json:"hits"`
			Misses int64 `json:"misses"`
			Stores int64 `json:"stores"`
		} `json:"cacheStats"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.CacheStats.Hits != 7 || payload.CacheStats.Misses != 3 {
		t.Errorf("cacheStats = %+v, want hits=7 misses=3", payload.CacheStats)
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func TestStartGraphServer_GracefulShutdown(t *testing.T) {
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		StartGraphServer(ctx, runstate.NewRun(), runstate.NewHistory(10), addr)
		close(done)
	}()

	// Wait until the server is accepting connections.
	baseURL := fmt.Sprintf("http://%s", addr)
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(baseURL + "/api/repository")
		if err == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("server never became reachable: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(ShutdownTimeout + 5*time.Second):
		t.Fatal("StartGraphServer did not return within the shutdown window")
	}
}
