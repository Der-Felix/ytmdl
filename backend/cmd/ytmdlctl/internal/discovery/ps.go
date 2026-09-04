package discovery

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"ytdm/backend/cmd/ytmdlctl/internal/engine"
)

// ServiceStatus holds runtime container status for a compose service.
type ServiceStatus struct {
	Name   string // service name (e.g. "backend", "frontend", "db")
	State  string // "running", "exited", "created", etc.
	Health string // "healthy", "unhealthy", "starting", "none", etc.
}

// rawServiceJSON captures fields from Docker/Podman JSON compose ps output.
type rawServiceJSON struct {
	Service string `json:"Service"`
	Name    string `json:"Name"`
	State   string `json:"State"`
	Status  string `json:"Status"`
	Health  string `json:"Health"`
}

// InspectServices queries the container engine for service statuses.
func InspectServices(ctx context.Context, eng engine.Engine, projectDir, composeFile string) (map[string]ServiceStatus, error) {
	result := make(map[string]ServiceStatus)

	// Try machine-readable JSON format first
	res, err := eng.PS(ctx, projectDir, composeFile, "--format", "json")
	if err == nil && len(res.Stdout) > 0 {
		if parsePSJSON(res.Stdout, result) {
			return result, nil
		}
	}

	// Fallback to standard tabular ps
	res, err = eng.PS(ctx, projectDir, composeFile)
	if err != nil {
		return nil, err
	}

	parsePSTabular(res.Stdout, result)
	return result, nil
}

func parsePSJSON(data []byte, out map[string]ServiceStatus) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return false
	}

	// Try JSON array first
	if trimmed[0] == '[' {
		var list []rawServiceJSON
		if err := json.Unmarshal(trimmed, &list); err == nil {
			for _, item := range list {
				addRawService(item, out)
			}
			return len(out) > 0
		}
	}

	// Try line-delimited NDJSON
	scanner := bufio.NewScanner(bytes.NewReader(trimmed))
	foundAny := false
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var item rawServiceJSON
		if err := json.Unmarshal(line, &item); err == nil {
			addRawService(item, out)
			foundAny = true
		}
	}

	return foundAny
}

func addRawService(item rawServiceJSON, out map[string]ServiceStatus) {
	svc := strings.TrimSpace(item.Service)
	if svc == "" {
		svc = strings.TrimSpace(item.Name)
	}
	if svc == "" {
		return
	}

	state := strings.ToLower(strings.TrimSpace(item.State))
	if state == "" && strings.Contains(strings.ToLower(item.Status), "up") {
		state = "running"
	} else if state == "" {
		state = "unknown"
	}

	health := strings.ToLower(strings.TrimSpace(item.Health))
	if health == "" {
		statusLower := strings.ToLower(item.Status)
		if strings.Contains(statusLower, "(healthy)") {
			health = "healthy"
		} else if strings.Contains(statusLower, "(unhealthy)") {
			health = "unhealthy"
		} else if strings.Contains(statusLower, "(health: starting)") {
			health = "starting"
		} else {
			health = "none"
		}
	}

	out[svc] = ServiceStatus{
		Name:   svc,
		State:  state,
		Health: health,
	}
}

func parsePSTabular(data []byte, out map[string]ServiceStatus) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	headerParsed := false
	serviceCol := -1
	statusCol := -1

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if !headerParsed {
			// Find column indices
			for i, f := range fields {
				switch strings.ToUpper(f) {
				case "SERVICE":
					serviceCol = i
				case "STATUS":
					statusCol = i
				}
			}
			headerParsed = true
			continue
		}

		if len(fields) <= serviceCol || serviceCol == -1 {
			// Try basic matching for standard service names
			for _, known := range []string{"backend", "frontend", "db"} {
				if strings.Contains(line, known) {
					state := "running"
					health := "none"
					if strings.Contains(line, "healthy") {
						health = "healthy"
					}
					out[known] = ServiceStatus{
						Name:   known,
						State:  state,
						Health: health,
					}
				}
			}
			continue
		}

		svc := fields[serviceCol]
		statusStr := ""
		if statusCol != -1 && len(fields) > statusCol {
			statusStr = strings.Join(fields[statusCol:], " ")
		}

		state := "running"
		if !strings.Contains(strings.ToLower(statusStr), "up") {
			state = "stopped"
		}

		health := "none"
		if strings.Contains(strings.ToLower(statusStr), "(healthy)") {
			health = "healthy"
		} else if strings.Contains(strings.ToLower(statusStr), "(unhealthy)") {
			health = "unhealthy"
		}

		out[svc] = ServiceStatus{
			Name:   svc,
			State:  state,
			Health: health,
		}
	}
}
