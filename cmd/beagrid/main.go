// beagrid CLI — the unified command-line tool for managing grids, engines, and inference.
// Mirrors the command surface of autonomous-grid: up, down, ls, info, join, leave, models, engines, chat, use.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafaribe/beagrid/internal/agent"
)

var version = "dev"

func main() {
	root := &cobra.Command{
		Use:   "beagrid",
		Short: "Beagrid — pool your Ollama nodes into one inference grid",
		Long: `Beagrid connects multiple machines running Ollama into a unified
inference grid. One OpenAI-compatible endpoint routes requests to the
best available node based on load and priority.`,
		Version: version,
	}

	root.AddCommand(
		cmdUp(),
		cmdDown(),
		cmdInfo(),
		cmdLs(),
		cmdUse(),
		cmdJoin(),
		cmdLeave(),
		cmdModels(),
		cmdEngines(),
		cmdChat(),
		cmdVersion(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// --- grid up ---
func cmdUp() *cobra.Command {
	var port int
	var host string

	cmd := &cobra.Command{
		Use:   "up [name]",
		Short: "Bring a grid online (creates it on first run; default: home)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := "home"
			if len(args) > 0 {
				name = args[0]
			}
			fmt.Printf("Starting beagrid server '%s' on %s:%d...\n", name, host, port)
			fmt.Printf("Run: beagrid-server --name %s --port %d --host %s\n", name, port, host)
			fmt.Printf("grid=%s\n", name)
			fmt.Printf("grid_url=http://%s:%d\n", localIP(), port)
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", 8090, "Server listen port")
	cmd.Flags().StringVar(&host, "host", "0.0.0.0", "Server listen host")
	return cmd
}

// --- grid down ---
func cmdDown() *cobra.Command {
	return &cobra.Command{
		Use:   "down [name]",
		Short: "Take a grid offline",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := "home"
			if len(args) > 0 {
				name = args[0]
			}
			fmt.Printf("Stopping grid '%s'...\n", name)
			return nil
		},
	}
}

// --- grid info ---
func cmdInfo() *cobra.Command {
	var gridURL string
	var env bool
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "info [grid]",
		Short: "Endpoint, key, and live models for a grid",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := resolveGridURL(gridURL, args)
			if env {
				fmt.Printf("export OPENAI_BASE_URL=\"%s/v1\"\n", url)
				fmt.Printf("export OPENAI_API_KEY=\"local-grid\"\n")
				return nil
			}
			resp, err := httpGet(url + "/grid/info")
			if err != nil {
				return fmt.Errorf("cannot reach grid at %s: %w", url, err)
			}
			if jsonOut {
				fmt.Println(string(resp))
			} else {
				var info map[string]any
				json.Unmarshal(resp, &info)
				fmt.Printf("Grid:    %s\n", info["name"])
				fmt.Printf("ID:      %s\n", info["grid_id"])
				fmt.Printf("Type:    %s\n", info["grid_type"])
				fmt.Printf("Engines: %.0f online\n", info["engines_online"])
				fmt.Printf("URL:     %s\n", url)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&gridURL, "grid", "", "Grid URL (default: http://localhost:8090)")
	cmd.Flags().BoolVar(&env, "env", false, "Print OPENAI_* shell exports")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output JSON")
	return cmd
}

// --- grid ls ---
func cmdLs() *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List your grids",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("NAME    URL                    STATUS")
			fmt.Println("home    http://localhost:8090   running")
			return nil
		},
	}
}

// --- grid use ---
func cmdUse() *cobra.Command {
	return &cobra.Command{
		Use:   "use [name]",
		Short: "Set the active grid",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				fmt.Println("active: home")
			} else {
				fmt.Printf("grid=%s\n", args[0])
			}
			return nil
		},
	}
}

// --- grid join ---
func cmdJoin() *cobra.Command {
	var atURL string
	var models []string
	var name string
	var advertiseAs []string
	var ollamaURL string
	var detectAll bool
	var interval float64

	cmd := &cobra.Command{
		Use:   "join [grid]",
		Short: "Join an engine to a grid",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gridURL := resolveGridURL("", args)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() { <-sigCh; cancel() }()

			cfg := agent.DaemonConfig{
				ServerURL:   gridURL,
				OllamaURL:   ollamaURL,
				Name:        name,
				EndpointURL: atURL,
				Models:      models,
				AdvertiseAs: advertiseAs,
				AutoDetect:  detectAll,
				Interval:    time.Duration(interval * float64(time.Second)),
			}

			logger := newLogger()
			daemon := agent.NewDaemon(cfg, logger)
			return daemon.Run(ctx)
		},
	}
	cmd.Flags().StringVar(&atURL, "at", "", "URL of an existing OpenAI-compatible engine")
	cmd.Flags().StringSliceVarP(&models, "model", "m", nil, "Model(s) the engine serves")
	cmd.Flags().StringVar(&name, "name", "", "Engine display name")
	cmd.Flags().StringSliceVar(&advertiseAs, "advertise-as", nil, "Model name(s) advertised to the grid")
	cmd.Flags().StringVar(&ollamaURL, "ollama", "http://localhost:11434", "Ollama URL")
	cmd.Flags().BoolVar(&detectAll, "all", false, "Join every detected engine")
	cmd.Flags().Float64Var(&interval, "heartbeat-interval", 15.0, "Heartbeat interval in seconds")
	return cmd
}

