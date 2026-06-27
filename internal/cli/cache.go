package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/pflag"

	"github.com/nikalosa/claude-god/internal/cache"
	"github.com/nikalosa/claude-god/internal/dsl"
)

const (
	defaultModel  = "claude-opus-4-8"
	defaultEffort = "medium"
)

type cacheFlags struct {
	model      string
	effort     string
	cliVersion string
	noCache    bool
}

func addCacheFlags(f *pflag.FlagSet, c *cacheFlags) {
	f.StringVar(&c.model, "model", defaultModel, "model every run uses (controlled variable; keyed by the Run cache)")
	f.StringVar(&c.effort, "effort", defaultEffort, "reasoning effort every run uses (controlled variable; keyed by the Run cache)")
	f.StringVar(&c.cliVersion, "cli-version", "", "override the CLI-version `token` of the cache key to reuse a pool across bumps (default: detected from claude --version)")
	f.BoolVar(&c.noCache, "no-cache", false, "bypass the Run cache read (still writes) — forces fresh draws to re-measure noise")
}

func newStore(target string, mem memPolicy, cf cacheFlags, concurrency int) (*cache.Store, error) {
	detected, derr := detectCLIVersion()
	key := cf.cliVersion
	stamp := detected
	if key == "" {
		if derr != nil {
			return nil, derr
		}
		key = detected
	}
	if stamp == "" {
		stamp = key
	}
	memTag, err := memTag(mem)
	if err != nil {
		return nil, err
	}
	return cache.New(cache.Opts{
		Root:            filepath.Join(target, ".benchmark", "cache"),
		Target:          target,
		Model:           cf.model,
		Effort:          cf.effort,
		CLIVersionKey:   key,
		CLIVersionStamp: stamp,
		MemTag:          memTag,
		Concurrency:     concurrency,
	}), nil
}

func detectCLIVersion() (string, error) {
	out, err := exec.Command("claude", "--version").Output()
	if err != nil {
		return "", fmt.Errorf("detect claude --version (pass --cli-version to override): %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("empty `claude --version` output")
	}
	return fields[0], nil
}

func memTag(mem memPolicy) (string, error) {
	if mem.source != "" {
		h, err := hashDir(mem.source)
		if err != nil {
			return "", fmt.Errorf("hash live memory %s: %w", mem.source, err)
		}
		return "live:" + h, nil
	}
	if mem.noSnapshot {
		return "none", nil
	}
	return "snapshot", nil
}

func hashDir(dir string) (string, error) {
	var paths []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		rel, _ := filepath.Rel(dir, p)
		fmt.Fprintf(h, "%s\x00", rel)
		f, err := os.Open(p)
		if err != nil {
			return "", err
		}
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return "", err
		}
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func cachePlan(store *cache.Store, probes []dsl.Probe, samples int, noCache bool, envs ...Env) (cached, toRun int, err error) {
	for _, env := range envs {
		for _, p := range probes {
			served := 0
			if !noCache && !env.Volatile {
				key, err := store.Key(env.Ref, env.MCPConfig, taskPrompt(p))
				if err != nil {
					return 0, 0, err
				}
				pool, err := store.Read(key)
				if err != nil {
					return 0, 0, err
				}
				served = min(len(pool), samples)
			}
			cached += served
			toRun += samples - served
		}
	}
	return cached, toRun, nil
}
