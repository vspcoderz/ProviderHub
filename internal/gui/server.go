package gui

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/vspcoderz/provider-hub/internal/config"
	"github.com/vspcoderz/provider-hub/internal/gen/codex"
	"github.com/vspcoderz/provider-hub/internal/gen/hermes"
	"github.com/vspcoderz/provider-hub/internal/gen/opencode"
	"github.com/vspcoderz/provider-hub/internal/gen/pi"
	"github.com/vspcoderz/provider-hub/internal/schema"
	"github.com/vspcoderz/provider-hub/internal/skill"
	"github.com/vspcoderz/provider-hub/internal/system"
)

//go:embed templates/*.html
var templateFS embed.FS

var addr = ":7357"

func init() {
	if port := os.Getenv("PROVIDER_HUB_PORT"); port != "" {
		addr = ":" + port
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	}
	if cmd != nil {
		cmd.Start()
	}
}

// Serve starts the web GUI and opens the browser.
func Serve() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/provider/", handleProvider)
	mux.HandleFunc("/provider/add", handleProviderAdd)
	mux.HandleFunc("/provider/edit", handleProviderEdit)
	mux.HandleFunc("/provider/delete", handleProviderDelete)
	mux.HandleFunc("/sync", handleSync)
	mux.HandleFunc("/sync/preview", handleSyncPreview)
	mux.HandleFunc("/doctor", handleDoctor)
	mux.HandleFunc("/import", handleImport)
	mux.HandleFunc("/agents-md", handleAgentsMD)
	mux.HandleFunc("/skills", handleSkills)
	mux.HandleFunc("/skills/save", handleSkillSave)
	mux.HandleFunc("/skills/delete", handleSkillDelete)
	mux.HandleFunc("/system", handleSystem)
	mux.HandleFunc("/system/save", handleSystemSave)
	mux.HandleFunc("/system/delete", handleSystemDelete)

	url := "http://localhost" + addr
	fmt.Printf("Starting provider-hub GUI at %s\n", url)

	go openBrowser(url)

	return http.ListenAndServe(addr, mux)
}

func render(w http.ResponseWriter, name string, data interface{}) {
	tmpl, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.ExecuteTemplate(w, name, data)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	type providerView struct {
		schema.Provider
		Compat    map[string]bool
		CompatMsg map[string]string
	}

	var providers []providerView
	for _, p := range cfg.Providers {
		pv := providerView{
			Provider: p,
		}
		pv.Compat = map[string]bool{
			"codex":    canUseCodex(p),
			"pi":       true,
			"opencode": true,
			"hermes":   true,
		}
		pv.CompatMsg = map[string]string{}
		if !pv.Compat["codex"] {
			pv.CompatMsg["codex"] = "requires openai protocol"
		}
		providers = append(providers, pv)
	}

	render(w, "index.html", map[string]interface{}{
		"Providers": providers,
		"Count":     len(providers),
	})
}

func canUseCodex(p schema.Provider) bool {
	for _, proto := range p.Protocols {
		if proto == "openai" {
			return true
		}
	}
	return len(p.Protocols) == 0
}

