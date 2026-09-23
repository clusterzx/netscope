package plugin

import (
	"fmt"
	"strings"
	"time"
)

// Priority of a notification.
type Priority string

const (
	PrioLow    Priority = "low"
	PrioNormal Priority = "normal"
	PrioHigh   Priority = "high"
	PrioUrgent Priority = "urgent"
)

// Priorities lists all priorities in ascending order.
var Priorities = []Priority{PrioLow, PrioNormal, PrioHigh, PrioUrgent}

// Valid reports whether p is known.
func (p Priority) Valid() bool {
	for _, v := range Priorities {
		if v == p {
			return true
		}
	}
	return false
}

// Notification kinds.
const (
	NotifyEvent      = "event"
	NotifyEscalation = "escalation"
	NotifyTest       = "test"
	NotifyReport     = "report"
)

// EventView is an event prepared for publishers.
type EventView struct {
	ID           int64          `json:"id"`
	Type         string         `json:"type"`
	Label        string         `json:"label"`
	Category     string         `json:"category"`
	Severity     Severity       `json:"severity"`
	Title        string         `json:"title"`
	Message      string         `json:"message,omitempty"`
	At           time.Time      `json:"at"`
	DeviceID     int64          `json:"deviceId,omitempty"`
	DeviceName   string         `json:"deviceName,omitempty"`
	DeviceIP     string         `json:"deviceIp,omitempty"`
	DeviceMAC    string         `json:"deviceMac,omitempty"`
	Link         string         `json:"link,omitempty"`
	DeviceLink   string         `json:"deviceLink,omitempty"`
	Payload      map[string]any `json:"payload,omitempty"`
	Escalated    bool           `json:"escalated,omitempty"`
	Acknowledged bool           `json:"acknowledged,omitempty"`
}

// Notification is what a publisher delivers. Events are bundled per rule action.
type Notification struct {
	ID        int64       `json:"id"`
	Kind      string      `json:"kind"` // event | escalation | test | report
	Priority  Priority    `json:"priority"`
	Title     string      `json:"title"`
	Body      string      `json:"body,omitempty"` // Markdown body for reports and tests
	RuleID    int64       `json:"ruleId,omitempty"`
	RuleName  string      `json:"ruleName,omitempty"`
	Events    []EventView `json:"events,omitempty"`
	Link      string      `json:"link,omitempty"` // deep link into the UI
	CreatedAt time.Time   `json:"createdAt"`
}

// PlainText renders the notification as plain text (used by email, ntfy, logs).
func (n *Notification) PlainText() string {
	var b strings.Builder
	b.WriteString(n.Title)
	b.WriteString("\n")
	if n.Body != "" {
		b.WriteString("\n")
		b.WriteString(n.Body)
		b.WriteString("\n")
	}
	for _, e := range n.Events {
		fmt.Fprintf(&b, "\n[%s] %s", e.Severity.Label(), e.Title)
		if e.DeviceName != "" && !strings.Contains(e.Title, e.DeviceName) {
			fmt.Fprintf(&b, " – %s", e.DeviceName)
		}
		if e.DeviceIP != "" {
			fmt.Fprintf(&b, " (%s)", e.DeviceIP)
		}
		if e.Message != "" {
			fmt.Fprintf(&b, "\n  %s", e.Message)
		}
		if e.Link != "" {
			fmt.Fprintf(&b, "\n  %s", e.Link)
		}
		b.WriteString("\n")
	}
	if n.Link != "" {
		fmt.Fprintf(&b, "\n%s\n", n.Link)
	}
	return b.String()
}

// MaxSeverity returns the highest severity of the bundled events (info if none).
func (n *Notification) MaxSeverity() Severity {
	best := SevInfo
	for _, e := range n.Events {
		if e.Severity.Rank() > best.Rank() {
			best = e.Severity
		}
	}
	return best
}
