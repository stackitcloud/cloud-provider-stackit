package client

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

type LabelMap map[string]string

func (l LabelMap) ToSDK() map[string]any {
	sdkLabels := make(map[string]any, len(l))
	for k, v := range l {
		sdkLabels[k] = v
	}
	return sdkLabels
}

func (l LabelMap) Selector() string {
	sb := strings.Builder{}
	for _, key := range l.sortedKeys() {
		val := l[key]
		// prevents trailing comma at the end
		if sb.Len() > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, "%s=%s", key, val)
	}
	return sb.String()
}

// sortedKeys returns a sorted list of all label keys. This is used in [LabelMap.Selector] to ensure the selector query is deterministic.
func (l LabelMap) sortedKeys() []string {
	return slices.Sorted(maps.Keys(l))
}
