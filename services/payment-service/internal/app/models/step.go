package models

import (
	"strings"

	"github.com/google/uuid"
)

type Step string

const (
	StepDebit  Step = "debit"
	StepRefund Step = "refund"
)

const referenceScope = "payment"

func Reference(id uuid.UUID) string {
	return referenceScope + ":" + id.String()
}

func ParseReference(reference string) (uuid.UUID, bool) {
	parts := strings.Split(reference, ":")
	if len(parts) != 2 || parts[0] != referenceScope {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

func StepKey(id uuid.UUID, step Step) string {
	return Reference(id) + ":" + string(step)
}

func ParseStepKey(key string) (uuid.UUID, Step, bool) {
	cut := strings.LastIndex(key, ":")
	if cut < 0 {
		return uuid.Nil, "", false
	}
	id, ok := ParseReference(key[:cut])
	if !ok {
		return uuid.Nil, "", false
	}
	switch Step(key[cut+1:]) {
	case StepDebit, StepRefund:
		return id, Step(key[cut+1:]), true
	}
	return uuid.Nil, "", false
}
