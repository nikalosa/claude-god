package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type Inputs struct {
	CommitSHA  string
	MCPConfig  string
	MemTag     string
	Model      string
	Effort     string
	CLIVersion string
	RunPrompt  string
}

func Fingerprint(in Inputs) string {
	in.MCPConfig = normalizeJSON(in.MCPConfig)
	b, err := json.Marshal(in)
	if err != nil {

		b = []byte(in.CommitSHA + "\x00" + in.MCPConfig + "\x00" + in.MemTag + "\x00" +
			in.Model + "\x00" + in.Effort + "\x00" + in.CLIVersion + "\x00" + in.RunPrompt)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func normalizeJSON(s string) string {
	if s == "" {
		return ""
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return s
	}
	return string(b)
}
