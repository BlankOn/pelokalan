package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"pelokalan/internal/config"
)

const (
	mailtrapSendURL    = "https://send.api.mailtrap.io/api/send"
	mailtrapSandboxURL = "https://sandbox.api.mailtrap.io/api/send"
)

// Message is an outgoing email
type Message struct {
	To       string
	Subject  string
	Text     string
	HTML     string
	Category string
}

// Mailer sends emails
type Mailer interface {
	Send(ctx context.Context, msg *Message) error
}

// NewMailer returns a Mailtrap mailer, or a log-only mailer when no API token is configured
func NewMailer(cfg config.MailConfig) Mailer {
	if cfg.APIToken == "" {
		log.Println("Mailer: MAILTRAP_API_TOKEN not set, running in DEV MODE (emails will be printed to console)")
		return &LogMailer{}
	}

	url := mailtrapSendURL
	if cfg.InboxID != "" {
		url = mailtrapSandboxURL + "/" + cfg.InboxID
		log.Printf("Mailer: Mailtrap sandbox inbox %s", cfg.InboxID)
	} else {
		log.Printf("Mailer: Mailtrap sending API, from=%s <%s>", cfg.FromName, cfg.FromEmail)
	}

	return &MailtrapMailer{
		url:       url,
		token:     cfg.APIToken,
		fromEmail: cfg.FromEmail,
		fromName:  cfg.FromName,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

// LogMailer prints emails to the log instead of sending them (development)
type LogMailer struct{}

// Send logs the message
func (m *LogMailer) Send(ctx context.Context, msg *Message) error {
	log.Printf("[DEV MODE] email to %s\nSubject: %s\n%s", msg.To, msg.Subject, msg.Text)
	return nil
}

// MailtrapMailer sends emails through the Mailtrap API
type MailtrapMailer struct {
	url       string
	token     string
	fromEmail string
	fromName  string
	client    *http.Client
}

type mailtrapAddress struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type mailtrapPayload struct {
	From     mailtrapAddress   `json:"from"`
	To       []mailtrapAddress `json:"to"`
	Subject  string            `json:"subject"`
	Text     string            `json:"text,omitempty"`
	HTML     string            `json:"html,omitempty"`
	Category string            `json:"category,omitempty"`
}

// Send delivers the message via Mailtrap
func (m *MailtrapMailer) Send(ctx context.Context, msg *Message) error {
	body, err := json.Marshal(mailtrapPayload{
		From:     mailtrapAddress{Email: m.fromEmail, Name: m.fromName},
		To:       []mailtrapAddress{{Email: msg.To}},
		Subject:  msg.Subject,
		Text:     msg.Text,
		HTML:     msg.HTML,
		Category: msg.Category,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Api-Token", m.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("mailtrap API error: status %d, body: %s", resp.StatusCode, respBody)
	}
	return nil
}
