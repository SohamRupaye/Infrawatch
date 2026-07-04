package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/engine/config"
)

// slackPayload is the Slack Incoming Webhook JSON body.
type slackPayload struct {
	Text        string            `json:"text,omitempty"`
	Channel     string            `json:"channel,omitempty"`
	Username    string            `json:"username,omitempty"`
	IconEmoji   string            `json:"icon_emoji,omitempty"`
	Attachments []slackAttachment `json:"attachments,omitempty"`
}

type slackAttachment struct {
	Color  string       `json:"color"`
	Title  string       `json:"title"`
	Text   string       `json:"text"`
	Fields []slackField `json:"fields"`
	Footer string       `json:"footer"`
	Ts     int64        `json:"ts"`
}

type slackField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

// SlackChannel sends alerts to a Slack channel via an Incoming Webhook.
type SlackChannel struct {
	cfg    config.SlackConfig
	client *http.Client
}

// NewSlackChannel creates a SlackChannel.
func NewSlackChannel(cfg config.SlackConfig) *SlackChannel {
	return &SlackChannel{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Name implements Channel.
func (s *SlackChannel) Name() string { return "slack" }

// Send implements Channel.
func (s *SlackChannel) Send(ctx context.Context, alert Alert) error {
	color := stateColor(alert.State)

	payload := slackPayload{
		Username:  "Infrawatch",
		IconEmoji: ":satellite_antenna:",
		Channel:   s.cfg.Channel,
		Attachments: []slackAttachment{
			{
				Color: color,
				Title: fmt.Sprintf("[%s] %s", alert.State, alert.ServiceName),
				Text:  alert.Message,
				Fields: []slackField{
					{Title: "Service", Value: alert.ServiceName, Short: true},
					{Title: "State", Value: alert.State, Short: true},
					{Title: "Previous State", Value: alert.PreviousState, Short: true},
					{Title: "Response Time", Value: fmt.Sprintf("%dms", alert.ResponseTimeMs), Short: true},
				},
				Footer: "Infrawatch",
				Ts:     alert.Timestamp.Unix(),
			},
		},
	}

	if alert.DashboardURL != "" {
		payload.Attachments[0].Fields = append(payload.Attachments[0].Fields,
			slackField{Title: "Dashboard", Value: alert.DashboardURL, Short: false},
		)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal slack payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("send slack: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack returned status %d", resp.StatusCode)
	}
	return nil
}

func stateColor(state string) string {
	switch state {
	case "HEALTHY":
		return "good" // green
	case "DEGRADED":
		return "warning" // yellow
	case "UNHEALTHY", "DEAD":
		return "danger" // red
	case "RECOVERING":
		return "#439FE0" // blue
	}
	return "#aaaaaa" // grey
}
