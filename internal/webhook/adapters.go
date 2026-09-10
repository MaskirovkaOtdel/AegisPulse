package webhook

import (
	"encoding/json"
	"fmt"
)

type UniversalEvent struct {
	EventID     string            `json:"event_id"`
	EventType   string            `json:"event_type"` // "BURST_OVERAGE", "SECURITY_INCIDENT", "KILL_SWITCH", "CHAOS"
	APIKey      string            `json:"api_key"`
	Tier        string            `json:"tier"`
	Timestamp   int64             `json:"timestamp"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Channel     string            `json:"channel,omitempty"`
	Signature   string            `json:"signature,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// Discord Webhook structs (Rich Embed format)
type DiscordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type DiscordFooter struct {
	Text string `json:"text"`
}

type DiscordEmbed struct {
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Color       int                 `json:"color"`
	Fields      []DiscordEmbedField `json:"fields"`
	Footer      DiscordFooter       `json:"footer"`
}

type DiscordPayload struct {
	Username  string         `json:"username"`
	AvatarURL string         `json:"avatar_url,omitempty"`
	Embeds    []DiscordEmbed `json:"embeds"`
}

// Slack Webhook structs (Block Kit layout)
type SlackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type SlackBlock struct {
	Type   string      `json:"type"`
	Text   *SlackText  `json:"text,omitempty"`
	Fields []SlackText `json:"fields,omitempty"`
}

type SlackPayload struct {
	Channel string       `json:"channel,omitempty"`
	Blocks  []SlackBlock `json:"blocks"`
}

func AdaptGeneric(event *UniversalEvent) ([]byte, error) {
	return json.Marshal(event)
}

func AdaptDiscord(event *UniversalEvent) ([]byte, error) {
	color := 0x3498DB // Blue default
	if event.EventType == "SECURITY_INCIDENT" || event.EventType == "KILL_SWITCH" {
		color = 0xE74C3C // Red
	} else if event.EventType == "BURST_OVERAGE" {
		color = 0xF39C12 // Amber / Gold
	}

	fields := []DiscordEmbedField{
		{Name: "Event Type", Value: event.EventType, Inline: true},
		{Name: "Tier", Value: event.Tier, Inline: true},
		{Name: "API Key", Value: event.APIKey, Inline: true},
		{Name: "Timestamp", Value: fmt.Sprintf("%d", event.Timestamp), Inline: true},
	}

	if event.Channel != "" {
		fields = append(fields, DiscordEmbedField{Name: "Channel", Value: event.Channel, Inline: true})
	}
	if event.Signature != "" {
		fields = append(fields, DiscordEmbedField{Name: "Signature", Value: event.Signature, Inline: false})
	}

	for k, v := range event.Metadata {
		fields = append(fields, DiscordEmbedField{Name: k, Value: v, Inline: true})
	}

	payload := DiscordPayload{
		Username: "AegisPulse Security Gate",
		Embeds: []DiscordEmbed{
			{
				Title:       fmt.Sprintf("🚨 AegisPulse Alert: %s", event.Title),
				Description: event.Description,
				Color:       color,
				Fields:      fields,
				Footer:      DiscordFooter{Text: "AegisPulse Universal Webhook Engine"},
			},
		},
	}

	return json.Marshal(payload)
}

func AdaptSlack(event *UniversalEvent) ([]byte, error) {
	channel := event.Channel
	if channel == "" && (event.EventType == "SECURITY_INCIDENT" || event.EventType == "KILL_SWITCH") {
		channel = "#security-incidents"
	}

	fields := []SlackText{
		{Type: "mrkdwn", Text: fmt.Sprintf("*Event:*\n%s", event.EventType)},
		{Type: "mrkdwn", Text: fmt.Sprintf("*Tier:*\n%s", event.Tier)},
		{Type: "mrkdwn", Text: fmt.Sprintf("*API Key:*\n%s", event.APIKey)},
		{Type: "mrkdwn", Text: fmt.Sprintf("*Timestamp:*\n%d", event.Timestamp)},
	}

	if channel != "" {
		fields = append(fields, SlackText{Type: "mrkdwn", Text: fmt.Sprintf("*Channel:*\n%s", channel)})
	}
	if event.Signature != "" {
		fields = append(fields, SlackText{Type: "mrkdwn", Text: fmt.Sprintf("*Signature:*\n`%s`", event.Signature)})
	}

	for k, v := range event.Metadata {
		fields = append(fields, SlackText{Type: "mrkdwn", Text: fmt.Sprintf("*%s:*\n%s", k, v)})
	}

	payload := SlackPayload{
		Channel: channel,
		Blocks: []SlackBlock{
			{
				Type: "header",
				Text: &SlackText{Type: "plain_text", Text: fmt.Sprintf("🛡️ AegisPulse Alert: %s", event.Title)},
			},
			{
				Type: "section",
				Text: &SlackText{Type: "mrkdwn", Text: event.Description},
			},
			{
				Type:   "section",
				Fields: fields,
			},
		},
	}

	return json.Marshal(payload)
}
