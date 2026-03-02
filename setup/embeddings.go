package setup

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/bubblmail/bubblmail/config"
)

// RunEmbeddingsSetup runs the interactive embeddings setup wizard.
func RunEmbeddingsSetup(cfg *config.Config) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Bubblmail embeddings setup")
	fmt.Println("==========================")
	fmt.Println()
	fmt.Println("Configure OpenRouter embeddings for semantic search.")
	fmt.Println("Press Enter to accept the default value shown in [brackets].")
	fmt.Println()

	// -- OpenRouter --
	fmt.Println("-- OpenRouter --")
	fmt.Println()

	apiKey := ""
	apiKeyCmd := ""
	if PromptYesNo(reader, "Store API key in config?", false) {
		apiKey = PromptPassword(reader)
	} else if PromptYesNo(reader, "Use a command to fetch API key?", true) {
		apiKeyCmd = Prompt(reader, "API key command", cfg.Embeddings.APIKeyCmd)
	} else {
		fmt.Println("  Using OPENROUTER_API_KEY from env.")
	}

	model := Prompt(reader, "Embedding model", cfg.Embeddings.Model)
	baseURL := Prompt(reader, "Base URL", cfg.Embeddings.BaseURL)

	model = strings.TrimSpace(model)
	baseURL = strings.TrimSpace(baseURL)

	if model != "" {
		cfg.Embeddings.Model = model
	}
	if baseURL != "" {
		cfg.Embeddings.BaseURL = baseURL
	}
	if apiKey != "" {
		cfg.Embeddings.APIKey = apiKey
		cfg.Embeddings.APIKeyCmd = ""
	} else if apiKeyCmd != "" {
		cfg.Embeddings.APIKeyCmd = apiKeyCmd
		cfg.Embeddings.APIKey = ""
	}

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	configDir, _ := os.UserConfigDir()
	fmt.Printf("Embeddings configuration saved to %s/bubblmail/config.toml\n", configDir)
	return nil
}
