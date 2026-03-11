package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/bubblmail/bubblmail/cache"
	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	imaplib "github.com/bubblmail/bubblmail/imap"
)

const (
	defaultBaseURL = "https://openrouter.ai/api/v1"
	defaultLimit   = 20
	defaultMailbox = "INBOX"
	defaultTimeout = 30 * time.Second
)

var defaultModels = []string{
	"google/gemini-2.0-flash-lite-001",
	"openai/gpt-5-mini",
	"openai/gpt-5-nano",
}

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

type tipicalEvent struct {
	HasEvent    bool   `json:"hasEvent"`
	Summary     string `json:"summary,omitempty"`
	Date        string `json:"date,omitempty"`
	Start       string `json:"start,omitempty"`
	End         string `json:"end,omitempty"`
	Location    string `json:"location,omitempty"`
	Calendar    string `json:"calendar,omitempty"`
	Status      string `json:"status,omitempty"`
	AllDay      bool   `json:"allDay,omitempty"`
	Recurring   bool   `json:"recurring,omitempty"`
	Description string `json:"description,omitempty"`
}

type demoResult struct {
	Model      string
	Latency    time.Duration
	Raw        string
	Parsed     *tipicalEvent
	ParseError error
	CallError  error
}

type messageSample struct {
	Message *data.Message
	Body    string
}

type demoClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func Main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "demo error:", err)
		os.Exit(1)
	}
}

func run() error {
	accountFlag := flag.String("account", "", "Account to sample (defaults to config default_account or first account)")
	mailboxFlag := flag.String("mailbox", defaultMailbox, "Mailbox to sample")
	limitFlag := flag.Int("limit", defaultLimit, "Number of messages to sample")
	refreshFlag := flag.Bool("refresh", false, "Refresh mailbox from IMAP before running demo")
	baseURLFlag := flag.String("base-url", "", "Override OpenRouter base URL")
	calendarFlag := flag.String("calendar", "Work", "Calendar name to place in TipiCal-style JSON")
	modelAFlag := flag.String("model-a", defaultModels[0], "First model to compare")
	modelBFlag := flag.String("model-b", defaultModels[1], "Second model to compare")
	modelCFlag := flag.String("model-c", defaultModels[2], "Third model to compare")
	flag.Parse()

	if *limitFlag <= 0 {
		return errors.New("limit must be positive")
	}

	cfg, store, err := loadConfigAndStore()
	if err != nil {
		return err
	}
	defer store.Close()

	account, err := chooseAccount(cfg, *accountFlag)
	if err != nil {
		return err
	}

	if *refreshFlag {
		fmt.Printf("Refreshing %s/%s...\n", account.Name, *mailboxFlag)
		if err := refreshMailbox(cfg, store, account, *mailboxFlag, *limitFlag); err != nil {
			return err
		}
	}

	apiKey, err := cfg.Classification.ResolveAPIKey(cfg.Embeddings)
	if err != nil {
		return fmt.Errorf("resolve OpenRouter API key: %w", err)
	}
	if strings.TrimSpace(apiKey) == "" {
		return errors.New("missing OpenRouter API key in classification/api_key, classification/api_key_cmd, embeddings/api_key, or embeddings/api_key_cmd")
	}

	baseURL := strings.TrimSpace(*baseURLFlag)
	if baseURL == "" {
		baseURL = strings.TrimSpace(cfg.Classification.BaseURL)
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	models := uniqueNonEmpty([]string{*modelAFlag, *modelBFlag, *modelCFlag})
	if len(models) == 0 {
		return errors.New("at least one model is required")
	}

	samples, err := loadSamples(store, account.Name, *mailboxFlag, *limitFlag)
	if err != nil {
		return err
	}
	if len(samples) == 0 {
		return fmt.Errorf("no cached messages found for %s/%s; run with --refresh or sync the mailbox first", account.Name, *mailboxFlag)
	}

	client := &demoClient{
		apiKey:     apiKey,
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: defaultTimeout},
	}

	fmt.Printf("Bubblmail -> TipiCal event extraction demo\n")
	fmt.Printf("Account: %s\nMailbox: %s\nMessages: %d\nModels: %s\n\n", account.Name, *mailboxFlag, len(samples), strings.Join(models, ", "))

	stats := make(map[string]*modelStats, len(models))
	for _, model := range models {
		stats[model] = &modelStats{}
	}

	for i, sample := range samples {
		msg := sample.Message
		fmt.Printf("[%02d/%02d] %s | %s\n", i+1, len(samples), trimForDisplay(msg.FromString(), 28), trimForDisplay(msg.Subject, 72))
		for _, model := range models {
			res := client.extractEvent(context.Background(), model, *calendarFlag, sample)
			stats[model].add(res)
			printResult(model, res)
		}
		fmt.Println()
	}

	printSummary(stats)
	return nil
}

