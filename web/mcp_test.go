package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/madprocs/config"
	"github.com/speakeasy-api/madprocs/process"
)

func TestMCPTools(t *testing.T) {
	cfg := &config.Config{
		Scrollback: 100,
		Procs: map[string]config.ProcConfig{
			"api": {
				Description: "API server",
				Cmd:         config.StringOrSlice{"go", "run", "."},
				Cwd:         "/tmp/api",
			},
			"worker": {
				Description: "background worker",
				Shell:       "printf 'restart-ok\\n'; sleep 10",
				Autostart:   boolPtr(false),
			},
		},
	}

	manager, err := process.NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	manager.Get("api").Buffer.Write("api", "stdout", "line one")
	manager.Get("api").Buffer.Write("api", "stdout", "line two")
	manager.Get("api").Buffer.Write("api", "stderr", "line three")

	session, closeSession := connectTestMCP(t, manager)
	defer closeSession()

	instructions := session.InitializeResult().Instructions
	for _, want := range []string{"api", "worker", "go", "run", "API server", "/tmp/api"} {
		if !strings.Contains(instructions, want) {
			t.Fatalf("instructions missing %q: %s", want, instructions)
		}
	}
	if strings.Contains(instructions, "env") || strings.Contains(instructions, "autorestart") {
		t.Fatalf("instructions should include only cmd/cwd/description config: %s", instructions)
	}

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var toolNames []string
	for _, tool := range tools.Tools {
		toolNames = append(toolNames, tool.Name)
	}
	if !reflect.DeepEqual(toolNames, []string{"ListProcesses", "ReadProcessLogs", "RestartProcess"}) {
		t.Fatalf("tool names = %v", toolNames)
	}

	listResult, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "ListProcesses",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var listOut listProcessesOutput
	decodeStructured(t, listResult, &listOut)
	if !reflect.DeepEqual(listOut.Processes, []string{"api", "worker"}) {
		t.Fatalf("processes = %v", listOut.Processes)
	}

	tailResult, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "ReadProcessLogs",
		Arguments: map[string]any{
			"name":       "api",
			"type":       "tail",
			"line_count": 2,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var tailOut readProcessLogsOutput
	decodeStructured(t, tailResult, &tailOut)
	if got := logContents(tailOut.Lines); !reflect.DeepEqual(got, []string{"line two", "line three"}) {
		t.Fatalf("tail logs = %v", got)
	}
	if tailOut.TotalLines != 3 || tailOut.LineCount != 2 {
		t.Fatalf("tail counts = total %d returned %d", tailOut.TotalLines, tailOut.LineCount)
	}

	headResult, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "ReadProcessLogs",
		Arguments: map[string]any{
			"name":       "api",
			"type":       "head",
			"line_count": 1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var headOut readProcessLogsOutput
	decodeStructured(t, headResult, &headOut)
	if got := logContents(headOut.Lines); !reflect.DeepEqual(got, []string{"line one"}) {
		t.Fatalf("head logs = %v", got)
	}

	restartResult, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "RestartProcess",
		Arguments: map[string]any{"name": "worker"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var restartOut restartProcessOutput
	decodeStructured(t, restartResult, &restartOut)
	if restartOut.Name != "worker" || restartOut.Status != "restarted" {
		t.Fatalf("restart output = %+v", restartOut)
	}
	if manager.Get("worker").State() != process.StateRunning {
		t.Fatalf("worker state = %s", manager.Get("worker").State())
	}
}

func TestMCPServerInstructionsDescribeProcessesWithRelevantConfigOnly(t *testing.T) {
	envValue := "postgres://secret"
	cfg := &config.Config{
		Scrollback: 10,
		Procs: map[string]config.ProcConfig{
			"api": {
				Description: "public API server",
				Cmd:         config.StringOrSlice{"go", "run", "./cmd/api"},
				Cwd:         "/workspace/api",
				Env: map[string]*string{
					"DATABASE_URL": &envValue,
				},
				Autorestart: true,
				LogDir:      "/tmp/logs",
				Tui:         true,
			},
			"worker": {
				Description: "background worker",
				Shell:       "npm run worker",
				Cwd:         "/workspace/worker",
			},
		},
	}
	manager, err := process.NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	session, closeSession := connectTestMCP(t, manager)
	defer closeSession()

	instructions := session.InitializeResult().Instructions
	for _, want := range []string{
		"Configured processes are:",
		`"api":{"cmd":["go","run","./cmd/api"],"cwd":"/workspace/api","description":"public API server"}`,
		`"worker":{"cmd":"npm run worker","cwd":"/workspace/worker","description":"background worker"}`,
	} {
		if !strings.Contains(instructions, want) {
			t.Fatalf("instructions missing %q: %s", want, instructions)
		}
	}

	for _, notWant := range []string{
		"DATABASE_URL",
		envValue,
		"autorestart",
		"log_dir",
		"tui",
	} {
		if strings.Contains(instructions, notWant) {
			t.Fatalf("instructions included irrelevant config %q: %s", notWant, instructions)
		}
	}
}

func TestReadProcessLogsRejectsInvalidArgs(t *testing.T) {
	cfg := &config.Config{
		Scrollback: 10,
		Procs: map[string]config.ProcConfig{
			"api": {Cmd: config.StringOrSlice{"echo", "ok"}},
		},
	}
	manager, err := process.NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	session, closeSession := connectTestMCP(t, manager)
	defer closeSession()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "ReadProcessLogs",
		Arguments: map[string]any{
			"name":       "api",
			"type":       "middle",
			"line_count": 1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected invalid enum value to return a tool error")
	}
}

func connectTestMCP(t *testing.T, manager *process.Manager) (*mcpsdk.ClientSession, func()) {
	t.Helper()

	mux := http.NewServeMux()
	mux.Handle("/mcp", newMCPHandler(manager, "test"))
	server := httptest.NewServer(mux)

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "madprocs-test", Version: "test"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	session, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{
		Endpoint:             server.URL + "/mcp",
		DisableStandaloneSSE: true,
	}, nil)
	cancel()
	if err != nil {
		server.Close()
		t.Fatal(err)
	}

	return session, func() {
		session.Close()
		server.Close()
	}
}

func decodeStructured[T any](t *testing.T, result *mcpsdk.CallToolResult, out *T) {
	t.Helper()
	if result.IsError {
		t.Fatalf("tool returned error: %+v", result.Content)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatal(err)
	}
}

func logContents(lines []mcpLogLine) []string {
	contents := make([]string, len(lines))
	for i, line := range lines {
		contents[i] = line.Content
	}
	return contents
}

func boolPtr(v bool) *bool {
	return &v
}
