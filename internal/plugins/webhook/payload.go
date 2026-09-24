package webhook

import (
	"bytes"
	"encoding/json"
	"time"

	"netscope/internal/plugin"
)

// PayloadVersion is the version of the NetScope notification payload. It is increased
// only for incompatible changes; new fields may be added within a version.
const PayloadVersion = 1

// Payload is the NetScope notification payload v1 sent by the webhook and n8n
// publishers (documented in docs/PUBLISHERS.md). All keys are always present so that
// receivers can rely on a stable structure; missing values are "", 0, false, [] or null.
type Payload struct {
	Version      int                 `json:"version"`
	Source       string              `json:"source"`
	SentAt       string              `json:"sentAt"` // RFC 3339, UTC
	Notification PayloadNotification `json:"notification"`
	Summary      PayloadSummary      `json:"summary"`
	Events       []PayloadEvent      `json:"events"`
	Text         string              `json:"text"` // plain-text rendering of the whole notification
	Body         string              `json:"body"` // Markdown body of reports and test messages
}

// PayloadNotification describes the notification (one rule action, bundled events).
type PayloadNotification struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`     // event | escalation | test | report
	Priority  string `json:"priority"` // low | normal | high | urgent
	Title     string `json:"title"`
	RuleID    int64  `json:"ruleId"`
	RuleName  string `json:"ruleName"`
	Link      string `json:"link"`      // deep link into the UI ("" without public URL)
	CreatedAt string `json:"createdAt"` // RFC 3339, UTC
}

// PayloadSummary aggregates the bundled events.
type PayloadSummary struct {
	Events          int            `json:"events"`
	MaxSeverity     string         `json:"maxSeverity"`     // info if there are no events
	MaxSeverityRank int            `json:"maxSeverityRank"` // 0 (info) … 4 (critical)
	BySeverity      map[string]int `json:"bySeverity"`      // all five severities, counts may be 0
}

// PayloadEvent is one bundled event.
type PayloadEvent struct {
	ID            int64          `json:"id"`
	Type          string         `json:"type"`     // catalog type, e.g. port.opened
	Label         string         `json:"label"`    // German label of the type
	Category      string         `json:"category"` // device | port | cert | …
	Severity      string         `json:"severity"` // info | low | medium | high | critical
	SeverityRank  int            `json:"severityRank"`
	SeverityLabel string         `json:"severityLabel"` // German label
	Title         string         `json:"title"`
	Message       string         `json:"message"`
	At            string         `json:"at"` // RFC 3339, UTC
	Acknowledged  bool           `json:"acknowledged"`
	Escalated     bool           `json:"escalated"`
	Link          string         `json:"link"`
	Device        *PayloadDevice `json:"device"`  // null for events without device
	Payload       map[string]any `json:"payload"` // type-specific fields, see the event catalog
	// Site is the NetScope site that raised the event (only on a central instance).
	Site string `json:"site,omitempty"`
}

// PayloadDevice is the device an event refers to.
type PayloadDevice struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	IP   string `json:"ip"`
	MAC  string `json:"mac"`
	Link string `json:"link"`
}

// BuildPayload converts a notification into payload v1. sentAt is the delivery time
// (the webhook publisher uses the same instant for the signature timestamp).
func BuildPayload(n *plugin.Notification, sentAt time.Time) *Payload {
	p := &Payload{
		Version: PayloadVersion,
		Source:  "netscope",
		SentAt:  formatTime(sentAt),
		Notification: PayloadNotification{
			ID:        n.ID,
			Kind:      n.Kind,
			Priority:  string(n.Priority),
			Title:     n.Title,
			RuleID:    n.RuleID,
			RuleName:  n.RuleName,
			Link:      n.Link,
			CreatedAt: formatTime(n.CreatedAt),
		},
		Events: make([]PayloadEvent, 0, len(n.Events)),
		Text:   n.PlainText(),
		Body:   n.Body,
	}
	maxSev := n.MaxSeverity()
	p.Summary = PayloadSummary{
		Events:          len(n.Events),
		MaxSeverity:     string(maxSev),
		MaxSeverityRank: max(maxSev.Rank(), 0),
		BySeverity:      make(map[string]int, len(plugin.Severities)),
	}
	for _, s := range plugin.Severities {
		p.Summary.BySeverity[string(s)] = 0
	}
	for _, e := range n.Events {
		p.Summary.BySeverity[string(e.Severity)]++
		pe := PayloadEvent{
			ID:            e.ID,
			Type:          e.Type,
			Label:         e.Label,
			Category:      e.Category,
			Severity:      string(e.Severity),
			SeverityRank:  max(e.Severity.Rank(), 0),
			SeverityLabel: e.Severity.Label(),
			Title:         e.Title,
			Message:       e.Message,
			At:            formatTime(e.At),
			Acknowledged:  e.Acknowledged,
			Escalated:     e.Escalated,
			Link:          e.Link,
			Payload:       e.Payload,
			Site:          e.Site,
		}
		if pe.Payload == nil {
			pe.Payload = map[string]any{}
		}
		if e.DeviceID != 0 || e.DeviceName != "" || e.DeviceIP != "" || e.DeviceMAC != "" {
			pe.Device = &PayloadDevice{
				ID:   e.DeviceID,
				Name: e.DeviceName,
				IP:   e.DeviceIP,
				MAC:  e.DeviceMAC,
				Link: e.DeviceLink,
			}
		}
		p.Events = append(p.Events, pe)
	}
	return p
}

// EncodePayload serializes a payload as compact JSON without HTML escaping (links keep
// their "&"). The result is exactly the request body that gets signed.
func EncodePayload(p *Payload) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
