package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"wktbox/internal/agentintegration"
	"wktbox/internal/connection"
	"wktbox/internal/loopback"
	"wktbox/internal/prune"
	"wktbox/internal/state"
)

type Options struct {
	JSON  bool
	Quiet bool
	Out   io.Writer
	Err   io.Writer
}

type Renderer struct {
	json  bool
	quiet bool
	out   io.Writer
	err   io.Writer
}

type URLs struct {
	Webtop     string `json:"webtop,omitempty"`
	Gateway    string `json:"gateway,omitempty"`
	BrowserCDP string `json:"browserCdp,omitempty"`
}

type PortSet struct {
	WebtopHTTP  int `json:"webtopHttp,omitempty"`
	WebtopHTTPS int `json:"webtopHttps,omitempty"`
	SSH         int `json:"ssh,omitempty"`
	Gateway     int `json:"gateway,omitempty"`
	BrowserCDP  int `json:"browserCdp,omitempty"`
}

type BoxData struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Status     state.Status    `json:"status"`
	Worktree   string          `json:"worktree"`
	Branch     string          `json:"branch,omitempty"`
	URLs       URLs            `json:"urls"`
	Ports      PortSet         `json:"ports"`
	CreatedAt  *time.Time      `json:"createdAt,omitempty"`
	LastUsedAt *time.Time      `json:"lastUsedAt,omitempty"`
	Loopback   loopback.Status `json:"loopback"`
}

type ConnectionMemberData struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Status state.Status `json:"status"`
	Alias  string       `json:"alias"`
}

type ConnectionData struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Network      string                 `json:"network"`
	State        state.ConnectionStatus `json:"state"`
	Members      []ConnectionMemberData `json:"members"`
	CreatedAt    time.Time              `json:"createdAt"`
	ReconciledAt *time.Time             `json:"reconciledAt,omitempty"`
	Error        string                 `json:"error,omitempty"`
}

func New(options Options) Renderer {
	out := options.Out
	if out == nil {
		out = io.Discard
	}
	errOutput := options.Err
	if errOutput == nil {
		errOutput = io.Discard
	}
	return Renderer{
		json:  options.JSON,
		quiet: options.Quiet,
		out:   out,
		err:   errOutput,
	}
}

func (renderer Renderer) Box(box state.BoxRecord) error {
	if renderer.json {
		return writeJSON(renderer.out, boxData(box))
	}
	if renderer.quiet {
		return nil
	}
	_, err := fmt.Fprint(renderer.out, humanBox(box))
	return err
}

