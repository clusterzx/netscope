package pluginhost

import (
	"netscope/internal/i18n"
	"netscope/internal/plugin"
)

// LocalizeNotification returns a copy of n with its texts in the language of the
// notifications (system setting). Event titles and messages were written in German when
// the events were raised; they are translated through the catalogs. Reports bring their
// own language (the report plugin renders them in it).
func LocalizeNotification(n *plugin.Notification, loc i18n.Locale) *plugin.Notification {
	out := *n
	out.Lang = loc
	if loc == i18n.DE {
		return &out
	}
	out.Title = i18n.T(loc, n.Title)
	out.Body = i18n.T(loc, n.Body)
	out.Events = make([]plugin.EventView, len(n.Events))
	for i, e := range n.Events {
		e.Label = i18n.T(loc, e.Label)
		e.Title = i18n.T(loc, e.Title)
		e.Message = i18n.T(loc, e.Message)
		out.Events[i] = e
	}
	return &out
}
