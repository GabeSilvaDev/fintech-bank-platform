package models

type SubmissionStatus string

const (
	SubmissionSettled  SubmissionStatus = "settled"
	SubmissionPending  SubmissionStatus = "pending"
	SubmissionRejected SubmissionStatus = "rejected"
)

type Submission struct {
	ExternalID string
	Status     SubmissionStatus
	Reason     string
}

func ParseSettlementStatus(s string) (SubmissionStatus, bool) {
	switch SubmissionStatus(s) {
	case SubmissionSettled, SubmissionRejected:
		return SubmissionStatus(s), true
	}
	return "", false
}