func handleProvider(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/provider/")
	if id == "" {
		http.NotFound(w, r)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	p := config.FindProvider(cfg, id)
	if p == nil {
		http.Error(w, "Provider not found", 404)
		return
	}

	render(w, "edit.html", map[string]interface{}{
		"Provider":   p,
		"Protocols":  strings.Join(p.Protocols, ", "),
		"ModelsText": modelsToText(p.Models),
		"ToolCodex":  toolEnabled(p, "codex"),
		"ToolPi":     toolEnabled(p, "pi"),
		"ToolOpen":   toolEnabled(p, "opencode"),
		"ToolHermes": toolEnabled(p, "hermes"),
	})
}

func handleProviderEdit(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	id := r.FormValue("id")
	if id == "" {
		http.Error(w, "missing id", 400)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	existing := config.FindProvider(cfg, id)
	if existing == nil {
		http.Error(w, "Provider not found", 404)
		return
	}

	var protocols []string
	if proto := r.FormValue("protocols"); proto != "" {
		for _, p := range strings.Split(proto, ",") {
			if p = strings.TrimSpace(p); p != "" {
				protocols = append(protocols, p)
			}
		}
	}

	provider := schema.Provider{
		ID:        id,
		Name:      r.FormValue("name"),
		BaseURL:   r.FormValue("base_url"),
		APIKeyEnv: r.FormValue("api_key_env"),
		Protocols: protocols,
		Headers:   existing.Headers,
		Tools:     map[string]schema.Tool{},
		Models:    parseModelsText(r.FormValue("models")),
	}
	for _, tool := range []string{"codex", "pi", "opencode", "hermes"} {
		provider.Tools[tool] = schema.Tool{Enabled: r.FormValue("tool_"+tool) == "on"}
	}

	config.UpsertProvider(cfg, provider)
	if err := config.Save(cfg); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleProviderAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		render(w, "add.html", nil)
		return
	}

	// POST: create provider
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var protocols []string
	if proto := r.FormValue("protocols"); proto != "" {
		protocols = strings.Split(proto, ",")
		for i := range protocols {
			protocols[i] = strings.TrimSpace(protocols[i])
		}
	}

	provider := schema.Provider{
		ID:        r.FormValue("id"),
		Name:      r.FormValue("name"),
		BaseURL:   r.FormValue("base_url"),
		APIKeyEnv: r.FormValue("api_key_env"),
		Protocols: protocols,
		Tools:     map[string]schema.Tool{},
	}

	// Parse tools
	for _, tool := range []string{"codex", "pi", "opencode", "hermes"} {
		enabled := r.FormValue("tool_"+tool) == "on"
		provider.Tools[tool] = schema.Tool{Enabled: enabled}
	}

	// Parse models (from textarea, one per line: id|name|context|max_output)
	if modelsStr := r.FormValue("models"); modelsStr != "" {
		for _, line := range strings.Split(modelsStr, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.Split(line, "|")
			m := schema.Model{
				ID:   strings.TrimSpace(parts[0]),
				Name: strings.TrimSpace(parts[0]),
			}
			if len(parts) > 1 {
				m.Name = strings.TrimSpace(parts[1])
			}
			provider.Models = append(provider.Models, m)
		}
	}

	config.UpsertProvider(cfg, provider)
	if err := config.Save(cfg); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleProviderDelete(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id", 400)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	config.RemoveProvider(cfg, id)
	if err := config.Save(cfg); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

type syncResult struct {
	Tool    string
	Path    string
	Content string
	Error   string
}

func handleSync(w http.ResponseWriter, r *http.Request) {
	toolFilter := r.URL.Query().Get("tool")
	dryRun := r.URL.Query().Get("dry") == "true"

	cfg, err := config.Load()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	generators := []struct {
		name string
		fn   func(*schema.Config, bool) (string, string, error)
	}{
		{"codex", codex.Generate},
		{"pi", pi.Generate},
		{"opencode", opencode.Generate},
		{"hermes", hermes.Generate},
	}

	var results []syncResult
	for _, g := range generators {
		if toolFilter != "" && g.name != toolFilter {
			continue
		}

		path, content, err := g.fn(cfg, dryRun)
		r := syncResult{
			Tool:    g.name,
			Path:    path,
			Content: content,
		}
		if err != nil {
			r.Error = err.Error()
		}
		results = append(results, r)
	}

	render(w, "sync.html", map[string]interface{}{
		"Results": results,
		"DryRun":  dryRun,
	})
}

func handleSyncPreview(w http.ResponseWriter, r *http.Request) {
	tool := r.URL.Query().Get("tool")
	if tool == "" {
		http.Error(w, "missing tool", 400)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	generators := map[string]func(*schema.Config, bool) (string, string, error){
		"codex":    codex.Generate,
		"pi":       pi.Generate,
		"opencode": opencode.Generate,
		"hermes":   hermes.Generate,
	}

	fn, ok := generators[tool]
	if !ok {
		http.Error(w, "unknown tool", 400)
		return
	}

	path, content, err := fn(cfg, true)
	w.Header().Set("Content-Type", "text/plain")
	if err != nil {
		fmt.Fprintf(w, "Error: %v", err)
		return
	}
	fmt.Fprintf(w, "# Would write to: %s\n\n%s", path, content)
}

func handleDoctor(w http.ResponseWriter, r *http.Request) {
	type checkResult struct {
		Name      string
		Path      string
		OK        bool
		Error     string
		Providers []string
	}

	checks := []checkResult{
		{Name: "codex"},
		{Name: "pi"},
		{Name: "opencode"},
		{Name: "hermes"},
	}

	validators := []func() error{codex.Validate, pi.Validate, opencode.Validate, hermes.Validate}
	finders := [][]string{codex.FindExistingProviders(), pi.FindExistingProviders(), opencode.FindExistingProviders(), hermes.FindExistingProviders()}
	pathGetters := []func() (string, error){codex.ConfigPath, pi.ConfigPath, opencode.ConfigPath, hermes.ConfigPath}

	for i := range checks {
		path, _ := pathGetters[i]()
		checks[i].Path = path
		if err := validators[i](); err != nil {
			checks[i].Error = err.Error()
		} else {
			checks[i].OK = true
		}
		checks[i].Providers = finders[i]
	}

	// Check env vars
	cfg, err := config.Load()
	type envCheck struct {
		Var   string
		Value string
		Set   bool
	}
	var envChecks []envCheck
	if err == nil {
		for _, p := range cfg.Providers {
			if p.APIKeyEnv != "" {
				val := os.Getenv(p.APIKeyEnv)
				envChecks = append(envChecks, envCheck{
					Var:   p.APIKeyEnv,
					Value: val,
					Set:   val != "",
				})
			}
		}
	}

	render(w, "doctor.html", map[string]interface{}{
		"Checks":  checks,
		"EnvVars": envChecks,
	})
}

func handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		render(w, "import.html", nil)
		return
	}

	// POST: run import
	// For now, just redirect to doctor
	http.Redirect(w, r, "/doctor", http.StatusSeeOther)
}

// handleAgentsMD serves a markdown summary of all configured providers.
func handleAgentsMD(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Load()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var sb strings.Builder
	sb.WriteString("# Provider Agents\n\n")
	sb.WriteString("| ID | Name | Base URL | API Key Env | Tools |\n")
	sb.WriteString("|----|------|----------|-------------|-------|\n")
	for _, p := range cfg.Providers {
		var tools []string
		for name, t := range p.Tools {
			if t.Enabled {
				tools = append(tools, name)
			}
		}
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |\n",
			p.ID, p.Name, p.BaseURL, p.APIKeyEnv, strings.Join(tools, ", ")))
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fmt.Fprint(w, sb.String())
}

// ConfigDir returns the path for a tool's config.
func configDir(tool string) string {
	home, _ := os.UserHomeDir()
	switch tool {
	case "codex":
		return filepath.Join(home, ".codex")
	case "pi":
		return filepath.Join(home, ".pi", "agent")
	case "opencode":
		return filepath.Join(home, ".config", "opencode")
	case "hermes":
		return filepath.Join(home, ".hermes")
	}
	return ""
}

// --- model text helpers ---

func modelsToText(models []schema.Model) string {
	var sb strings.Builder
	for _, m := range models {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		sb.WriteString(fmt.Sprintf("%s | %s", m.ID, name))
		if m.ContextWindow > 0 || m.MaxOutput > 0 {
			sb.WriteString(fmt.Sprintf(" | %d | %d", m.ContextWindow, m.MaxOutput))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func parseModelsText(s string) []schema.Model {
	var models []schema.Model
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		m := schema.Model{ID: strings.TrimSpace(parts[0])}
		m.Name = m.ID
		if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
			m.Name = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			if n, err := strconv.Atoi(strings.TrimSpace(parts[2])); err == nil {
				m.ContextWindow = n
			}
		}
		if len(parts) > 3 {
			if n, err := strconv.Atoi(strings.TrimSpace(parts[3])); err == nil {
				m.MaxOutput = n
			}
		}
		models = append(models, m)
	}
	return models
}

func toolEnabled(p *schema.Provider, name string) bool {
	if p.Tools == nil {
		return true
	}
	t, ok := p.Tools[name]
	if !ok {
		return true
	}
	return t.Enabled
}

// --- skills ---

type kvItem struct {
	Key   string
	Value string
}

func handleSkills(w http.ResponseWriter, r *http.Request) {
	skills, err := skill.List()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	items := make([]kvItem, 0, len(skills))
	for k, v := range skills {
		items = append(items, kvItem{Key: k, Value: v})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	render(w, "skills.html", map[string]interface{}{"Items": items})
}

func handleSkillSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	body := r.FormValue("body")
	if name == "" || body == "" {
		http.Error(w, "name and body are required", 400)
		return
	}
	if err := skill.Set(name, body); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/skills", http.StatusSeeOther)
}

func handleSkillDelete(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "missing name", 400)
		return
	}
	if err := skill.Remove(name); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/skills", http.StatusSeeOther)
}

// --- system prompts ---

func handleSystem(w http.ResponseWriter, r *http.Request) {
	prompts, err := system.ListSystemPrompts()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	items := make([]kvItem, 0, len(prompts))
	for k, v := range prompts {
		items = append(items, kvItem{Key: k, Value: v})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	render(w, "system.html", map[string]interface{}{"Items": items})
}

func handleSystemSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	prompt := r.FormValue("prompt")
	if id == "" || prompt == "" {
		http.Error(w, "id and prompt are required", 400)
		return
	}
	if err := system.SetSystemPrompt(id, prompt); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/system", http.StatusSeeOther)
}

func handleSystemDelete(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id", 400)
		return
	}
	if err := system.RemoveSystemPrompt(id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/system", http.StatusSeeOther)
}
