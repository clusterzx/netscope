package api

// Answers in the language of the request (see requestLocale). The data model stays German:
// catalogs (plugins, events, credentials, permissions) are translated field by field, and
// texts written in German at runtime – event titles, run errors, log lines, audit summaries –
// are translated when they are read, through the patterns of the i18n catalogs. A text
// without a translation is shown as it was stored.

import (
	"strings"

	"netscope/internal/audit"
	"netscope/internal/auth"
	"netscope/internal/bus"
	"netscope/internal/cron"
	"netscope/internal/events"
	"netscope/internal/federation"
	"netscope/internal/federation/wire"
	"netscope/internal/i18n"
	"netscope/internal/inventory"
	"netscope/internal/logging"
	"netscope/internal/plugin"
	"netscope/internal/pluginhost"
	"netscope/internal/plugins/cve"
	"netscope/internal/plugins/healthcheck"
	"netscope/internal/rules"
	"netscope/internal/tunnel"
)

func localizePluginView(v *pluginhost.PluginView, loc i18n.Locale) *pluginhost.PluginView {
	if v == nil || loc == i18n.DE {
		return v
	}
	out := *v
	out.Info = v.Info.Localize(loc)
	out.Schema = v.Schema.Localize(loc)
	out.Actions = plugin.LocalizeActions(v.Actions, loc)
	if v.Config != nil {
		c := *v.Config
		c.ScheduleText = describeSchedule(c.Schedule, c.ScheduleText, loc)
		out.Config = &c
	}
	out.Running = localizeRun(v.Running, loc)
	out.LastRun = localizeRun(v.LastRun, loc)
	return &out
}

func localizePluginViews(list []*pluginhost.PluginView, loc i18n.Locale) []*pluginhost.PluginView {
	if loc == i18n.DE {
		return list
	}
	out := make([]*pluginhost.PluginView, len(list))
	for i, v := range list {
		out[i] = localizePluginView(v, loc)
	}
	return out
}

func localizeRun(v *pluginhost.RunView, loc i18n.Locale) *pluginhost.RunView {
	if v == nil || loc == i18n.DE {
		return v
	}
	out := *v
	out.PluginName = i18n.T(loc, v.PluginName)
	out.Error = i18n.Err(loc, v.Error)
	out.Stats = localizeStats(v.Stats, loc)
	return &out
}

func localizeRuns(list []*pluginhost.RunView, loc i18n.Locale) []*pluginhost.RunView {
	if loc == i18n.DE {
		return list
	}
	out := make([]*pluginhost.RunView, len(list))
	for i, v := range list {
		out[i] = localizeRun(v, loc)
	}
	return out
}

// errorAttrs are log attributes that hold (German) error texts.
var errorAttrs = map[string]bool{"err": true, "error": true, "reason": true}

func localizeAttrs(attrs map[string]any, loc i18n.Locale) map[string]any {
	if attrs == nil || loc == i18n.DE {
		return attrs
	}
	out := make(map[string]any, len(attrs))
	for k, v := range attrs {
		if s, ok := v.(string); ok && errorAttrs[k] {
			v = i18n.Err(loc, s)
		}
		out[k] = v
	}
	return out
}

func localizeRunLogs(list []pluginhost.RunLog, loc i18n.Locale) []pluginhost.RunLog {
	if loc == i18n.DE {
		return list
	}
	out := make([]pluginhost.RunLog, len(list))
	for i, l := range list {
		l.Msg = i18n.T(loc, l.Msg)
		l.Attrs = localizeAttrs(l.Attrs, loc)
		out[i] = l
	}
	return out
}

func localizeLogEntries(list []logging.Entry, loc i18n.Locale) []logging.Entry {
	if loc == i18n.DE {
		return list
	}
	out := make([]logging.Entry, len(list))
	for i, e := range list {
		e.Msg = i18n.T(loc, e.Msg)
		e.Attrs = localizeAttrs(e.Attrs, loc)
		out[i] = e
	}
	return out
}

// localizeEvent translates the label (from the catalog) and title and message (written in
// German when the event was raised; patterns such as "Neues Gerät: %s" also cover events
// stored before the English catalog existed).
func localizeEvent(e events.Event, loc i18n.Locale) events.Event {
	if loc == i18n.DE {
		return e
	}
	e.Label = i18n.T(loc, e.Label)
	e.Title = i18n.T(loc, e.Title)
	e.Message = i18n.T(loc, e.Message)
	return e
}

func localizeEvents(list []events.Event, loc i18n.Locale) []events.Event {
	if loc == i18n.DE {
		return list
	}
	out := make([]events.Event, len(list))
	for i, e := range list {
		out[i] = localizeEvent(e, loc)
	}
	return out
}

