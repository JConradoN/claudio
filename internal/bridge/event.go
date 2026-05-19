package bridge

// EventContent returns the text content of an event, preferring the Text field
// over Content. This matches how PI SDK populates event payloads across
// different event types — assistant events put text in Text, result events
// may put the final answer in either field.
func EventContent(ev Event) string {
	if ev.Text != "" {
		return ev.Text
	}
	return ev.Content
}
