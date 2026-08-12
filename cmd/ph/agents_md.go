package main

import (
    "fmt"
    "os"
    "strings"

    "github.com/vspcoderz/provider-hub/internal/config"
)

// cmdAgentsMd generates a markdown file (AGENTS.md) summarizing all configured providers.
func cmdAgentsMd(args []string) {
    cfg, err := config.Load()
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
        os.Exit(1)
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

    outPath := "AGENTS.md"
    if err := os.WriteFile(outPath, []byte(sb.String()), 0o644); err != nil {
        fmt.Fprintf(os.Stderr, "Failed to write %s: %v\n", outPath, err)
        os.Exit(1)
    }
    fmt.Printf("Generated %s (%d providers)\n", outPath, len(cfg.Providers))
}
