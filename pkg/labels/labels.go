package labels

import (
	"regexp"
	"strings"
)

// Replace non-alphanumeric characters (except '-', '_', '.') with '-'
var reg = regexp.MustCompile(`[^-a-zA-Z0-9_.]+`)

func Sanitize(input string) string {
	sanitized := reg.ReplaceAllString(input, "-")

	// Ensure the label starts and ends with an alphanumeric character
	sanitized = strings.Trim(sanitized, "-_.")

	// Ensure the label is not longer than 63 characters
	if len(sanitized) > 63 {
		sanitized = sanitized[:63]
	}

	return sanitized
}
