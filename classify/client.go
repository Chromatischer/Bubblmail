package classify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bubblmail/bubblmail/config"
)

// CategoryNone is the value returned when no category matches.
const CategoryNone = "NONE"

// Client calls an OpenAI-compatible chat completions endpoint to classify
// email messages into smart folder categories.
type Client struct {
	apiKey     string
	baseURL    string
	model      string
	categories []string
	httpClient *http.Client
}

// NewClient creates a classification client from config.
// Returns nil, nil when classification is disabled or has no API key — callers
// must check for nil before use.
func NewClient(cfg config.ClassificationConfig, embCfg config.EmbeddingsConfig) (*Client, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	key, err := cfg.ResolveAPIKey(embCfg)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(key) == "" {
		return nil, nil // no key available — silently disabled
	}
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = "google/gemini-2.0-flash-lite-001"
	}
	cats := cfg.Categories
	if len(cats) == 0 {
		cats = config.DefaultCategories
	}
	return &Client{
		apiKey:     key,
		baseURL:    strings.TrimRight(base, "/"),
		model:      model,
		categories: cats,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// Categories returns the configured category list.
func (c *Client) Categories() []string {
	return c.categories
}

// chatRequest is the OpenAI-compatible chat completions request body.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// classifyResult is the expected JSON structure from the model.
type classifyResult struct {
	Category   string  `json:"category"`
	Confidence float32 `json:"confidence"`
}

// Classify sends a single message to the model and returns the category and
// confidence. Returns (CategoryNone, 0, nil) when the model output is
// unparseable rather than an error, so callers can cache the result without
// retrying on every sync.
func (c *Client) Classify(ctx context.Context, from, subject, snippet string) (string, float32, error) {
	systemPrompt := c.buildSystemPrompt()
	userContent := c.buildUserContent(from, subject, snippet)

	reqBody, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userContent},
		},
	})
	if err != nil {
		return CategoryNone, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return CategoryNone, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://bubblmail.local")
	req.Header.Set("X-Title", "bubblmail")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return CategoryNone, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return CategoryNone, 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CategoryNone, 0, fmt.Errorf("classification request failed (%d): %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return CategoryNone, 0, err
	}
	if parsed.Error != nil {
		return CategoryNone, 0, errors.New(parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return CategoryNone, 0, nil
	}

	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	category, confidence := c.parseResult(content)
	return category, confidence, nil
}

// buildSystemPrompt constructs the system prompt with the category list.
func (c *Client) buildSystemPrompt() string {
	catList := strings.Join(c.categories, ", ")
	return fmt.Sprintf(`You are an email classifier. Classify the email into exactly one of these categories: %s, NONE.

Rules:
- IMPORTANT: personal messages, direct requests, urgent matters, action items
- GITHUB: notifications from GitHub, GitLab, Bitbucket (PRs, issues, CI, code reviews)
- DELIVERIES: shipping confirmations, tracking updates, package notifications
- NEWSLETTERS: marketing emails, blog digests, company announcements, mailing lists
- RECEIPTS: purchase confirmations, invoices, billing statements, subscription renewals
- SPAM: unsolicited commercial email, phishing, scams
- NONE: does not fit any category above

Respond with JSON only, no explanation:
{"category": "CATEGORY_NAME", "confidence": 0.95}`, catList)
}

// buildUserContent formats the email fields for classification.
func (c *Client) buildUserContent(from, subject, snippet string) string {
	// Keep input short — subject + snippet is enough; no body fetch needed.
	s := snippet
	if len(s) > 300 {
		s = s[:300]
	}
	return fmt.Sprintf("From: %s\nSubject: %s\nSnippet: %s", from, subject, s)
}

// parseResult extracts the category and confidence from the model's JSON output.
// Falls back to NONE if parsing fails or the category is not in the allowed set.
func (c *Client) parseResult(content string) (string, float32) {
	// Strip potential markdown code fences
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	// Find the JSON object in the response
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start == -1 || end == -1 || end <= start {
		return CategoryNone, 0
	}
	content = content[start : end+1]

	var result classifyResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return CategoryNone, 0
	}

	cat := strings.ToUpper(strings.TrimSpace(result.Category))
	if cat == "" || cat == CategoryNone {
		return CategoryNone, 0
	}

	// Validate against the allowed set
	for _, allowed := range c.categories {
		if strings.ToUpper(allowed) == cat {
			conf := result.Confidence
			if conf <= 0 || conf > 1 {
				conf = 0.5
			}
			return cat, conf
		}
	}
	return CategoryNone, 0
}
