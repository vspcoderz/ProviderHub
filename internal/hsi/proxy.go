package hsi

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vspcoderz/provider-hub/internal/schema"
)

//go:embed api2codex.py
var api2codexScript []byte

// defaultProxyUA is the User-Agent the routers expect from Anthropic clients.
// Used when a provider does not declare its own headers.
const defaultProxyUA = "claude-cli/2.1.0 (external, cli)"

// proxyCacheDir returns the shared cache dir for the translation proxy.
func proxyCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "provider-hub", "hsi", "proxy"), nil
}

// ensureProxyRuntime creates (once) the venv with the proxy dependencies and
// writes the embedded api2codex.py script. Returns the python binary path.
func ensureProxyRuntime() (string, error) {
	dir, err := proxyCacheDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}

	script := filepath.Join(dir, "api2codex.py")
	if err := os.WriteFile(script, api2codexScript, 0o600); err != nil {
		return "", err
	}

	venv := filepath.Join(dir, "venv")
	python := filepath.Join(venv, "bin", "python")
	if _, err := os.Stat(python); err == nil {
		return python, nil
	}

	uv, err := exec.LookPath("uv")
	if err != nil {
		return "", fmt.Errorf("uv not found on PATH (needed to create the codex proxy venv): %w", err)
	}

	fmt.Fprintf(os.Stderr, "[ph-codex] setting up translation proxy (one-time)...\n")
	if out, err := exec.Command(uv, "venv", venv).CombinedOutput(); err != nil {
		return "", fmt.Errorf("uv venv: %v\n%s", err, out)
	}
	if out, err := exec.Command(uv, "pip", "install", "--python", python, "fastapi", "uvicorn", "httpx").CombinedOutput(); err != nil {
		return "", fmt.Errorf("uv pip install: %v\n%s", err, out)
	}
	return python, nil
}

// upstreamBase ensures the proxy upstream base ends in "/v1" so that the
// appended "/chat/completions" hits the OpenAI-compatible surface.
func upstreamBase(baseURL string) string {
	b := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(b, "/v1") {
		return b
	}
	return b + "/v1"
}

// freePort finds an available TCP port on 127.0.0.1.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// startProxy launches the Responses→ChatCompletions translation proxy bound
// to 127.0.0.1 on a free port, pointing at the provider's OpenAI surface.
// It returns the local port and a stop func that terminates the proxy.
func startProxy(provider *schema.Provider, key string) (int, func(), error) {
	python, err := ensureProxyRuntime()
	if err != nil {
		return 0, nil, err
	}
	dir, err := proxyCacheDir()
	if err != nil {
		return 0, nil, err
	}

	port, err := freePort()
	if err != nil {
		return 0, nil, err
	}

	ua := defaultProxyUA
	if h := provider.Headers["User-Agent"]; h != "" {
		ua = h
	}

	logFile := filepath.Join(dir, "proxy.log")
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, nil, err
	}

	cmd := exec.Command(python, filepath.Join(dir, "api2codex.py"))
	cmd.Env = append(os.Environ(),
		"UPSTREAM_BASE_URL="+upstreamBase(provider.BaseURL),
		"UPSTREAM_API_KEY="+key,
		"UPSTREAM_USER_AGENT="+ua,
		"HOST=127.0.0.1",
		fmt.Sprintf("PORT=%d", port),
	)
	cmd.Stdout = f
	cmd.Stderr = f

	if err := cmd.Start(); err != nil {
		f.Close()
		return 0, nil, err
	}
	stop := func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		f.Close()
	}

	// Wait for /health (venv import can take a few seconds on first run).
	client := &http.Client{Timeout: 500 * time.Millisecond}
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	for i := 0; i < 60; i++ {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return port, stop, nil
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	stop()
	return 0, nil, fmt.Errorf("proxy failed to start; see %s", logFile)
}

