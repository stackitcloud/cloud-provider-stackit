package client

func LabelsFromTags(tags map[string]string) map[string]any {
	// Create a new map of type map[string]any
	l := make(map[string]any, len(tags))

	// Convert each value from string to any
	for key, value := range tags {
		l[key] = value
	}

	return l
}
