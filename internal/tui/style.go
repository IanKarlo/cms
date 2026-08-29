package tui

func renderHeading(value string, noColor bool) string {
	if noColor {
		return value
	}
	return heading.Render(value)
}
