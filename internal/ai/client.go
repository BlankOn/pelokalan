package ai

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

	"pelokalan/internal/netsafe"
)

// DefaultBaseURL is the OpenAI API endpoint
const DefaultBaseURL = "https://api.openai.com/v1"

// Usage reports tokens consumed by a request
type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
}

// APIError is an error returned by the API
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("API error %d: %s", e.Status, e.Message) }

// ErrUnauthorized means the API key was rejected
var ErrUnauthorized = errors.New("API key rejected")

// Client talks to an OpenAI-compatible chat completions API
type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
	retries int
	backoff time.Duration
}

// NewClient creates a client. Unless allowPrivate is set, the base URL may
// not point to a private network.
func NewClient(baseURL, apiKey, model string, allowPrivate bool) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Transport: netsafe.Transport(allowPrivate), Timeout: 5 * time.Minute},
		retries: 3,
		backoff: 2 * time.Second,
	}
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.backoff * time.Duration(1<<(attempt-1))):
			}
		}
		var rd io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				return err
			}
			rd = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rd)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			if errors.Is(err, netsafe.ErrBlockedAddress) || ctx.Err() != nil {
				return err
			}
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return ErrUnauthorized
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = &APIError{Status: resp.StatusCode, Message: apiMessage(data)}
			continue
		case resp.StatusCode >= 400:
			return &APIError{Status: resp.StatusCode, Message: apiMessage(data)}
		}
		return json.Unmarshal(data, out)
	}
	return lastErr
}

func apiMessage(data []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	s := strings.TrimSpace(string(data))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// Ping verifies the API key and that the model exists
func (c *Client) Ping(ctx context.Context) error {
	var out struct {
		ID string `json:"id"`
	}
	return c.do(ctx, http.MethodGet, "/models/"+c.model, nil, &out)
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	Temperature    *float64      `json:"temperature,omitempty"`
	ResponseFormat struct {
		Type string `json:"type"`
	} `json:"response_format"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
}

// chatTemperature is the sampling temperature for translations
const chatTemperature = 0.2

// Chat sends a system and user message and returns the JSON reply
func (c *Client) Chat(ctx context.Context, system, user string) (string, Usage, error) {
	req := chatRequest{
		Model:    c.model,
		Messages: []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}},
	}
	req.ResponseFormat.Type = "json_object"
	// a low temperature keeps translations consistent; some models only accept their
	// default, so a rejected temperature is retried without it
	t := chatTemperature
	req.Temperature = &t
	var resp chatResponse
	err := c.do(ctx, http.MethodPost, "/chat/completions", req, &resp)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "temperature") {
		req.Temperature = nil
		resp = chatResponse{}
		err = c.do(ctx, http.MethodPost, "/chat/completions", req, &resp)
	}
	if err != nil {
		return "", Usage{}, err
	}
	if len(resp.Choices) == 0 {
		return "", resp.Usage, errors.New("empty response")
	}
	return resp.Choices[0].Message.Content, resp.Usage, nil
}
