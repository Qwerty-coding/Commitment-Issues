package promptcontext

import "sync"

// promptStore holds the most recently generated PromptContextIR per
// conflicted file. Mirrors internal/graph's graphStore pattern: the
// pipeline registers results as it computes them, REST handlers just read.
var (
	storeMu sync.Mutex
	store   = make(map[string]PromptContextIR)
	order   []string
)

func RegisterPromptContext(fileName string, ctx PromptContextIR) {
	storeMu.Lock()
	defer storeMu.Unlock()
	if _, exists := store[fileName]; !exists {
		order = append(order, fileName)
	}
	store[fileName] = ctx
}

func GetPromptContext(fileName string) (PromptContextIR, bool) {
	storeMu.Lock()
	defer storeMu.Unlock()
	ctx, ok := store[fileName]
	return ctx, ok
}

func AllPromptContexts() []PromptContextIR {
	storeMu.Lock()
	defer storeMu.Unlock()
	out := make([]PromptContextIR, 0, len(order))
	for _, f := range order {
		out = append(out, store[f])
	}
	return out
}