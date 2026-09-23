//go:build e2e

package e2e

import (
	"testing"
)

func notificationSubjects(t *testing.T, userID string) []string {
	t.Helper()
	items := list(t, "/api/v1/users/"+userID+"/notifications?limit=50")
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
	senderUser, sender := newCustomer(t, "")
	receiverUser, receiver := newCustomer(t, "")

	deposit(t, sender, 200)
	tx := transaction(t, sender, transfer(t, sender, receiver, 80))
	if tx["status"] != "completed" {
		t.Fatalf("transfer did not complete: %v", tx)
	}

	var senderSubjects []string
	eventually(t, 0, func() bool {
		senderSubjects = notificationSubjects(t, senderUser)
		return hasAll(senderSubjects, "Depósito concluído", "Transferência enviada")
	}, "sender notifications: %v", &senderSubjects)

	var receiverSubjects []string
	eventually(t, 0, func() bool {
		receiverSubjects = notificationSubjects(t, receiverUser)
		return hasAll(receiverSubjects, "Transferência recebida")
	}, "receiver notifications: %v", &receiverSubjects)
}