// writeModelCatalog writes a Codex model_catalog.json for the provider into
// the shared proxy dir and returns its path.
func writeModelCatalog(p *schema.Provider) (string, error) {
	dir, err := proxyCacheDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	data, err := modelCatalog(p)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "model_catalog.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// ExitError carries a child process exit code so the caller can propagate it.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// catalogEntry mirrors the subset of Codex's model_catalog.json schema that
// provider-hub fills in; omitted fields get Codex's defaults.
type catalogEntry struct {
	Slug                        string `json:"slug"`
	DisplayName                 string `json:"display_name"`
	Description                 string `json:"description"`
	DefaultReasoningLevel       string `json:"default_reasoning_level"`
	SupportedReasoningLevels    []struct {
		Effort      string `json:"effort"`
		Description string `json:"description"`
	} `json:"supported_reasoning_levels"`
	ContextWindow             int    `json:"context_window"`
	MaxContextWindow          int    `json:"max_context_window"`
	EffectiveContextWindowPct int    `json:"effective_context_window_percent"`
	SupportsReasoningSummary  bool   `json:"supports_reasoning_summaries"`
	DefaultReasoningSummary   string `json:"default_reasoning_summary"`
	SupportVerbosity          bool   `json:"support_verbosity"`
	DefaultVerbosity          string `json:"default_verbosity"`
	SupportedInAPI            bool   `json:"supported_in_api"`
	Visibility                string `json:"visibility"`
	Priority                  int    `json:"priority"`
	ShellType                 string `json:"shell_type"`
	ApplyPatchToolType        string `json:"apply_patch_tool_type"`
	WebSearchToolType         string `json:"web_search_tool_type"`
	TruncationPolicy          struct {
		Mode  string `json:"mode"`
		Limit int    `json:"limit"`
	} `json:"truncation_policy"`
	SupportsParallelToolCalls bool     `json:"supports_parallel_tool_calls"`
	SupportsImageDetailOrig   bool     `json:"supports_image_detail_original"`
	InputModalities           []string `json:"input_modalities"`
	ExperimentalSupportedTools []any   `json:"experimental_supported_tools"`
	SupportsSearchTool        bool     `json:"supports_search_tool"`
	BaseInstructions          string   `json:"base_instructions"`
}

// modelCatalog builds a Codex model_catalog.json for a provider's models so
// Codex stops warning about missing metadata and applies sensible limits.
func modelCatalog(p *schema.Provider) ([]byte, error) {
	entries := make([]catalogEntry, 0, len(p.Models))
	for i, m := range p.Models {
		ctx := m.ContextWindow
		if ctx == 0 {
			ctx = 200000
		}
		entry := catalogEntry{
			Slug:                        m.ID,
			DisplayName:                 m.Name,
			Description:                 fmt.Sprintf("%s via %s", m.Name, p.Name),
			DefaultReasoningLevel:       "medium",
			ContextWindow:               ctx,
			MaxContextWindow:            ctx,
			EffectiveContextWindowPct:   95,
			SupportsReasoningSummary:    true,
			DefaultReasoningSummary:     "auto",
			SupportVerbosity:            true,
			DefaultVerbosity:            "low",
			SupportedInAPI:              true,
			Visibility:                  "list",
			Priority:                    100 + i,
			ShellType:                   "shell_command",
			ApplyPatchToolType:          "freeform",
			WebSearchToolType:           "text",
			SupportsParallelToolCalls:   true,
			SupportsImageDetailOrig:     false,
			InputModalities:             []string{"text"},
			ExperimentalSupportedTools:  []any{},
			SupportsSearchTool:          false,
			BaseInstructions:            fmt.Sprintf("You are %s running in the Codex CLI, a terminal-based coding assistant, via the %s provider. Be precise, safe, and helpful.", m.Name, p.Name),
		}
		entry.TruncationPolicy.Mode = "bytes"
		entry.TruncationPolicy.Limit = 10000
		levels := []struct {
			Effort      string `json:"effort"`
			Description string `json:"description"`
		}{
			{Effort: "low", Description: "Fast, low reasoning"},
			{Effort: "medium", Description: "Balanced reasoning"},
			{Effort: "high", Description: "Deep reasoning"},
		}
		entry.SupportedReasoningLevels = levels
		entries = append(entries, entry)
	}
	return json.Marshal(struct {
		Models []catalogEntry `json:"models"`
	}{Models: entries})
}