type modelStats struct {
	Calls        int
	ValidJSON    int
	HasEvent     int
	CallFailures int
	TotalLatency time.Duration
}

func (s *modelStats) add(res demoResult) {
	s.Calls++
	s.TotalLatency += res.Latency
	if res.CallError != nil {
		s.CallFailures++
		return
	}
	if res.ParseError == nil && res.Parsed != nil {
		s.ValidJSON++
		if res.Parsed.HasEvent {
			s.HasEvent++
		}
	}
}

func printResult(model string, res demoResult) {
	ms := res.Latency.Milliseconds()
	if res.CallError != nil {
		fmt.Printf("  - %s | call error | %d ms | %s\n", model, ms, res.CallError)
		return
	}
	if res.ParseError != nil {
		fmt.Printf("  - %s | parse error | %d ms\n", model, ms)
		fmt.Printf("    raw: %s\n", trimForDisplay(res.Raw, 180))
		return
	}
	payload, _ := json.Marshal(res.Parsed)
	state := "no-event"
	if res.Parsed.HasEvent {
		state = "event"
	}
	fmt.Printf("  - %s | %s | %d ms | %s\n", model, state, ms, string(payload))
}

func printSummary(stats map[string]*modelStats) {
	models := make([]string, 0, len(stats))
	for model := range stats {
		models = append(models, model)
	}
	sort.Strings(models)

	fmt.Println("Summary")
	for _, model := range models {
		s := stats[model]
		avg := int64(0)
		if s.Calls > 0 {
			avg = s.TotalLatency.Milliseconds() / int64(s.Calls)
		}
		fmt.Printf("- %s | calls=%d valid_json=%d has_event=%d call_failures=%d avg_ms=%d\n",
			model, s.Calls, s.ValidJSON, s.HasEvent, s.CallFailures, avg)
	}
}

func (c *demoClient) extractEvent(ctx context.Context, model, calendar string, sample messageSample) demoResult {
	start := time.Now()
	raw, callErr := c.callModel(ctx, model, buildSystemPrompt(calendar), buildUserPrompt(sample))
	res := demoResult{
		Model:     model,
		Latency:   time.Since(start),
		Raw:       raw,
		CallError: callErr,
	}
	if callErr != nil {
		return res
	}
	parsed, parseErr := parseTipicalJSON(raw)
	res.Parsed = parsed
	res.ParseError = parseErr
	return res
}

