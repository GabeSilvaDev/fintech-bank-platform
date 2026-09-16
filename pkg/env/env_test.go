package env

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGet(t *testing.T) {
	t.Setenv("ENV_TEST_STR", "value")
	assert.Equal(t, "value", Get("ENV_TEST_STR", "default"))
	assert.Equal(t, "default", Get("ENV_TEST_MISSING", "default"))
}

func TestGetInt(t *testing.T) {
	t.Setenv("ENV_TEST_INT", "42")
	t.Setenv("ENV_TEST_INT_BAD", "x")
	assert.Equal(t, 42, GetInt("ENV_TEST_INT", 1))
	assert.Equal(t, 1, GetInt("ENV_TEST_INT_BAD", 1))
	assert.Equal(t, 1, GetInt("ENV_TEST_MISSING", 1))
}

func TestGetIntMin(t *testing.T) {
	t.Setenv("ENV_TEST_INT_MIN", "0")
	t.Setenv("ENV_TEST_INT_OK", "7")
	assert.Equal(t, 3, GetIntMin("ENV_TEST_INT_MIN", 3, 1))
	assert.Equal(t, 7, GetIntMin("ENV_TEST_INT_OK", 3, 1))
}

func TestGetUint32(t *testing.T) {
	t.Setenv("ENV_TEST_U32", "9")
	t.Setenv("ENV_TEST_U32_NEG", "-1")
	t.Setenv("ENV_TEST_U32_ZERO", "0")
	assert.Equal(t, uint32(9), GetUint32("ENV_TEST_U32", 5))
	assert.Equal(t, uint32(5), GetUint32("ENV_TEST_U32_NEG", 5))
	assert.Equal(t, uint32(5), GetUint32("ENV_TEST_U32_ZERO", 5))
}

func TestGetBool(t *testing.T) {
	t.Setenv("ENV_TEST_BOOL", "true")
	t.Setenv("ENV_TEST_BOOL_BAD", "maybe")
	assert.True(t, GetBool("ENV_TEST_BOOL", false))
	assert.False(t, GetBool("ENV_TEST_BOOL_BAD", false))
	assert.True(t, GetBool("ENV_TEST_MISSING", true))
}

func TestGetDuration(t *testing.T) {
	t.Setenv("ENV_TEST_DUR", "2s")
	t.Setenv("ENV_TEST_DUR_BAD", "soon")
	assert.Equal(t, 2*time.Second, GetDuration("ENV_TEST_DUR", time.Second))
	assert.Equal(t, time.Second, GetDuration("ENV_TEST_DUR_BAD", time.Second))
}

func TestGetDurations(t *testing.T) {
	t.Setenv("ENV_TEST_DURS", "200ms, 1s ,5s")
	t.Setenv("ENV_TEST_DURS_BAD", "1s,nope")
	def := []time.Duration{time.Second}
	assert.Equal(t, []time.Duration{200 * time.Millisecond, time.Second, 5 * time.Second}, GetDurations("ENV_TEST_DURS", def))
	assert.Equal(t, def, GetDurations("ENV_TEST_DURS_BAD", def))
	assert.Equal(t, def, GetDurations("ENV_TEST_MISSING", def))
}

func TestSplitAndTrim(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, SplitAndTrim(" a , b ,, "))
	assert.Equal(t, []string{}, SplitAndTrim(""))
}