func (renderer Renderer) Boxes(boxes []state.BoxRecord) error {
	if renderer.json {
		data := make([]BoxData, 0, len(boxes))
		for _, box := range boxes {
			data = append(data, boxData(box))
		}
		return writeJSON(renderer.out, data)
	}
	if renderer.quiet {
		return nil
	}
	if len(boxes) == 0 {
		_, err := fmt.Fprintln(renderer.out, "No boxes found.")
		return err
	}
	for index, box := range boxes {
		if index > 0 {
			if _, err := fmt.Fprintln(renderer.out); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprint(renderer.out, humanBox(box)); err != nil {
			return err
		}
	}
	return nil
}

func (renderer Renderer) Connection(
	record state.ConnectionRecord,
	boxes map[string]state.BoxRecord,
) error {
	if renderer.json {
		return writeJSON(renderer.out, connectionData(record, boxes))
	}
	return renderer.Connections([]state.ConnectionRecord{record}, boxes)
}

func (renderer Renderer) Connections(
	records []state.ConnectionRecord,
	boxes map[string]state.BoxRecord,
) error {
	if renderer.json {
		data := make([]ConnectionData, 0, len(records))
		for _, record := range records {
			data = append(data, connectionData(record, boxes))
		}
		return writeJSON(renderer.out, data)
	}
	if renderer.quiet {
		return nil
	}
	if len(records) == 0 {
		_, err := fmt.Fprintln(renderer.out, "No connections found.")
		return err
	}
	for index, record := range records {
		if index > 0 {
			if _, err := fmt.Fprintln(renderer.out); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprint(
			renderer.out,
			humanConnection(record, boxes),
		); err != nil {
			return err
		}
	}
	return nil
}

func (renderer Renderer) Value(value any) error {
	if renderer.json {
		return writeJSON(renderer.out, value)
	}
	if renderer.quiet {
		return nil
	}
	_, err := fmt.Fprintln(renderer.out, value)
	return err
}

func (renderer Renderer) Message(message string) error {
	if renderer.quiet {
		return nil
	}
	if renderer.json {
		return writeJSON(renderer.out, map[string]string{"message": message})
	}
	_, err := fmt.Fprintln(renderer.out, message)
	return err
}

func (renderer Renderer) Error(code string, message string) error {
	if renderer.json {
		return writeJSON(renderer.err, map[string]any{
			"error": map[string]string{
				"code":    code,
				"message": message,
			},
		})
	}
	_, err := fmt.Fprintln(renderer.err, "Error:", message)
	return err
}

func (renderer Renderer) LoopbackSummary(status loopback.Status) error {
	if renderer.quiet {
		return nil
	}
	if renderer.json {
		return writeJSON(renderer.err, map[string]any{"loopback": status})
	}
	_, err := fmt.Fprint(renderer.err, humanLoopback(status, true))
	return err
}

func (renderer Renderer) AgentReport(report agentintegration.Report) error {
	if renderer.json {
		return writeJSON(renderer.out, report)
	}
	if renderer.quiet {
		return nil
	}
	for index, status := range report.Targets {
		if index > 0 {
			if _, err := fmt.Fprintln(renderer.out); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprint(renderer.out, humanAgentStatus(status)); err != nil {
			return err
		}
	}
	return nil
}

func (renderer Renderer) PruneReport(report prune.Report, force bool) error {
	if renderer.json {
		return writeJSON(renderer.out, report)
	}
	if renderer.quiet {
		return nil
	}
	if force {
		if len(report.Destroyed) == 0 {
			if _, err := fmt.Fprintln(
				renderer.out,
				"No stale boxes destroyed.",
			); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintln(renderer.out, "Destroyed stale boxes:"); err != nil {
				return err
			}
			if err := writeCandidates(renderer.out, report.Destroyed); err != nil {
				return err
			}
		}
	} else {
		if len(report.Candidates) == 0 {
			if _, err := fmt.Fprintln(
				renderer.out,
				"No stale boxes found.",
			); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintln(
				renderer.out,
				"Stale boxes found (dry run):",
			); err != nil {
				return err
			}
			if err := writeCandidates(renderer.out, report.Candidates); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(
				renderer.out,
				"Run \"wktbox prune --force\" to destroy only these boxes.",
			); err != nil {
				return err
			}
		}
	}
	for _, warning := range report.Warnings {
		if _, err := fmt.Fprintf(
			renderer.out,
			"warning: box %s path %q: %s\n",
			warning.ID,
			warning.Path,
			warning.Message,
		); err != nil {
			return err
		}
	}
	return nil
}

func writeCandidates(destination io.Writer, candidates []prune.Candidate) error {
	for _, candidate := range candidates {
		name := candidate.Name
		if name == "" {
			name = candidate.ID
		}
		if _, err := fmt.Fprintf(
			destination,
			"  - %s (%s): %s\n",
			name,
			candidate.ID,
			candidate.Worktree,
		); err != nil {
			return err
		}
	}
	return nil
}

func humanAgentStatus(status agentintegration.TargetStatus) string {
	var result strings.Builder
	state := "not installed"
	if status.Installed {
		state = "installed"
	}
	fmt.Fprintf(&result, "Agent %s: %s\n", status.Target, state)
	if status.Version != "" {
		fmt.Fprintf(&result, "Version:      %s\n", status.Version)
	}
	fmt.Fprintf(&result, "Skill:        %s\n", status.SkillPath)
	fmt.Fprintf(&result, "Instructions: %s\n", status.InstructionsPath)
	fmt.Fprintf(&result, "Managed block: %t\n", status.ManagedBlock)
	if status.Changed {
		result.WriteString("Changes:      required\n")
	}
	if status.Conflict {
		result.WriteString("conflict: installed skill has local modifications\n")
		fmt.Fprintf(
			&result,
			"Remediation: wktbox agents install --target %s --force\n",
			status.Target,
		)
	}
	return result.String()
}

func WebtopURL(box state.BoxRecord) string {
	if box.Ports.Size == 0 {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", box.Ports.HTTP())
}

func GatewayURL(box state.BoxRecord) string {
	if !box.GatewayEnabled || box.Ports.Size == 0 {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", box.Ports.Gateway())
}

func BrowserCDPURL(box state.BoxRecord) string {
	if box.Ports.Size < 5 {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", box.Ports.BrowserCDP())
}

func boxData(box state.BoxRecord) BoxData {
	data := BoxData{
		ID:       box.ID,
		Name:     box.Name,
		Status:   box.Status,
		Worktree: box.Worktree,
		Branch:   box.Branch,
		Loopback: box.Loopback,
		URLs: URLs{
			Webtop:     WebtopURL(box),
			Gateway:    GatewayURL(box),
			BrowserCDP: BrowserCDPURL(box),
		},
	}
	if box.Ports.Size != 0 {
		data.Ports = PortSet{
			WebtopHTTP:  box.Ports.HTTP(),
			WebtopHTTPS: box.Ports.HTTPS(),
			SSH:         box.Ports.SSH(),
			Gateway:     box.Ports.Gateway(),
			BrowserCDP:  box.Ports.BrowserCDP(),
		}
	}
	if !box.CreatedAt.IsZero() {
		createdAt := box.CreatedAt
		data.CreatedAt = &createdAt
	}
	if !box.LastUsedAt.IsZero() {
		lastUsedAt := box.LastUsedAt
		data.LastUsedAt = &lastUsedAt
	}
	return data
}

func connectionData(
	record state.ConnectionRecord,
	boxes map[string]state.BoxRecord,
) ConnectionData {
	name := record.Name
	if name == "" {
		name = record.ID
	}
	data := ConnectionData{
		ID:        record.ID,
		Name:      name,
		Network:   record.Network,
		State:     record.Status,
		Members:   make([]ConnectionMemberData, 0, len(record.Members)),
		CreatedAt: record.CreatedAt,
		Error:     record.Error,
	}
	if !record.ReconciledAt.IsZero() {
		reconciledAt := record.ReconciledAt
		data.ReconciledAt = &reconciledAt
	}
	for _, id := range record.Members {
		box, exists := boxes[id]
		memberName := id
		memberStatus := state.Error
		if exists {
			if box.Name != "" {
				memberName = box.Name
			}
			memberStatus = box.Status
		}
		data.Members = append(data.Members, ConnectionMemberData{
			ID:     id,
			Name:   memberName,
			Status: memberStatus,
			Alias:  connection.Alias(id),
		})
	}
	return data
}

func humanBox(box state.BoxRecord) string {
	var result strings.Builder
	name := box.Name
	if name == "" {
		name = box.ID
	}
	fmt.Fprintf(&result, "Box %s (%s) is %s\n", name, box.ID, box.Status)
	if box.Worktree != "" {
		fmt.Fprintf(&result, "Worktree: %s\n", box.Worktree)
	}
	if box.Branch != "" {
		fmt.Fprintf(&result, "Branch:   %s\n", box.Branch)
	}
	if url := WebtopURL(box); url != "" {
		fmt.Fprintf(&result, "Webtop:   %s\n", url)
	}
	if url := BrowserCDPURL(box); url != "" {
		fmt.Fprintf(&result, "Browser CDP: %s\n", url)
	}
	if url := GatewayURL(box); url != "" {
		fmt.Fprintf(&result, "Gateway:  %s\n", url)
	}
	if box.Loopback.EventStream != "" {
		result.WriteString(humanLoopback(box.Loopback, false))
	}
	return result.String()
}

func humanConnection(
	record state.ConnectionRecord,
	boxes map[string]state.BoxRecord,
) string {
	data := connectionData(record, boxes)
	var result strings.Builder
	fmt.Fprintf(
		&result,
		"Connection %s (%s) is %s\n",
		data.Name,
		data.ID,
		data.State,
	)
	fmt.Fprintf(&result, "Network: %s\n", data.Network)
	result.WriteString("Members:\n")
	for _, member := range data.Members {
		fmt.Fprintf(
			&result,
			"  %s (%s) -> %s [%s]\n",
			member.Name,
			member.ID,
			member.Alias,
			member.Status,
		)
	}
	if data.Error != "" {
		fmt.Fprintf(&result, "Error: %s\n", data.Error)
	}
	return result.String()
}

func humanLoopback(status loopback.Status, summary bool) string {
	var result strings.Builder
	if summary {
		result.WriteString("Automatic localhost routes inside Webtop:\n")
	} else {
		fmt.Fprintf(&result, "Loopback event stream: %s\n", status.EventStream)
	}
	for _, route := range status.Routes {
		if route.State == loopback.RouteConflict {
			fmt.Fprintf(
				&result,
				"  localhost:%d conflict: %s\n",
				route.Port,
				route.Error,
			)
			continue
		}
		switch routeScheme(route.Port) {
		case "http", "https":
			fmt.Fprintf(
				&result,
				"  %s://localhost:%d\n",
				routeScheme(route.Port),
				route.Port,
			)
		default:
			fmt.Fprintf(
				&result,
				"  localhost:%d -> %s\n",
				route.Port,
				route.Target,
			)
		}
	}
	for _, warning := range status.Warnings {
		fmt.Fprintf(&result, "  warning: %s\n", warning.Message)
	}
	return result.String()
}

func routeScheme(port uint16) string {
	switch port {
	case 80, 3000, 4173, 5000, 5173, 8000, 8080:
		return "http"
	case 443, 8443:
		return "https"
	default:
		return ""
	}
}

func writeJSON(destination io.Writer, value any) error {
	encoder := json.NewEncoder(destination)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("encode JSON output: %w", err)
	}
	return nil
}
