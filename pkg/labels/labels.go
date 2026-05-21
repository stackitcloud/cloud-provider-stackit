package labels

import (
	"regexp"
	"strings"
)

var (
	// 1-63 characters
	// must begin and end with an alphanumerical character [a-z0-9A-Z]
	// may contain dashes (-), underscores (_), dots (.), and alphanumerics between
	labelKeyRegex = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9._-]{1,61}[a-zA-Z0-9])?$`)

	// 0-63 characters, allow empty string
	// must begin and end with an alphanumerical character [a-z0-9A-Z]
	// may contain dashes (-), underscores (_), dots (.), and alphanumerics between
	labelValueRegex = regexp.MustCompile(`^$|^[a-zA-Z0-9](?:[a-zA-Z0-9._-]{0,61}[a-zA-Z0-9])?$`)

	// Replace non-alphanumeric characters (except '-', '_', '.') with '-'
	reg = regexp.MustCompile(`[^-a-zA-Z0-9_.]+`)
)

func IsValidLabelKey(key string) bool {
	return labelKeyRegex.MatchString(key)
}

func IsValidLabelValue(value string) bool {
	return labelValueRegex.MatchString(value)
}

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
