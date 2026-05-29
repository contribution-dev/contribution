// Package tools discovers required and optional external analyzers.
package tools

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/contribution-dev/contribution/internal/signals"
)

// Discover checks runtime tools and records graceful degradation.
func Discover(ctx context.Context, includeOptional bool, createdAt time.Time) signals.ToolingReport {
	defs := []struct {
		name     string
		required bool
		args     []string
		fix      string
	}{
		{name: "git", required: true, args: []string{"--version"}, fix: "Install git and ensure it is on PATH."},
		{name: "scc", args: []string{"--version"}, fix: "Install scc for richer language inventory."},
		{name: "semgrep", args: []string{"--version"}, fix: "Install semgrep for static analysis signals."},
		{name: "gitleaks", args: []string{"version"}, fix: "Install gitleaks for secret scanning signals."},
		{name: "osv-scanner", args: []string{"--version"}, fix: "Install osv-scanner for dependency vulnerability signals."},
		{name: "trivy", args: []string{"--version"}, fix: "Install trivy for filesystem or container vulnerability signals."},
	}
	report := signals.ToolingReport{GeneratedAt: createdAt.UTC()}
	for _, def := range defs {
		if !def.required && !includeOptional {
			report.Tools = append(report.Tools, signals.ToolAvailability{
				Name:      def.name,
				Required:  false,
				Available: false,
				Reason:    "Skipped because --no-external-tools was set.",
			})
			report.Limitations = append(report.Limitations, fmt.Sprintf("%s was skipped; related signals are unavailable.", def.name))
			continue
		}
		availability := checkTool(ctx, def.name, def.args, def.required)
		if !availability.Available {
			availability.Reason = strings.TrimSpace(availability.Reason)
			if availability.Reason == "" {
				availability.Reason = def.fix
			}
			if def.required {
				report.Limitations = append(report.Limitations, fmt.Sprintf("Required tool %s is unavailable: %s", def.name, availability.Reason))
			} else {
				report.Limitations = append(report.Limitations, fmt.Sprintf("%s unavailable; %s", def.name, def.fix))
			}
		}
		report.Tools = append(report.Tools, availability)
	}
	return report
}

// Signals converts tool availability into normalized signals.
func Signals(repoID string, tooling signals.ToolingReport, createdAt time.Time) []signals.Signal {
	out := make([]signals.Signal, 0, len(tooling.Tools))
	for _, tool := range tooling.Tools {
		direction := signals.DirectionPositive
		severity := signals.SeverityInfo
		message := fmt.Sprintf("%s is available.", tool.Name)
		value := 1.0
		if !tool.Available {
			direction = signals.DirectionNeutral
			severity = signals.SeverityLow
			value = 0
			message = fmt.Sprintf("%s is unavailable: %s", tool.Name, tool.Reason)
		}
		if tool.Required && !tool.Available {
			severity = signals.SeverityHigh
			direction = signals.DirectionNegative
		}
		sig := signals.New(repoID, tool.Name, "tool_availability", "tool", tool.Name, severity, direction, signals.ConfidenceHigh, value, "boolean", message, true, createdAt)
		sig.Evidence.ToolVersion = tool.Version
		out = append(out, sig)
	}
	return out
}

func checkTool(parent context.Context, name string, args []string, required bool) signals.ToolAvailability {
	availability := signals.ToolAvailability{Name: name, Required: required}
	path, err := exec.LookPath(name)
	if err != nil {
		availability.Reason = "not found on PATH"
		return availability
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	// #nosec G204 -- path comes from exec.LookPath for a fixed tool name in the local definition list.
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- tool names come from the fixed definitions above, not user input.
	cmd := exec.CommandContext(ctx, path, args...)
	out, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		availability.Reason = "version command timed out"
		return availability
	}
	if err != nil {
		availability.Reason = strings.TrimSpace(string(out))
		if availability.Reason == "" {
			availability.Reason = err.Error()
		}
		return availability
	}
	availability.Available = true
	availability.Version = firstLine(string(out))
	return availability
}

func firstLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