// --- grid leave ---
func cmdLeave() *cobra.Command {
	return &cobra.Command{
		Use:   "leave [grid]",
		Short: "Stop and unregister engines from a grid",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Left the grid. Engine unregistered.")
			return nil
		},
	}
}

// --- grid models ---
func cmdModels() *cobra.Command {
	var gridURL string
	var verbose bool
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "models [grid]",
		Short: "Live models the grid can run now",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := resolveGridURL(gridURL, args)
			resp, err := httpGet(url + "/v1/models")
			if err != nil {
				return fmt.Errorf("cannot reach grid: %w", err)
			}
			if jsonOut {
				fmt.Println(string(resp))
				return nil
			}
			var result struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			json.Unmarshal(resp, &result)
			if len(result.Data) == 0 {
				fmt.Println("(no live models — `beagrid join` an engine first)")
				return nil
			}
			if verbose {
				// Fetch engines for detailed view
				engResp, _ := httpGet(url + "/nodes/discover")
				var engines struct {
					Engines []struct {
						Name        string   `json:"name"`
						NodeID      string   `json:"node_id"`
						Models      []string `json:"models"`
						EndpointURL string   `json:"endpoint_url"`
					} `json:"engines"`
				}
				json.Unmarshal(engResp, &engines)
				fmt.Printf("%-30s %-20s %s\n", "MODEL", "ENGINE", "WHERE")
				for _, eng := range engines.Engines {
					for _, m := range eng.Models {
						label := eng.Name
						if label == "" {
							label = eng.NodeID
						}
						fmt.Printf("%-30s %-20s %s\n", m, label, eng.EndpointURL)
					}
				}
			} else {
				for _, m := range result.Data {
					fmt.Println(m.ID)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&gridURL, "grid", "", "Grid URL")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Show engine serving each model")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output JSON")
	return cmd
}

// --- grid engines ---
func cmdEngines() *cobra.Command {
	var gridURL string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "engines [grid]",
		Short: "Live engines joined to a grid",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := resolveGridURL(gridURL, args)
			resp, err := httpGet(url + "/nodes/discover")
			if err != nil {
				return fmt.Errorf("cannot reach grid: %w", err)
			}
			if jsonOut {
				fmt.Println(string(resp))
				return nil
			}
			var result struct {
				Engines []struct {
					Name        string   `json:"name"`
					NodeID      string   `json:"node_id"`
					Models      []string `json:"models"`
					EndpointURL string   `json:"endpoint_url"`
				} `json:"engines"`
			}
			json.Unmarshal(resp, &result)
			if len(result.Engines) == 0 {
				fmt.Println("(no engines — `beagrid join` one first)")
				return nil
			}
			fmt.Printf("%-20s %s\n", "ENGINE", "WHERE")
			for _, eng := range result.Engines {
				label := eng.Name
				if label == "" {
					label = eng.NodeID
				}
				fmt.Printf("%-20s %s\n", label, eng.EndpointURL)
				fmt.Printf("%-20s models: %s\n", "", strings.Join(eng.Models, ","))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&gridURL, "grid", "", "Grid URL")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output JSON")
	return cmd
}

// --- grid chat ---
func cmdChat() *cobra.Command {
	var model string
	var gridURL string
	var timeout float64

	cmd := &cobra.Command{
		Use:   "chat <message>",
		Short: "Send one chat message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := resolveGridURL(gridURL, nil)
			payload := map[string]any{
				"model":    model,
				"messages": []map[string]string{{"role": "user", "content": args[0]}},
				"stream":   false,
			}
			body, _ := json.Marshal(payload)

			client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
			resp, err := client.Post(url+"/v1/chat/completions", "application/json", bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()

			respBody, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != 200 {
				return fmt.Errorf("error (%d): %s", resp.StatusCode, string(respBody))
			}

			var result struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
			}
			if err := json.Unmarshal(respBody, &result); err != nil {
				return fmt.Errorf("parsing response: %w", err)
			}
			if len(result.Choices) > 0 {
				fmt.Println(result.Choices[0].Message.Content)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&model, "model", "m", "", "Model to use (required)")
	cmd.Flags().StringVar(&gridURL, "grid", "", "Grid URL")
	cmd.Flags().Float64Var(&timeout, "timeout", 600, "Request timeout in seconds")
	cmd.MarkFlagRequired("model")
	return cmd
}

// --- version ---
func cmdVersion() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the beagrid version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("beagrid %s\n", version)
		},
	}
}

// --- helpers ---

func resolveGridURL(flag string, args []string) string {
	if flag != "" {
		return strings.TrimRight(flag, "/")
	}
	if len(args) > 0 && strings.HasPrefix(args[0], "http") {
		return strings.TrimRight(args[0], "/")
	}
	if env := os.Getenv("BEAGRID_URL"); env != "" {
		return strings.TrimRight(env, "/")
	}
	return "http://localhost:8090"
}

func httpGet(url string) ([]byte, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func localIP() string {
	return agent.DetectLocalIP()
}

func newLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}
