package pretranslate

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"pelokalan/pkg/ai"
	"pelokalan/pkg/netsafe"
)

// Claude (Anthropic) as the project's model, beside OpenAI-compatible APIs: AI settings with provider "anthropic"
// use this client, built on Anthropic's official Go SDK. It answers the same Chat/Ping calls as ai.Client, so
// pre-translation, single-entry AI, glossary extraction and the consistency check work unchanged.

// Providers of a project's AI settings
const (
	ProviderOpenAI    = "openai"    // OpenAI or any API compatible with its chat completions
	ProviderAnthropic = "anthropic" // Claude through the Anthropic API
)

// DefaultAnthropicBaseURL is the Anthropic API (the SDK adds /v1/...)
const DefaultAnthropicBaseURL = "https://api.anthropic.com"

// claudeMaxTokens bounds one answer (thinking included); a batch of 25 strings fits comfortably
const claudeMaxTokens = 16000

// claudeFallbackModels are the models that take the server-side refusal fallback ("fallbacks": "default")
var claudeFallbackModels = map[string]bool{
	"claude-opus-5-5": true, "claude-sonnet-5-5": true, "claude-opus-5": true,
	"claude-fable-5-2": true, "claude-fable-5-1": true,
}

type claudeClient struct {
	client anthropic.Client
	model  string
}

func newClaudeClient(baseURL, apiKey, model string, allowPrivate bool) *claudeClient {
	baseURL = strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(baseURL), "/"), "/v1")
	if baseURL == "" {
		baseURL = DefaultAnthropicBaseURL
	}
	return &claudeClient{
		client: anthropic.NewClient(
			option.WithAPIKey(apiKey),
			option.WithBaseURL(baseURL),
			// the same SSRF-safe transport as every other outgoing request of the server
			option.WithHTTPClient(&http.Client{Transport: netsafe.Transport(allowPrivate), Timeout: 10 * time.Minute}),
		),
		model: model,
	}
}

// Chat sends one system + user message and returns the text of the answer (the prompts ask for JSON)
func (c *claudeClient) Chat(ctx context.Context, system, user string) (string, ai.Usage, error) {
	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: claudeMaxTokens,
		System:    []anthropic.BetaTextBlockParam{{Text: system}},
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(user))},
		// translation and term extraction are routine work: low effort keeps thinking (and cost) small
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffortLow},
	}
	if claudeFallbackModels[c.model] { // a policy refusal is answered by a fallback model in the same call
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
		params.Fallbacks = anthropic.BetaFallbacksParamOfDefault()
	}
	resp, err := c.client.Beta.Messages.New(ctx, params)
	if err != nil {
		return "", ai.Usage{}, claudeError(err)
	}
	u := ai.Usage{
		PromptTokens:     resp.Usage.InputTokens + resp.Usage.CacheCreationInputTokens + resp.Usage.CacheReadInputTokens,
		CompletionTokens: resp.Usage.OutputTokens,
	}
	if resp.StopReason == anthropic.BetaStopReasonRefusal {
		return "", u, &ai.APIError{Status: http.StatusOK, Message: "the model declined this request"}
	}
	var b strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			b.WriteString(t.Text)
		}
	}
	if b.Len() == 0 {
		return "", u, errors.New("empty response")
	}
	return b.String(), u, nil
}

// Ping checks the key and that the model exists
func (c *claudeClient) Ping(ctx context.Context) error {
	_, err := c.client.Models.Get(ctx, c.model, anthropic.ModelGetParams{})
	return claudeError(err)
}

// claudeError turns SDK errors into the ones the settings page and jobs already explain
func claudeError(err error) error {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	if apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden {
		return ai.ErrUnauthorized
	}
	msg := apiErr.Error()
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return &ai.APIError{Status: apiErr.StatusCode, Message: msg}
}
