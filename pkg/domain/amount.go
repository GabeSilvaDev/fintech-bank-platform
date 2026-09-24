package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	amountPattern = regexp.MustCompile(`^-?[0-9]{1,17}(\.[0-9]{1,2})?$`)
	numberPattern = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)
)

type Amount int64

func AmountFromCents(cents int64) Amount {
	return Amount(cents)
}

func ParseAmount(s string) (Amount, error) {
	if !amountPattern.MatchString(s) {
		return 0, Invalid("invalid_amount", "amount must be a decimal number with up to two decimal places")
	}

	intPart, fracPart, _ := strings.Cut(strings.TrimPrefix(s, "-"), ".")
	return fromParts(strings.HasPrefix(s, "-"), intPart, fracPart)
}

func parseNumber(literal string) (Amount, error) {
	intPart, fracPart, _ := strings.Cut(strings.TrimPrefix(literal, "-"), ".")
	fracPart = strings.TrimRight(fracPart, "0")
	if len(fracPart) > 2 {
		return 0, Invalid("invalid_amount", "amount must have at most two decimal places")
	}
	if len(intPart) > 17 {
		return 0, Invalid("invalid_amount", "amount is out of range")
	}

	return fromParts(strings.HasPrefix(literal, "-"), intPart, fracPart)
}

func fromParts(negative bool, intPart, fracPart string) (Amount, error) {
	for len(fracPart) < 2 {
		fracPart += "0"
	}

	intValue, _ := strconv.ParseUint(intPart, 10, 64)
	fracValue, _ := strconv.ParseUint(fracPart, 10, 64)
	magnitude := intValue*100 + fracValue

	limit := uint64(math.MaxInt64)
	if negative {
		limit++
	}
	if magnitude > limit {
		return 0, Invalid("invalid_amount", "amount is out of range")
	}

	if negative {
		return Amount(int64(-magnitude)), nil
	}
	return Amount(int64(magnitude)), nil
}

func (a Amount) Cents() int64 {
	return int64(a)
}

func (a Amount) String() string {
	cents := int64(a)
	sign := ""
	var u uint64
	if cents < 0 {
		sign = "-"
		u = uint64(-(cents + 1)) + 1
	} else {
		u = uint64(cents)
	}

	return fmt.Sprintf("%s%d.%02d", sign, u/100, u%100)
}

func (a Amount) IsPositive() bool {
	return a > 0
}

func (a Amount) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.String())
}

func (a *Amount) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return Invalid("invalid_amount", "amount is required")
	}

	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		parsed, err := ParseAmount(s)
		if err != nil {
			return err
		}

		*a = parsed
		return nil
	}

	if numberPattern.Match(data) {
		parsed, err := parseNumber(string(data))
		if err != nil {
			return err
		}

		*a = parsed
		return nil
	}

	var f float64
	if err := json.Unmarshal(data, &f); err != nil {
		return Invalid("invalid_amount", "amount must be a string or a number")
	}

	scaled := f * 100
	rounded := math.Round(scaled)
	if math.Abs(scaled-rounded) > 1e-6 {
		return Invalid("invalid_amount", "amount must have at most two decimal places")
	}
	if rounded >= 0x1p63 || rounded < -0x1p63 {
		return Invalid("invalid_amount", "amount is out of range")
	}

	*a = Amount(int64(rounded))
	return nil
}
