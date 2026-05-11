package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/madprocs/config"
	"github.com/speakeasy-api/madprocs/process"
)

type listProcessesInput struct{}

type listProcessesOutput struct {
	Processes []string `json:"processes" jsonschema:"configured process names"`
}

type readProcessLogsInput struct {
	Name      string `json:"name" jsonschema:"process name to read logs from"`
	Type      string `json:"type" jsonschema:"which side of the log buffer to read: head or tail"`
	LineCount int    `json:"line_count" jsonschema:"number of log lines to return"`
}

type mcpLogLine struct {
	Timestamp string `json:"timestamp" jsonschema:"local timestamp formatted as HH:MM:SS"`
	Stream    string `json:"stream" jsonschema:"log stream, such as stdout, stderr, or tui"`
	Content   string `json:"content" jsonschema:"log line content"`
}

type readProcessLogsOutput struct {
	Name       string       `json:"name" jsonschema:"process name"`
	Type       string       `json:"type" jsonschema:"head or tail"`
	LineCount  int          `json:"line_count" jsonschema:"number of returned lines"`
	TotalLines int          `json:"totalLines" jsonschema:"number of lines currently available in memory"`
	Lines      []mcpLogLine `json:"lines" jsonschema:"selected log lines"`
}

type restartProcessInput struct {
	Name string `json:"name" jsonschema:"process name to restart"`
}

type restartProcessOutput struct {
	Name   string `json:"name" jsonschema:"process name"`
	Status string `json:"status" jsonschema:"restart status"`
}

func newMCPHandler(manager *process.Manager, version string) http.Handler {
	server := newMCPServer(manager, version)
	return mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return server
	}, &mcpsdk.StreamableHTTPOptions{
		SessionTimeout: 30 * time.Minute,
	})
}

func newMCPServer(manager *process.Manager, version string) *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "madprocs",
		Version: version,
	}, &mcpsdk.ServerOptions{
		Instructions: mcpInstructions(manager),
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "ListProcesses",
		Title:       "List Processes",
		Description: "Return the configured madprocs process names. Takes no arguments.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input listProcessesInput) (*mcpsdk.CallToolResult, listProcessesOutput, error) {
		return nil, listProcessesOutput{Processes: manager.Names()}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "ReadProcessLogs",
		Title:       "Read Process Logs",
		Description: "Read the head or tail of a process log buffer. Arguments: name, type=head|tail, line_count.",
		InputSchema: readProcessLogsInputSchema(),
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input readProcessLogsInput) (*mcpsdk.CallToolResult, readProcessLogsOutput, error) {
		if input.LineCount <= 0 {
			return nil, readProcessLogsOutput{}, fmt.Errorf("line_count must be greater than 0")
		}
		if input.Type != "head" && input.Type != "tail" {
			return nil, readProcessLogsOutput{}, fmt.Errorf("type must be one of: head, tail")
		}

		proc := manager.Get(input.Name)
		if proc == nil {
			return nil, readProcessLogsOutput{}, fmt.Errorf("process not found: %s", input.Name)
		}

		lines := proc.Buffer.Lines()
		start, end := selectLogRange(len(lines), input.Type, input.LineCount)
		selected := lines[start:end]
		outLines := make([]mcpLogLine, len(selected))
		for i, line := range selected {
			outLines[i] = mcpLogLine{
				Timestamp: line.Timestamp.Format("15:04:05"),
				Stream:    line.Stream,
				Content:   line.Content,
			}
		}

		return nil, readProcessLogsOutput{
			Name:       input.Name,
			Type:       input.Type,
			LineCount:  len(outLines),
			TotalLines: len(lines),
			Lines:      outLines,
		}, nil
	})

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "RestartProcess",
		Title:       "Restart Process",
		Description: "Restart a configured madprocs process by name.",
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, input restartProcessInput) (*mcpsdk.CallToolResult, restartProcessOutput, error) {
		proc := manager.Get(input.Name)
		if proc == nil {
			return nil, restartProcessOutput{}, fmt.Errorf("process not found: %s", input.Name)
		}
		if err := proc.Restart(); err != nil {
			return nil, restartProcessOutput{}, err
		}
		return nil, restartProcessOutput{Name: input.Name, Status: "restarted"}, nil
	})

	return server
}

func readProcessLogsInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"name", "type", "line_count"},
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "process name to read logs from",
			},
			"type": map[string]any{
				"type":        "string",
				"enum":        []string{"head", "tail"},
				"description": "which side of the log buffer to read",
			},
			"line_count": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"description": "number of log lines to return",
			},
		},
	}
}

func selectLogRange(total int, typ string, lineCount int) (int, int) {
	if lineCount > total {
		lineCount = total
	}
	if typ == "head" {
		return 0, lineCount
	}
	return total - lineCount, total
}

func mcpInstructions(manager *process.Manager) string {
	var b strings.Builder
	b.WriteString("madprocs controls and reads logs from the processes in this running dashboard instance. ")
	b.WriteString("Configured processes are: ")
	b.WriteString(mcpProcessInventory(manager))
	b.WriteString(". Use ListProcesses before choosing a process when the user has not named one. ")
	b.WriteString("Use ReadProcessLogs with a process name, type head or tail, and a positive line_count. ")
	b.WriteString("Use RestartProcess only when the user wants the named process restarted.")
	return b.String()
}

func mcpProcessInventory(manager *process.Manager) string {
	inventory := make(map[string]map[string]any)
	for _, proc := range manager.List() {
		inventory[proc.Name] = mcpRelevantConfig(proc.Config)
	}
	data, err := json.Marshal(inventory)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func mcpRelevantConfig(cfg config.ProcConfig) map[string]any {
	out := make(map[string]any)
	if cfg.Description != "" {
		out["description"] = cfg.Description
	}
	if len(cfg.Cmd) > 0 {
		out["cmd"] = []string(cfg.Cmd)
	} else if cfg.Shell != "" {
		out["cmd"] = cfg.Shell
	}
	if cfg.Cwd != "" {
		out["cwd"] = cfg.Cwd
	}
	return out
}
