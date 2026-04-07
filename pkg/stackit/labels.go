package stackit

import (
	"fmt"
	"strings"
)

const labelKeyClusterName = "kubernetes.io_cluster"

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

func (l labels) Selector() string {
	sb := strings.Builder{}
	for k, v := range l {
		// prevents trailing comma at the end
		if sb.Len() > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, "%s=%s", k, v)
	}
	return sb.String()
}

func (l labels) ToSDK() map[string]any {
	return map[string]any(l)
}
