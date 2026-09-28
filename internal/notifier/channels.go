package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/elqsar/pganalyzer/internal/config"
)

// Channel delivers messages to one destination.
type Channel interface {
	// Name identifies the channel in logs without revealing its URL, which for
	// Slack and most webhooks is itself the credential.
	Name() string
	Send(ctx context.Context, m Message) error
}

const sendTimeout = 10 * time.Second

// NewChannels builds channels from configuration.
func NewChannels(cfgs []config.ChannelConfig, client *http.Client) ([]Channel, error) {
	if client == nil {
		client = &http.Client{Timeout: sendTimeout}
	}
	channels := make([]Channel, 0, len(cfgs))
	for i, c := range cfgs {
		name := fmt.Sprintf("%s#%d", c.Type, i)
		if u, err := url.Parse(c.URL); err == nil {
			name = fmt.Sprintf("%s#%d(%s)", c.Type, i, u.Host)
		}
		switch c.Type {
		case config.ChannelSlack:
			channels = append(channels, &slackChannel{name: name, url: c.URL, client: client})
		case config.ChannelWebhook:
			channels = append(channels, &webhookChannel{name: name, url: c.URL, headers: c.Headers, client: client})
		default:
			return nil, fmt.Errorf("unknown channel type %q", c.Type)
		}
	}
	return channels, nil
}

// slackChannel posts to a Slack incoming webhook. Discord and Mattermost accept
// the same payload on their Slack-compatible endpoints.
type slackChannel struct {
	name   string
	url    string
	client *http.Client
}

func (c *slackChannel) Name() string { return c.name }

func (c *slackChannel) Send(ctx context.Context, m Message) error {
	body, err := json.Marshal(map[string]any{
		"text":         render(m, slackMrkdwn),
		"unfurl_links": false,
	})
	if err != nil {
		return err
	}
	return post(ctx, c.client, c.url, nil, body)
}

// webhookChannel posts the message as JSON.
type webhookChannel struct {
	name    string
	url     string
	headers map[string]string
	client  *http.Client
}

func (c *webhookChannel) Name() string { return c.name }

func (c *webhookChannel) Send(ctx context.Context, m Message) error {
	m.Text = render(m, markdown)
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return post(ctx, c.client, c.url, c.headers, body)
}

func post(ctx context.Context, client *http.Client, target string, headers map[string]string, body []byte) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return errors.New("building request: invalid URL")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "pganalyzer")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		// url.Error embeds the full URL; keep only the underlying cause.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return fmt.Errorf("sending: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
