// Package notify sends short status messages to a Discord channel through
// an incoming webhook.
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// discordMessageLimit is Discord's hard cap on a webhook message's content field.
const discordMessageLimit = 2000

// Discord posts to a Discord incoming webhook URL.
type Discord struct {
	WebhookURL string
	client     *http.Client
}

func NewDiscord(webhookURL string) *Discord {
	return &Discord{WebhookURL: webhookURL, client: &http.Client{Timeout: 10 * time.Second}}
}

// Send posts text as the webhook's message content, trimming it to fit
// Discord's length cap rather than failing outright.
func (d *Discord) Send(text string) error {
	if d.WebhookURL == "" {
		return fmt.Errorf("notify: no webhook URL configured")
	}
	if len(text) > discordMessageLimit {
		text = text[:discordMessageLimit-15] + "\n[truncated]"
	}

	payload, err := json.Marshal(struct {
		Content string `json:"content"`
	}{Content: text})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, d.WebhookURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("notify: posting to discord: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("notify: discord rejected the message with %d: %s", resp.StatusCode, detail)
	}
	return nil
}
