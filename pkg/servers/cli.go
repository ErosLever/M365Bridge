// Package servers provides CLI and HTTP server implementations for M365 Copilot.
// This file contains the CLI server for interactive and single-query modes.
package servers

import (
	"bufio"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/auth"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/client"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/models"
)

// CLIServer handles command-line interface operations.
type CLIServer struct {
	config       *models.Config
	tokenManager *auth.TokenManager
	m365Client   *client.M365Client
}

// NewCLIServer creates a new CLI server instance.
func NewCLIServer(config *models.Config, tokenManager *auth.TokenManager) *CLIServer {
	return &CLIServer{
		config:       config,
		tokenManager: tokenManager,
	}
}

// Run executes the CLI with the given options.
func (cli *CLIServer) Run(options *CLIOptions) error {
	// Initialize client if needed
	if cli.m365Client == nil {
		cli.m365Client = client.NewM365Client(cli.tokenManager)
		cli.m365Client.SetWebSearchEnabled(cli.config.EnableWebSearch)
	}

	// Handle different modes
	switch {
	case options.ListModels:
		return cli.listModels()
	case options.Interactive:
		return cli.runInteractive(options)
	default:
		return cli.runSingleQuery(options)
	}
}

// CLIOptions represents command-line options.
type CLIOptions struct {
	Model       string
	Reasoning   bool
	Interactive bool
	NoStream    bool
	Prompt      string
	ListModels  bool
}

// listModels prints all available models, sorted by key. A map range would
// print the same list in a different order on every run, which reads as if the
// registry itself had changed.
func (cli *CLIServer) listModels() error {
	PrintModels(os.Stdout)
	return nil
}

// PrintModels writes the model registry compiled into this binary.
//
// It takes no credentials and no server, because listing what this build can
// address is not an upstream question and must work before setup.
func PrintModels(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Available models:")
	for _, key := range slices.Sorted(maps.Keys(models.ModelRegistry)) {
		cfg := models.ModelRegistry[key]
		desc := cfg.Tone
		if cfg.Override != "" {
			desc += fmt.Sprintf(" (%s)", cfg.Override)
		}
		_, _ = fmt.Fprintf(w, "  %-26s %-22s api id: %s\n", key, desc, cfg.OpenAIID)
	}
	_, _ = fmt.Fprintln(w, "\nThe left column is the -model name. The api id is what /v1/models advertises.")
}

// resolveCLIModel reports the requested model, or an error naming where the
// available keys are listed. An unknown name used to fall back to the default
// entry, so the session answered from a tone the caller never asked for.
func resolveCLIModel(key string) (models.ModelConfig, error) {
	cfg, ok := models.FindModel(key)
	if !ok {
		return models.ModelConfig{}, fmt.Errorf("unknown model %q; run --list-models for the available keys", key)
	}
	return cfg, nil
}

// runInteractive starts the interactive mode.
func (cli *CLIServer) runInteractive(options *CLIOptions) error {
	cfg, err := resolveCLIModel(options.Model)
	if err != nil {
		return err
	}
	fmt.Printf("M365 Copilot v%s (Interactive Mode)\n", models.Version)
	fmt.Printf("Model: %s\n", options.Model)
	fmt.Println("Exit: Ctrl+C")

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("\n> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		text := strings.TrimSpace(input)
		if text == "" {
			continue
		}

		tone := cfg.Tone
		if options.Reasoning {
			tone = models.ModelRegistry["reasoning"].Tone
		}

		if options.NoStream {
			result, chatErr := cli.m365Client.Chat(text, tone, cfg.Override, "", cli.config.UserOID, cli.config.TenantID, false)
			err = chatErr
			if err == nil && result != "" {
				fmt.Println(result)
			}
		} else {
			fmt.Println()
			_, err = cli.streamToStdout(text, tone, cfg.Override, "")
			fmt.Println()
		}

		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}
	}
}

// runSingleQuery executes a single query and prints the result.
func (cli *CLIServer) runSingleQuery(options *CLIOptions) error {
	if options.Prompt == "" {
		return fmt.Errorf("prompt is required for single query mode")
	}

	cfg, err := resolveCLIModel(options.Model)
	if err != nil {
		return err
	}
	tone := cfg.Tone
	if options.Reasoning {
		tone = models.ModelRegistry["reasoning"].Tone
	}

	var result string

	if options.NoStream {
		result, err = cli.m365Client.Chat(options.Prompt, tone, cfg.Override, "", cli.config.UserOID, cli.config.TenantID, false)
		if err == nil && result != "" {
			fmt.Println(result)
		}
	} else {
		_, err = cli.streamToStdout(options.Prompt, tone, cfg.Override, "")
	}

	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	return nil
}

// streamToStdout streams the response directly to stdout and returns the full text.
func (cli *CLIServer) streamToStdout(text, tone, gptOverride, convID string) (string, error) {
	fullText := ""
	ch := cli.m365Client.ChatStreamGen(text, tone, gptOverride, convID, cli.config.UserOID, cli.config.TenantID, false)
	for chunk := range ch {
		if chunk.Error != nil {
			return fullText, chunk.Error
		}
		if !chunk.IsFinal {
			fmt.Print(chunk.Text)
			fullText += chunk.Text
		}
	}
	return fullText, nil
}

// Close cleans up resources.
func (cli *CLIServer) Close() error {
	if cli.m365Client != nil {
		return cli.m365Client.Close()
	}
	return nil
}
