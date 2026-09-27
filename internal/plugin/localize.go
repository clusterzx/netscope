package plugin

import "netscope/internal/i18n"

// Localized copies of the plugin catalogs for the API. The definitions stay German (the
// source language); translations come from the i18n catalogs.

// Localize returns the info with name and description in the language.
func (i Info) Localize(loc i18n.Locale) Info {
	i.Name = i18n.T(loc, i.Name)
	i.Description = i18n.T(loc, i.Description)
	return i
}

// Localize returns a copy of the schema with labels, descriptions, groups, placeholders
// and option labels in the language.
func (s Schema) Localize(loc i18n.Locale) Schema {
	if loc == i18n.DE || s.Fields == nil {
		return s
	}
	return Schema{Fields: LocalizeFields(s.Fields, loc)}
}

// LocalizeFields translates settings fields (also action parameters).
func LocalizeFields(fields []Field, loc i18n.Locale) []Field {
	if loc == i18n.DE || fields == nil {
		return fields
	}
	out := make([]Field, len(fields))
	for i, f := range fields {
		f.Label = i18n.T(loc, f.Label)
		f.Description = i18n.T(loc, f.Description)
		f.Group = i18n.T(loc, f.Group)
		f.Placeholder = i18n.T(loc, f.Placeholder)
		if f.Options != nil {
			opts := make([]Option, len(f.Options))
			for j, o := range f.Options {
				opts[j] = Option{Value: o.Value, Label: i18n.T(loc, o.Label)}
			}
			f.Options = opts
		}
		out[i] = f
	}
	return out
}

// Localize returns the action with its texts in the language.
func (a Action) Localize(loc i18n.Locale) Action {
	a.Label = i18n.T(loc, a.Label)
	a.Description = i18n.T(loc, a.Description)
	a.Confirm = i18n.T(loc, a.Confirm)
	a.Params = LocalizeFields(a.Params, loc)
	return a
}

// LocalizeActions translates a list of actions.
func LocalizeActions(actions []Action, loc i18n.Locale) []Action {
	if loc == i18n.DE || actions == nil {
		return actions
	}
	out := make([]Action, len(actions))
	for i, a := range actions {
		out[i] = a.Localize(loc)
	}
	return out
}

// Localize returns the event type with label, description and payload descriptions in
// the language.
func (e EventSpec) Localize(loc i18n.Locale) EventSpec {
	if loc == i18n.DE {
		return e
	}
	e.Label = i18n.T(loc, e.Label)
	e.Description = i18n.T(loc, e.Description)
	payload := make([]PayloadField, len(e.Payload))
	for i, f := range e.Payload {
		f.Description = i18n.T(loc, f.Description)
		payload[i] = f
	}
	e.Payload = payload
	return e
}

// LocalizedCatalog returns the event type catalog in the language.
func LocalizedCatalog(loc i18n.Locale) []EventSpec {
	list := Catalog()
	for i, e := range list {
		list[i] = e.Localize(loc)
	}
	return list
}

// EventLabel returns the label of an event type in the language (the type if unknown).
func EventLabel(typ string, loc i18n.Locale) string {
	if spec, ok := LookupEvent(typ); ok {
		return i18n.T(loc, spec.Label)
	}
	return typ
}

// Localize returns the credential type with its texts in the language.
func (c CredentialType) Localize(loc i18n.Locale) CredentialType {
	c.Label = i18n.T(loc, c.Label)
	c.Description = i18n.T(loc, c.Description)
	c.Schema = c.Schema.Localize(loc)
	return c
}

// LocalizedCredentialTypes returns all credential types in the language.
func LocalizedCredentialTypes(loc i18n.Locale) []CredentialType {
	list := CredentialTypes()
	for i, c := range list {
		list[i] = c.Localize(loc)
	}
	return list
}

// LocalizeErrors translates the messages of field errors.
func LocalizeErrors(errs []FieldError, loc i18n.Locale) []FieldError {
	if loc == i18n.DE || errs == nil {
		return errs
	}
	out := make([]FieldError, len(errs))
	for i, e := range errs {
		out[i] = FieldError{Field: e.Field, Message: i18n.Err(loc, e.Message)}
	}
	return out
}
