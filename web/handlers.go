package web

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/speakeasy-api/madprocs/process"
)

// ProcessInfo represents process information for the API
type ProcessInfo struct {
	Name     string `json:"name"`
	State    string `json:"state"`
	ExitCode int    `json:"exitCode,omitempty"`
	Uptime   string `json:"uptime,omitempty"`
}

// LogMessage represents a log line for the API
type LogMessage struct {
	Timestamp string `json:"timestamp"`
	Content   string `json:"content"`
	Stream    string `json:"stream"`
	Process   string `json:"process"`
}

// handleProcesses returns a list of all processes
func (s *Server) handleProcesses(w http.ResponseWriter, r *http.Request) {
	procs := s.manager.List()
	infos := make([]ProcessInfo, len(procs))

	for i, proc := range procs {
		info := ProcessInfo{
			Name:  proc.Name,
			State: proc.State().String(),
		}

		if proc.State() == process.StateExited {
			info.ExitCode = proc.ExitCode()
		}

		if proc.State() == process.StateRunning {
			info.Uptime = proc.Uptime().Round(time.Second).String()
		}

		infos[i] = info
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(infos)
}

// handleLogs returns log history for a process
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	// Get process name from path parameter (Go 1.22+)
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "Process name required", http.StatusBadRequest)
		return
	}

	proc := s.manager.Get(name)
	if proc == nil {
		http.Error(w, "Process not found", http.StatusNotFound)
		return
	}

	lines := proc.Buffer.Lines()
	messages := make([]LogMessage, len(lines))

	for i, line := range lines {
		messages[i] = LogMessage{
			Timestamp: line.Timestamp.Format("15:04:05"),
			Content:   line.Content,
			Stream:    line.Stream,
			Process:   line.Process,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(messages)
}

// handleProcessAction handles start/stop/restart actions
func (s *Server) handleProcessAction(w http.ResponseWriter, r *http.Request) {
	// Get path parameters (Go 1.22+)
	name := r.PathValue("name")
	action := r.PathValue("action")

	if name == "" || action == "" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	proc := s.manager.Get(name)
	if proc == nil {
		http.Error(w, "Process not found", http.StatusNotFound)
		return
	}

	var err error
	switch action {
	case "start":
		err = proc.Start()
	case "stop":
		err = proc.Stop()
	case "restart":
		err = proc.Restart()
	case "clear":
		proc.Buffer.Clear()
	default:
		http.Error(w, "Invalid action", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"action": action,
		"name":   name,
	})
}
