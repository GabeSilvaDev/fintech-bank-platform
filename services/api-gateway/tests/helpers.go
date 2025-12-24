// ═══════════════════════════════════════════════════════════════════════════
// Test Helpers - Utility functions for testing
// ═══════════════════════════════════════════════════════════════════════════

package tests

import (
	"encoding/json"
	"math/rand"
	"time"

	"github.com/google/uuid"
)

// ═══════════════════════════════════════════════════════════════════════════
// Random Data Generators
// ═══════════════════════════════════════════════════════════════════════════

var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

func UUID() string {
	return uuid.New().String()
}

func RandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[rng.Intn(len(charset))]
	}
	return string(result)
}

func RandomEmail() string {
	return RandomString(10) + "@test.com"
}

func RandomInt(min, max int) int {
	return rng.Intn(max-min+1) + min
}

func RandomFloat(min, max float64) float64 {
	return min + rng.Float64()*(max-min)
}

func RandomBool() bool {
	return rng.Intn(2) == 1
}

func RandomChoice[T any](choices []T) T {
	return choices[rng.Intn(len(choices))]
}

// ═══════════════════════════════════════════════════════════════════════════
// JSON Helpers
// ═══════════════════════════════════════════════════════════════════════════

func ToJson(data interface{}) string {
	bytes, _ := json.Marshal(data)
	return string(bytes)
}

func FromJson(jsonStr string) map[string]interface{} {
	var result map[string]interface{}
	json.Unmarshal([]byte(jsonStr), &result)
	return result
}

// ═══════════════════════════════════════════════════════════════════════════
// Time Helpers
// ═══════════════════════════════════════════════════════════════════════════

func Now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func FutureTime(duration time.Duration) string {
	return time.Now().Add(duration).UTC().Format(time.RFC3339)
}

func PastTime(duration time.Duration) string {
	return time.Now().Add(-duration).UTC().Format(time.RFC3339)
}