func localizeNotifications(list []rules.NotificationView, loc i18n.Locale) []rules.NotificationView {
	if loc == i18n.DE {
		return list
	}
	out := make([]rules.NotificationView, len(list))
	for i, n := range list {
		n.Title = i18n.T(loc, n.Title)
		n.Error = i18n.Err(loc, n.Error)
		out[i] = n
	}
	return out
}

func localizeAudit(list []audit.Entry, loc i18n.Locale) []audit.Entry {
	if loc == i18n.DE {
		return list
	}
	out := make([]audit.Entry, len(list))
	for i, e := range list {
		e.Summary = i18n.T(loc, e.Summary)
		out[i] = e
	}
	return out
}

func localizePermissions(loc i18n.Locale) []auth.Permission {
	out := make([]auth.Permission, len(auth.Permissions))
	for i, p := range auth.Permissions {
		p.Group = i18n.T(loc, p.Group)
		p.Label = i18n.T(loc, p.Label)
		p.Hint = i18n.T(loc, p.Hint)
		out[i] = p
	}
	return out
}

func localizeQueryFields(loc i18n.Locale) []inventory.QueryField {
	list := inventory.QueryFields()
	for i, q := range list {
		q.Field = i18n.T(loc, q.Field)
		q.Description = i18n.T(loc, q.Description)
		list[i] = q
	}
	return list
}

// localizeMessage translates the texts of a live message (SSE) for one client.
func localizeMessage(msg bus.Message, loc i18n.Locale) bus.Message {
	if loc == i18n.DE {
		return msg
	}
	switch d := msg.Data.(type) {
	case events.Event:
		msg.Data = localizeEvent(d, loc)
	case logging.Entry:
		msg.Data = localizeLogEntries([]logging.Entry{d}, loc)[0]
	case map[string]any:
		out := make(map[string]any, len(d))
		for k, v := range d {
			switch x := v.(type) {
			case string:
				switch k {
				case "msg", "title", "message":
					v = i18n.T(loc, x)
				case "error", "err", "reason":
					v = i18n.Err(loc, x)
				}
			case *pluginhost.RunView:
				v = localizeRun(x, loc)
			case map[string]any:
				if k == "attrs" {
					v = localizeAttrs(x, loc)
				}
			}
			out[k] = v
		}
		msg.Data = out
	}
	return msg
}

// describeSchedule returns the plain-language description of a cron schedule.
func describeSchedule(expr, german string, loc i18n.Locale) string {
	if strings.TrimSpace(expr) == "" || loc == i18n.DE {
		return german
	}
	if text, err := describeCron(expr, loc); err == nil {
		return text
	}
	return german
}

// describeCron describes a cron expression in the language.
func describeCron(expr string, loc i18n.Locale) (string, error) {
	return cron.DescribeIn(expr, loc)
}

func localizeActionResult(o *pluginhost.ActionOutcome, loc i18n.Locale) *pluginhost.ActionOutcome {
	if o == nil || loc == i18n.DE {
		return o
	}
	out := *o
	out.Error = i18n.Err(loc, o.Error)
	if o.Result != nil {
		res := *o.Result
		res.Message = i18n.T(loc, res.Message)
		out.Result = &res
	}
	return &out
}

// localizeStats translates the message of an action result kept in the run statistics.
func localizeStats(stats map[string]any, loc i18n.Locale) map[string]any {
	res, ok := stats["result"].(map[string]any)
	if !ok {
		return stats
	}
	m, ok := res["message"].(string)
	if !ok || m == "" {
		return stats
	}
	out := make(map[string]any, len(stats))
	for k, v := range stats {
		out[k] = v
	}
	nr := make(map[string]any, len(res))
	for k, v := range res {
		nr[k] = v
	}
	nr["message"] = i18n.T(loc, m)
	out["result"] = nr
	return out
}

func localizeChecks(list []*healthcheck.Check, loc i18n.Locale) []*healthcheck.Check {
	if loc == i18n.DE {
		return list
	}
	out := make([]*healthcheck.Check, len(list))
	for i, c := range list {
		out[i] = localizeCheck(c, loc)
	}
	return out
}

func localizeCheck(c *healthcheck.Check, loc i18n.Locale) *healthcheck.Check {
	if c == nil || loc == i18n.DE {
		return c
	}
	cc := *c
	cc.LastError = i18n.Err(loc, c.LastError)
	return &cc
}

func localizeOutages(list []healthcheck.Outage, loc i18n.Locale) []healthcheck.Outage {
	if loc == i18n.DE {
		return list
	}
	out := make([]healthcheck.Outage, len(list))
	for i, o := range list {
		o.Reason = i18n.Err(loc, o.Reason)
		out[i] = o
	}
	return out
}

