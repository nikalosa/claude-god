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

	PeakContextTokens int `json:"peak_context_tokens"`

	BaseContextTokens int `json:"base_context_tokens"`

	ToolCalls []ToolCall `json:"tool_calls"`

	MCPServers []MCPServer `json:"mcp_servers,omitempty"`

	FileMutations []FileMutation `json:"file_mutations,omitempty"`

	CLIVersion  string `json:"cli_version,omitempty"`
	Concurrency int    `json:"concurrency,omitempty"`
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

type MCPServer struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type FileMutation struct {
	Path string `json:"path"`
	Op   string `json:"op"`
}

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

func (r *RunRecord) ContextWindowTokens() int {
	return r.PeakContextTokens
}

func (r *RunRecord) BaseContextWindowTokens() int {
	return r.BaseContextTokens
}

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
				SessionID  string      `json:"session_id"`
				Model      string      `json:"model"`
				Cwd        string      `json:"cwd"`
				MCPServers []MCPServer `json:"mcp_servers"`
			}
			if err := json.Unmarshal(line, &sys); err != nil {
				return nil, fmt.Errorf("parse system/init: %w", err)
			}
			rec.SessionID = sys.SessionID
			rec.Model = sys.Model
			rec.Cwd = sys.Cwd
			rec.MCPServers = sys.MCPServers

		case "assistant":

			var am struct {
				ParentToolUseID *string `json:"parent_tool_use_id"`
				Message         struct {
					Content []struct {
						Type string `json:"type"`
						Name string `json:"name"`
					} `json:"content"`
					Usage struct {
						InputTokens              int `json:"input_tokens"`
						CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
						CacheReadInputTokens     int `json:"cache_read_input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if err := json.Unmarshal(line, &am); err != nil {
				return nil, fmt.Errorf("parse assistant: %w", err)
			}
			if am.ParentToolUseID != nil {
				continue
			}

			resident := am.Message.Usage.InputTokens + am.Message.Usage.CacheCreationInputTokens + am.Message.Usage.CacheReadInputTokens
			if rec.BaseContextTokens == 0 {
				rec.BaseContextTokens = resident
			}
			if resident > rec.PeakContextTokens {
				rec.PeakContextTokens = resident
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
