//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func statementPage(t *testing.T, c customer, before string) ([]map[string]interface{}, string) {
	t.Helper()
	path := "/api/v1/accounts/" + c.accountID + "/transactions?limit=2"
	if before != "" {
		path += "&before=" + url.QueryEscape(before)
	}
	status, header, raw := exchange(t, http.MethodGet, gateway()+path, c.token, nil)
	require.Equal(t, http.StatusOK, status, "GET %s: %s", path, raw)
	out := decodeEnvelope(t, raw)
	require.True(t, out.Success, "GET %s: %s", path, raw)
	var items []map[string]interface{}
	require.NoError(t, json.Unmarshal(out.Data, &items))
	return items, header.Get("X-Next-Before")
}

func TestStatementPagination(t *testing.T) {
	t.Parallel()
	c := newCustomer(t, "")

	deposits := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		tx := transaction(t, c, movement(t, c, "deposit", "10.00"))
		require.Equal(t, "completed", tx["status"], "deposit: %v", tx)
		deposits = append(deposits, tx["transaction_id"].(string))
	}

	var seen []string
	before := ""
	for pages := 0; ; pages++ {
		require.Less(t, pages, 5, "statement kept returning X-Next-Before: %v", seen)
		items, next := statementPage(t, c, before)
		require.LessOrEqual(t, len(items), 2)
		for _, item := range items {
			seen = append(seen, item["transaction_id"].(string))
		}
		if next == "" {
			break
		}
		require.NotEqual(t, before, next)
		before = next
	}

	expected := make([]string, 0, len(deposits))
	for i := len(deposits) - 1; i >= 0; i-- {
		expected = append(expected, deposits[i])
	}
	require.Equal(t, expected, seen)
}
