//go:build e2e

package e2e

import (
	"testing"
)

func notificationSubjects(t *testing.T, c customer) []string {
	t.Helper()
	items := list(t, c.token, "/api/v1/users/"+c.userID+"/notifications?limit=50")
	subjects := make([]string, 0, len(items))
	for _, item := range items {
		if subject, ok := item["subject"].(string); ok {
			subjects = append(subjects, subject)
		}
	}
	return subjects
}

func hasAll(subjects []string, wanted ...string) bool {
	for _, subject := range wanted {
		if !contains(subjects, subject) {
			return false
		}
	}
	return true
}

func TestNotificationHistory(t *testing.T) {
	t.Parallel()
	sender := newCustomer(t, "")
	receiver := newCustomer(t, "")

	deposit(t, sender, "200.00")
	tx := transaction(t, sender, transfer(t, sender, receiver.accountID, "80.00"))
	if tx["status"] != "completed" {
		t.Fatalf("transfer did not complete: %v", tx)
	}

	var senderSubjects []string
	eventually(t, 0, func() bool {
		senderSubjects = notificationSubjects(t, sender)
		return hasAll(senderSubjects, "Depósito concluído", "Transferência enviada")
	}, "sender notifications: %v", &senderSubjects)

	var receiverSubjects []string
	eventually(t, 0, func() bool {
		receiverSubjects = notificationSubjects(t, receiver)
		return hasAll(receiverSubjects, "Transferência recebida")
	}, "receiver notifications: %v", &receiverSubjects)
}
