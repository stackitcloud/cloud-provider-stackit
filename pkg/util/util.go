package util

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	BYTE = 1.0 << (10 * iota)
	KIBIBYTE
	MEBIBYTE
	GIBIBYTE
	TEBIBYTE
)

// MyDuration is the encoding.TextUnmarshaler interface for time.Duration
type MyDuration struct {
	time.Duration
}

// UnmarshalText is used to convert from text to Duration
func (d *MyDuration) UnmarshalText(text []byte) error {
	res, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	d.Duration = res
	return nil
}

func SanitizeLabel(input string) string {
	// Replace non-alphanumeric characters (except '-', '_', '.') with '-'
	reg := regexp.MustCompile(`[^-a-zA-Z0-9_.]+`)
	sanitized := reg.ReplaceAllString(input, "-")

	// Ensure the label starts and ends with an alphanumeric character
	sanitized = strings.Trim(sanitized, "-_.")

	// Ensure the label is not longer than 63 characters
	if len(sanitized) > 63 {
		sanitized = sanitized[:63]
	}

	return sanitized
}

func ConvertMapStringToInterface(input map[string]string) map[string]interface{} {
	// Create a new map of type map[string]interface{}
	result := make(map[string]interface{})

	// Convert each value from string to interface{}
	for key, value := range input {
		result[key] = value
	}

	return result
}

// RoundUpSize calculates how many allocation units are needed to accommodate
// a volume of given size. E.g. when user wants 1500MiB volume, while AWS EBS
// allocates volumes in gibibyte-sized chunks,
// RoundUpSize(1500 * 1024*1024, 1024*1024*1024) returns '2'
// (2 GiB is the smallest allocatable volume that can hold 1500MiB)
func RoundUpSize(volumeSizeBytes, allocationUnitBytes int64) int64 {
	roundedUp := volumeSizeBytes / allocationUnitBytes
	if volumeSizeBytes%allocationUnitBytes > 0 {
		roundedUp++
	}
	return roundedUp
}

// SetMapIfNotEmpty sets the value of the key in the provided map if the value
// is not empty (i.e., it is not the zero value for that type) and returns a
// pointer to the new map. If the map is nil, it will be initialized with a new
// map.
func SetMapIfNotEmpty[K comparable, V comparable](m map[K]V, key K, value V) map[K]V {
	// Check if the value is the zero value for its type
	var zeroValue V
	if value == zeroValue {
		return m
	}

	// Initialize the map if it's nil
	if m == nil {
		m = make(map[K]V)
	}

	// Set the value in the map
	m[key] = value

	return m
}

func MapToString(m map[string]interface{}) string {
	var result string
	for key, value := range m {
		// Convert the value to string
		strValue := fmt.Sprintf("%v", value)
		// Concatenate the key-value pair, separating by a comma
		result += key + "=" + strValue + ","
	}

	// Remove the trailing comma
	if result == "" {
		result = result[:len(result)-1]
	}

	return result
}
