package parser

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type RunRecord struct {
	SessionID  string                `json:"session_id"`
	Model      string                `json:"model"`
	Cwd        string                `json:"cwd"`
	FinalText  string                `json:"final_text"`
	StopReason string                `json:"stop_reason"`
	IsError    bool                  `json:"is_error"`
	NumTurns   int                   `json:"num_turns"`
	Timing     Timing                `json:"timing"`
	Usage      Usage                 `json:"usage"`
	TotalCost  float64               `json:"total_cost_usd"`
	ModelUsage map[string]ModelUsage `json:"model_usage,omitempty"`

	// ToolCalls is the ordered list of top-level tool invocations. Sub-agent
	// internal calls are excluded (see the assistant case in Parse); empty in
	// flat no-tool runs.
	ToolCalls []ToolCall `json:"tool_calls"`

	// FileMutations is reserved for the harness to fill from the post-run git
	// diff; the parser always leaves it nil.
	FileMutations []FileMutation `json:"file_mutations,omitempty"`
}

type Timing struct {
	DurationMs    int `json:"duration_ms"`
	DurationAPIMs int `json:"duration_api_ms"`
	TTFTMs        int `json:"ttft_ms"`
}

type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type ModelUsage struct {
	InputTokens              int     `json:"input_tokens"`
	OutputTokens             int     `json:"output_tokens"`
	CacheCreationInputTokens int     `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int     `json:"cache_read_input_tokens"`
	CostUSD                  float64 `json:"cost_usd"`
}

type ToolCall struct {
	Name string `json:"name"`
}

type FileMutation struct {
	Path string `json:"path"`
	Op   string `json:"op"`
}

// TotalInputTokens returns session-wide input tokens (uncached + cache creation +
// cache read) summed across all models. The stream's result.usage reports only the
// final turn, so for multi-turn or sub-agent runs it badly undercounts (observed
// 10-13x); modelUsage is the true aggregate and the basis for total_cost_usd. Falls
// back to the final-turn Usage when modelUsage is absent.
func (r *RunRecord) TotalInputTokens() int {
	if len(r.ModelUsage) == 0 {
		return r.Usage.InputTokens + r.Usage.CacheCreationInputTokens + r.Usage.CacheReadInputTokens
	}
	total := 0
	for _, m := range r.ModelUsage {
		total += m.InputTokens + m.CacheCreationInputTokens + m.CacheReadInputTokens
	}
	return total
}

// TotalOutputTokens returns session-wide output tokens summed across all models,
// with the same final-turn fallback as TotalInputTokens.
func (r *RunRecord) TotalOutputTokens() int {
	if len(r.ModelUsage) == 0 {
		return r.Usage.OutputTokens
	}
	total := 0
	for _, m := range r.ModelUsage {
		total += m.OutputTokens
	}
	return total
}

func Parse(r io.Reader) (*RunRecord, error) {
	rec := &RunRecord{ToolCalls: []ToolCall{}}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	gotResult := false
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}

		var head struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
		}
		if err := json.Unmarshal(line, &head); err != nil {
			return nil, fmt.Errorf("parse event header: %w", err)
		}

		switch head.Type {
		case "system":
			if head.Subtype != "init" {
				continue
			}
			var sys struct {
				SessionID string `json:"session_id"`
				Model     string `json:"model"`
				Cwd       string `json:"cwd"`
			}
			if err := json.Unmarshal(line, &sys); err != nil {
				return nil, fmt.Errorf("parse system/init: %w", err)
			}
			rec.SessionID = sys.SessionID
			rec.Model = sys.Model
			rec.Cwd = sys.Cwd

		case "assistant":
			// Count top-level tool calls only: parent_tool_use_id is null for the
			// main session and non-null for a spawned Agent sub-session's internal
			// calls, which we skip. The harness disables subagents
			// (--disallowedTools "Agent"), so in practice it is always null.
			var am struct {
				ParentToolUseID *string `json:"parent_tool_use_id"`
				Message         struct {
					Content []struct {
						Type string `json:"type"`
						Name string `json:"name"`
					} `json:"content"`
				} `json:"message"`
			}
			if err := json.Unmarshal(line, &am); err != nil {
				return nil, fmt.Errorf("parse assistant: %w", err)
			}
			if am.ParentToolUseID != nil {
				continue
			}
			for _, b := range am.Message.Content {
				if b.Type == "tool_use" {
					rec.ToolCalls = append(rec.ToolCalls, ToolCall{Name: b.Name})
				}
			}

		case "result":
			var raw rawResult
			if err := json.Unmarshal(line, &raw); err != nil {
				return nil, fmt.Errorf("parse result: %w", err)
			}
			rec.FinalText = raw.Result
			rec.StopReason = raw.StopReason
			rec.IsError = raw.IsError
			rec.NumTurns = raw.NumTurns
			rec.Timing = Timing{
				DurationMs:    raw.DurationMs,
				DurationAPIMs: raw.DurationAPIMs,
				TTFTMs:        raw.TTFTMs,
			}
			rec.Usage = Usage{
				InputTokens:              raw.Usage.InputTokens,
				OutputTokens:             raw.Usage.OutputTokens,
				CacheCreationInputTokens: raw.Usage.CacheCreationInputTokens,
				CacheReadInputTokens:     raw.Usage.CacheReadInputTokens,
			}
			rec.TotalCost = raw.TotalCostUSD
			if len(raw.ModelUsage) > 0 {
				rec.ModelUsage = make(map[string]ModelUsage, len(raw.ModelUsage))
				for k, v := range raw.ModelUsage {
					rec.ModelUsage[k] = ModelUsage{
						InputTokens:              v.InputTokens,
						OutputTokens:             v.OutputTokens,
						CacheCreationInputTokens: v.CacheCreationInputTokens,
						CacheReadInputTokens:     v.CacheReadInputTokens,
						CostUSD:                  v.CostUSD,
					}
				}
			}
			gotResult = true

		default:
			// Tolerate unknown transient types (e.g., rate_limit_event).
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan stream: %w", err)
	}
	if !gotResult {
		return nil, errors.New("stream ended without a result event")
	}
	return rec, nil
}

type rawResult struct {
	Result        string                   `json:"result"`
	StopReason    string                   `json:"stop_reason"`
	IsError       bool                     `json:"is_error"`
	NumTurns      int                      `json:"num_turns"`
	DurationMs    int                      `json:"duration_ms"`
	DurationAPIMs int                      `json:"duration_api_ms"`
	TTFTMs        int                      `json:"ttft_ms"`
	TotalCostUSD  float64                  `json:"total_cost_usd"`
	Usage         rawUsage                 `json:"usage"`
	ModelUsage    map[string]rawModelUsage `json:"modelUsage"`
}

type rawUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type rawModelUsage struct {
	InputTokens              int     `json:"inputTokens"`
	OutputTokens             int     `json:"outputTokens"`
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens"`
	CostUSD                  float64 `json:"costUSD"`
}
