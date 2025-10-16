package stackit

type labels map[string]interface{}

func labelsFromTags(tags map[string]string) labels {
	// Create a new map of type map[string]interface{}
	l := make(map[string]interface{}, len(tags))

	// Convert each value from string to interface{}
	for key, value := range tags {
		l[key] = value
	}

	return labels(l)
}