func (c *demoClient) callModel(ctx context.Context, model, systemPrompt, userPrompt string) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://bubblmail.local")
	req.Header.Set("X-Title", "bubblmail-demo")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", err
	}
	if parsed.Error != nil {
		return "", errors.New(parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("empty choices")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

func buildSystemPrompt(calendar string) string {
	return fmt.Sprintf(`You extract calendar events from email and emit JSON for TipiCal.

Return JSON only.
If the email does not clearly contain a calendar-worthy event, return exactly:
{"hasEvent":false}

If it does contain an event, return exactly one JSON object with this shape:
{
  "hasEvent": true,
  "summary": "Short event title",
  "date": "YYYY-MM-DD",
  "start": "HH:MM",
  "end": "HH:MM",
  "location": "",
  "calendar": %q,
  "status": "CONFIRMED",
  "allDay": false,
  "recurring": false,
  "description": "Short factual description"
}

Rules:
- Do not invent missing dates, times, or location.
- Use 24-hour times.
- Use empty strings for unknown string fields when hasEvent is true.
- Set allDay true only for explicit all-day events.
- Set recurring true only when the email clearly says it repeats.
- Prefer the concrete event from the mail body over generic mailbox context.
- Do not wrap the JSON in markdown fences.`, calendar)
}

func buildUserPrompt(sample messageSample) string {
	msg := sample.Message
	date := ""
	if !msg.Date.IsZero() {
		date = msg.Date.Format(time.RFC3339)
	}
	body := strings.TrimSpace(sample.Body)
	if body == "" {
		body = strings.TrimSpace(msg.Snippet)
	}
	body = trimForPrompt(body, 4000)
	return fmt.Sprintf("From: %s\nSubject: %s\nReceived: %s\nMailbox: %s\nSnippet: %s\nBody:\n%s",
		msg.FromString(), msg.Subject, date, msg.FolderName, trimForPrompt(msg.Snippet, 500), body)
}

func parseTipicalJSON(raw string) (*tipicalEvent, error) {
	content := strings.TrimSpace(raw)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start == -1 || end == -1 || end < start {
		return nil, errors.New("no json object found")
	}
	content = content[start : end+1]

	var event tipicalEvent
	if err := json.Unmarshal([]byte(content), &event); err != nil {
		return nil, err
	}
	if !event.HasEvent {
		return &tipicalEvent{HasEvent: false}, nil
	}
	if strings.TrimSpace(event.Calendar) == "" {
		return nil, errors.New("missing calendar")
	}
	return &event, nil
}

func loadSamples(store *cache.Store, account, mailbox string, limit int) ([]messageSample, error) {
	msgs, err := store.GetMessagesFiltered(account, mailbox, false, limit)
	if err != nil {
		return nil, err
	}
	samples := make([]messageSample, 0, len(msgs))
	for _, msg := range msgs {
		body, _, err := store.GetBody(msg.ID)
		if err != nil {
			return nil, err
		}
		samples = append(samples, messageSample{Message: msg, Body: body})
	}
	return samples, nil
}

func chooseAccount(cfg *config.Config, requested string) (*config.AccountConfig, error) {
	if len(cfg.Accounts) == 0 {
		return nil, errors.New("no accounts configured")
	}
	if requested != "" {
		for i := range cfg.Accounts {
			if cfg.Accounts[i].Name == requested {
				return &cfg.Accounts[i], nil
			}
		}
		return nil, fmt.Errorf("account %q not found", requested)
	}
	if cfg.General.DefaultAccount != "" {
		for i := range cfg.Accounts {
			if cfg.Accounts[i].Name == cfg.General.DefaultAccount {
				return &cfg.Accounts[i], nil
			}
		}
	}
	return &cfg.Accounts[0], nil
}

func loadConfigAndStore() (*config.Config, *cache.Store, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}
	cacheDir := cfg.Cache.Dir
	if cacheDir == "" {
		userCache, err := os.UserCacheDir()
		if err != nil {
			return nil, nil, fmt.Errorf("finding cache dir: %w", err)
		}
		cacheDir = userCache + "/bubblmail"
	}
	store, err := cache.Open(cacheDir)
	if err != nil {
		return nil, nil, fmt.Errorf("opening cache: %w", err)
	}
	return cfg, store, nil
}

func refreshMailbox(_ *config.Config, store *cache.Store, account *config.AccountConfig, mailbox string, limit int) error {
	client, err := imaplib.Connect(account)
	if err != nil {
		return err
	}
	defer client.Close()

	msg := client.FetchMessages(mailbox, limit)()
	typed, ok := msg.(imaplib.MessageListMsg)
	if ok {
		if typed.Err != nil {
			return typed.Err
		}
		return store.UpsertMessages(typed.Messages)
	}

	return errors.New("unexpected IMAP response type")
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func trimForPrompt(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return strings.TrimSpace(s[:max])
}

func trimForDisplay(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}
