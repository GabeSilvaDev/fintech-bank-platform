package env

import (
	"os"
	"strconv"
	"strings"
	"time"
)

func Get(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func GetInt(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func GetIntMin(key string, defaultValue, min int) int {
	value := GetInt(key, defaultValue)
	if value < min {
		return defaultValue
	}
	return value
}

func GetUint32(key string, defaultValue uint32) uint32 {
	value := GetInt(key, int(defaultValue))
	if value < 1 {
		return defaultValue
	}
	return uint32(value)
}

func GetBool(key string, defaultValue bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func GetDuration(key string, defaultValue time.Duration) time.Duration {
	if value, exists := os.LookupEnv(key); exists {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func GetDurations(key string, defaultValue []time.Duration) []time.Duration {
	value, exists := os.LookupEnv(key)
	if !exists {
		return defaultValue
	}

	parts := SplitAndTrim(value)
	result := make([]time.Duration, 0, len(parts))
	for _, part := range parts {
		parsed, err := time.ParseDuration(part)
		if err != nil {
			return defaultValue
		}
		result = append(result, parsed)
	}
	return result
}

func SplitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
