package models

import (
	"strings"

	"github.com/google/uuid"
)

type Step string

const (
	StepDebit    Step = "debit"
	StepCredit   Step = "credit"
	StepReversal Step = "reversal"
)

func StepKey(id uuid.UUID, step Step) string {
	return id.String() + ":" + string(step)
}

func ParseStepKey(key string) (uuid.UUID, Step, bool) {
	parts := strings.Split(key, ":")
	if len(parts) != 2 {
		return uuid.Nil, "", false
	}
	id, err := uuid.Parse(parts[0])
	if err != nil {
		return uuid.Nil, "", false
	}
	switch Step(parts[1]) {
	case StepDebit, StepCredit, StepReversal:
		return id, Step(parts[1]), true
	}
	return uuid.Nil, "", false
}
