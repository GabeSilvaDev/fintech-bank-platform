package domain

import "math"

func ToCents(amount float64) (int64, error) {
	if amount <= 0 {
		return 0, Invalid("invalid_amount", "amount must be greater than zero")
	}

	scaled := amount * 100
	cents := math.Round(scaled)
	if math.Abs(scaled-cents) > 1e-6 {
		return 0, Invalid("invalid_amount", "amount must have at most two decimal places")
	}

	return int64(cents), nil
}

func FromCents(cents int64) float64 {
	return float64(cents) / 100
}

func Cents(amount float64) int64 {
	return int64(math.Round(amount * 100))
}
