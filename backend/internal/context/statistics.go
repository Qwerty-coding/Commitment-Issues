package promptcontext

import "sync"

// PromptStatisticsDTO matches the "Prompt Statistics" contract in
// API_SPEC.md: estimated tokens, selected nodes, excluded nodes, reduction %.
type PromptStatisticsDTO struct {
	File                string  `json:"file"`
	EstimatedTokens     int     `json:"estimatedTokens"`
	FullPayloadTokens   int     `json:"fullPayloadTokens"`
	SelectedNodes       int     `json:"selectedNodes"`
	ExcludedNodes       int     `json:"excludedNodes"`
	ReductionPercentage float64 `json:"reductionPercentage"`
}

var (
	statsMu    sync.Mutex
	stats      = make(map[string]PromptStatisticsDTO)
	statsOrder []string
)

func RegisterPromptStatistics(s PromptStatisticsDTO) {
	statsMu.Lock()
	defer statsMu.Unlock()
	if _, exists := stats[s.File]; !exists {
		statsOrder = append(statsOrder, s.File)
	}
	stats[s.File] = s
}

func GetPromptStatistics(file string) (PromptStatisticsDTO, bool) {
	statsMu.Lock()
	defer statsMu.Unlock()
	s, ok := stats[file]
	return s, ok
}

func AllPromptStatistics() []PromptStatisticsDTO {
	statsMu.Lock()
	defer statsMu.Unlock()
	out := make([]PromptStatisticsDTO, 0, len(statsOrder))
	for _, f := range statsOrder {
		out = append(out, stats[f])
	}
	return out
}

func BuildReductionPercentage(fullTokens, scopedTokens int) float64 {
	if fullTokens == 0 {
		return 0
	}
	return float64(fullTokens-scopedTokens) / float64(fullTokens) * 100
}