func localizeTimeline(list []inventory.TimelineEntry, loc i18n.Locale) []inventory.TimelineEntry {
	if loc == i18n.DE {
		return list
	}
	out := make([]inventory.TimelineEntry, len(list))
	for i, e := range list {
		e.Text = i18n.T(loc, e.Text)
		out[i] = e
	}
	return out
}

func localizePublishers(list []pluginhost.PublisherInfo, loc i18n.Locale) []pluginhost.PublisherInfo {
	if loc == i18n.DE {
		return list
	}
	out := make([]pluginhost.PublisherInfo, len(list))
	for i, p := range list {
		p.Name = i18n.T(loc, p.Name)
		out[i] = p
	}
	return out
}

// localizeSimResult translates the explanation of a rule test into the language of the
// request; the preview shows the notification as it would be sent, in the language of
// the notifications (system setting).
func localizeSimResult(res *rules.SimResult, loc, notify i18n.Locale) *rules.SimResult {
	if res == nil {
		return nil
	}
	out := *res
	if res.Preview != nil {
		out.Preview = pluginhost.LocalizeNotification(res.Preview, notify)
		out.PreviewText = out.Preview.PlainText()
	}
	if loc == i18n.DE {
		return &out
	}
	out.Note = i18n.T(loc, res.Note)
	out.Conditions = make([]rules.CondResult, len(res.Conditions))
	for i, c := range res.Conditions {
		c.Condition = i18n.T(loc, c.Condition)
		c.Detail = i18n.Err(loc, c.Detail)
		out.Conditions[i] = c
	}
	out.Actions = make([]rules.ActionPlan, len(res.Actions))
	for i, a := range res.Actions {
		a.PublisherName = i18n.T(loc, a.PublisherName)
		a.Skipped = i18n.T(loc, a.Skipped)
		out.Actions[i] = a
	}
	if res.Delivery != nil {
		out.Delivery = make(map[string]string, len(res.Delivery))
		for k, v := range res.Delivery {
			out.Delivery[k] = i18n.Err(loc, v)
		}
	}
	return &out
}

func localizeCVEStatus(st *cve.Status, loc i18n.Locale) *cve.Status {
	if st == nil || loc == i18n.DE {
		return st
	}
	out := *st
	out.SyncError = i18n.Err(loc, st.SyncError)
	out.Disclaimer = i18n.T(loc, st.Disclaimer)
	out.Feeds = make([]cve.FeedStatus, len(st.Feeds))
	for i, f := range st.Feeds {
		f.Error = i18n.Err(loc, f.Error)
		out.Feeds[i] = f
	}
	if st.LastMatch != nil {
		m := *st.LastMatch
		m.Error = i18n.Err(loc, m.Error)
		out.LastMatch = &m
	}
	return &out
}

func localizeStrings(list []string, loc i18n.Locale) []string {
	if loc == i18n.DE || list == nil {
		return list
	}
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = i18n.Err(loc, s)
	}
	return out
}

func localizeSteps(list []auth.TestStep, loc i18n.Locale) []auth.TestStep {
	if loc == i18n.DE || list == nil {
		return list
	}
	out := make([]auth.TestStep, len(list))
	for i, st := range list {
		st.Name = i18n.T(loc, st.Name)
		st.Error = i18n.Err(loc, st.Error)
		out[i] = st
	}
	return out
}

func localizeTunnelStatuses(list []tunnel.Status, loc i18n.Locale) []tunnel.Status {
	if loc == i18n.DE {
		return list
	}
	out := make([]tunnel.Status, len(list))
	for i, st := range list {
		st.Error = i18n.Err(loc, st.Error)
		st.Warnings = localizeStrings(st.Warnings, loc)
		out[i] = st
	}
	return out
}

// localizeSite translates what a site reported about its plugins (German texts of the site).
func localizeSite(st federation.Site, loc i18n.Locale) federation.Site {
	if loc == i18n.DE || st.Status == nil {
		return st
	}
	ws := *st.Status
	ws.Plugins = make([]wire.PluginStatus, len(st.Status.Plugins))
	for i, p := range st.Status.Plugins {
		p.Name = i18n.T(loc, p.Name)
		p.Error = i18n.Err(loc, p.Error)
		ws.Plugins[i] = p
	}
	st.Status = &ws
	return st
}

func localizeSites(list []federation.Site, loc i18n.Locale) []federation.Site {
	if loc == i18n.DE {
		return list
	}
	out := make([]federation.Site, len(list))
	for i, st := range list {
		out[i] = localizeSite(st, loc)
	}
	return out
}
