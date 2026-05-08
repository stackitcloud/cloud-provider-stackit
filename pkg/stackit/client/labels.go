package client

type Labels map[string]any

func LabelsFromTags(tags map[string]string) Labels {
	// Create a new map of type map[string]any
	l := make(map[string]any, len(tags))

	// Convert each value from string to any
	for key, value := range tags {
		l[key] = value
	}

	return Labels(l)
}
