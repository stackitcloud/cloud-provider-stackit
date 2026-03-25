package stackit

type labels map[string]any

func labelsFromTags(tags map[string]string) labels {
	// Create a new map of type map[string]any
	l := make(map[string]any, len(tags))

	// Convert each value from string to any
	for key, value := range tags {
		l[key] = value
	}

	return labels(l)
}
