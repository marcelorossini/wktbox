package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

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
	Webtop  string `json:"webtop,omitempty"`
	Gateway string `json:"gateway,omitempty"`
}

type PortSet struct {
	WebtopHTTP  int `json:"webtopHttp,omitempty"`
	WebtopHTTPS int `json:"webtopHttps,omitempty"`
	SSH         int `json:"ssh,omitempty"`
	Gateway     int `json:"gateway,omitempty"`
}

type BoxData struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Status     state.Status `json:"status"`
	Worktree   string       `json:"worktree"`
	Branch     string       `json:"branch,omitempty"`
	URLs       URLs         `json:"urls"`
	Ports      PortSet      `json:"ports"`
	CreatedAt  *time.Time   `json:"createdAt,omitempty"`
	LastUsedAt *time.Time   `json:"lastUsedAt,omitempty"`
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

func boxData(box state.BoxRecord) BoxData {
	data := BoxData{
		ID:       box.ID,
		Name:     box.Name,
		Status:   box.Status,
		Worktree: box.Worktree,
		Branch:   box.Branch,
		URLs: URLs{
			Webtop:  WebtopURL(box),
			Gateway: GatewayURL(box),
		},
	}
	if box.Ports.Size != 0 {
		data.Ports = PortSet{
			WebtopHTTP:  box.Ports.HTTP(),
			WebtopHTTPS: box.Ports.HTTPS(),
			SSH:         box.Ports.SSH(),
			Gateway:     box.Ports.Gateway(),
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
	if url := GatewayURL(box); url != "" {
		fmt.Fprintf(&result, "Gateway:  %s\n", url)
	}
	return result.String()
}

func writeJSON(destination io.Writer, value any) error {
	encoder := json.NewEncoder(destination)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("encode JSON output: %w", err)
	}
	return nil
}
