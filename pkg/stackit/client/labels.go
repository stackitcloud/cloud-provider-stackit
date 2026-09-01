package client

import (
	"fmt"
	"strings"
)

type Labels map[string]string

func (l Labels) ToSDK() map[string]any {
	sdkLabels := make(map[string]any, len(l))
	for k, v := range l {
		sdkLabels[k] = v
	}
	return sdkLabels
}

func (l Labels) Selector() string {
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
