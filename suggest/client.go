package suggest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/bubblmail/bubblmail/config"
	"github.com/bubblmail/bubblmail/data"
	"github.com/bubblmail/bubblmail/embeddings"
)

const defaultModel = "google/gemini-3-flash-preview"

type Client struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
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

type payload struct {
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

func NewClient(cfg config.ClassificationConfig, embCfg config.EmbeddingsConfig) (*Client, error) {
	key, err := cfg.ResolveAPIKey(embCfg)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(key) == "" {
		return nil, nil
	}
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = "https://openrouter.ai/api/v1"
	}
	return &Client{
		apiKey:     key,
		baseURL:    strings.TrimRight(base, "/"),
		model:      defaultModel,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}, nil
}

func (c *Client) Model() string {
	if c == nil || c.model == "" {
		return defaultModel
	}
	return c.model
}

func (c *Client) Extract(ctx context.Context, msg *data.Message, bodyText, bodyHTML string) (*data.SuggestedEvent, error) {
	if c == nil {
		return nil, errors.New("suggest client is nil")
	}
	content := buildUserPrompt(msg, bodyText, bodyHTML)
	sourceHash := embeddings.HashContent(content)
	raw, err := c.chat(ctx, buildSystemPrompt(), content)
	if err != nil {
		return &data.SuggestedEvent{
			MessageID:    msg.ID,
			Model:        c.model,
			GeneratedAt:  time.Now().Unix(),
			SourceHash:   sourceHash,
			ParseError:   err.Error(),
			GenerationOK: false,
		}, err
	}
	p, parseErr := parsePayload(raw)
	ev := &data.SuggestedEvent{
		MessageID:    msg.ID,
		Model:        c.model,
		GeneratedAt:  time.Now().Unix(),
		SourceHash:   sourceHash,
		JSONText:     compactJSON(raw),
		GenerationOK: parseErr == nil,
	}
	if parseErr != nil {
		ev.ParseError = parseErr.Error()
		return ev, parseErr
	}
	ev.HasEvent = p.HasEvent
	ev.Summary = p.Summary
	ev.Date = p.Date
	ev.Start = p.Start
	ev.End = p.End
	ev.Location = p.Location
	ev.Calendar = p.Calendar
	ev.Status = p.Status
	ev.AllDay = p.AllDay
	ev.Recurring = p.Recurring
	ev.Description = p.Description
	ev.PlainText = formatPlainText(ev)
	if ev.JSONText == "" {
		buf, _ := json.Marshal(p)
		ev.JSONText = string(buf)
	}
	return ev, nil
}

func (c *Client) chat(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model: c.model,
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
	req.Header.Set("X-Title", "bubblmail")
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
		return "", fmt.Errorf("suggested event request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
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

func buildSystemPrompt() string {
	return `You extract calendar events from email and emit JSON for TipiCal.

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
  "calendar": "Work",
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
- Do not wrap JSON in markdown fences.`
}

func buildUserPrompt(msg *data.Message, bodyText, bodyHTML string) string {
	date := ""
	if !msg.Date.IsZero() {
		date = msg.Date.Format(time.RFC3339)
	}
	body := bestBodyText(bodyText, bodyHTML)
	if body == "" {
		body = strings.TrimSpace(msg.Snippet)
	}
	if len(body) > 4000 {
		body = body[:4000]
	}
	return fmt.Sprintf("From: %s\nSubject: %s\nReceived: %s\nSnippet: %s\nBody:\n%s",
		msg.FromString(), msg.Subject, date, msg.Snippet, body)
}

func bestBodyText(bodyText, bodyHTML string) string {
	body := strings.TrimSpace(bodyText)
	if body != "" && !looksLikeHTMLFallbackStub(body) {
		return body
	}
	htmlText := htmlToText(bodyHTML)
	if strings.TrimSpace(htmlText) != "" {
		return htmlText
	}
	return body
}

func looksLikeHTMLFallbackStub(s string) bool {
	norm := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
	return norm == "please enable html in your email program to properly display this message."
}

var htmlTagRE = regexp.MustCompile(`(?s)<[^>]*>`)

func htmlToText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"<br>", "\n", "<br/>", "\n", "<br />", "\n",
		"</p>", "\n\n", "</div>", "\n", "</li>", "\n",
		"</tr>", "\n", "</td>", " ",
	)
	replaced := replacer.Replace(s)
	replaced = htmlTagRE.ReplaceAllString(replaced, " ")
	s = replaced
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func parsePayload(raw string) (*payload, error) {
	content := strings.TrimSpace(raw)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, errors.New("no json object found")
	}
	content = content[start : end+1]
	var p payload
	if err := json.Unmarshal([]byte(content), &p); err != nil {
		return nil, err
	}
	if !p.HasEvent {
		return &payload{HasEvent: false}, nil
	}
	if strings.TrimSpace(p.Calendar) == "" {
		p.Calendar = "Work"
	}
	if strings.TrimSpace(p.Status) == "" {
		p.Status = "CONFIRMED"
	}
	return &p, nil
}

func formatPlainText(ev *data.SuggestedEvent) string {
	if ev == nil || !ev.HasEvent {
		return "No suggested event found"
	}
	var lines []string
	lines = append(lines, ev.Summary)
	if ev.Date != "" {
		lines = append(lines, "Date: "+ev.Date)
	}
	if ev.AllDay {
		lines = append(lines, "Time: All Day")
	} else if ev.Start != "" || ev.End != "" {
		lines = append(lines, "Time: "+strings.TrimSpace(ev.Start+" - "+ev.End))
	}
	if ev.Location != "" {
		lines = append(lines, "Location: "+ev.Location)
	}
	if ev.Calendar != "" {
		lines = append(lines, "Calendar: "+ev.Calendar)
	}
	if ev.Status != "" {
		lines = append(lines, "Status: "+ev.Status)
	}
	if ev.Recurring {
		lines = append(lines, "Recurring: Yes")
	}
	if ev.Description != "" {
		lines = append(lines, "")
		lines = append(lines, ev.Description)
	}
	return strings.Join(lines, "\n")
}

func compactJSON(raw string) string {
	content := strings.TrimSpace(raw)
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start == -1 || end == -1 || end <= start {
		return ""
	}
	content = content[start : end+1]
	var decoded any
	if err := json.Unmarshal([]byte(content), &decoded); err != nil {
		return ""
	}
	b, err := json.Marshal(decoded)
	if err != nil {
		return ""
	}
	return string(b)
}
