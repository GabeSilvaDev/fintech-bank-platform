package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var amountPattern = regexp.MustCompile(`^-?[0-9]{1,15}(\.[0-9]{1,2})?$`)

type Amount int64

func AmountFromCents(cents int64) Amount {
	return Amount(cents)
}

func ParseAmount(s string) (Amount, error) {
	if !amountPattern.MatchString(s) {
		return 0, Invalid("invalid_amount", "amount must be a decimal number with up to two decimal places")
	}

	negative := false
	rest := s
	if strings.HasPrefix(rest, "-") {
		negative = true
		rest = rest[1:]
	}

	intPart := rest
	fracPart := ""
	if idx := strings.IndexByte(rest, '.'); idx >= 0 {
		intPart = rest[:idx]
		fracPart = rest[idx+1:]
	}
	for len(fracPart) < 2 {
		fracPart += "0"
	}

	intValue, _ := strconv.ParseInt(intPart, 10, 64)
	fracValue, _ := strconv.ParseInt(fracPart, 10, 64)

	cents := intValue*100 + fracValue
	if negative {
		cents = -cents
	}

	return Amount(cents), nil
}

func (a Amount) Cents() int64 {
	return int64(a)
}

func (a Amount) String() string {
	cents := int64(a)
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}

	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
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

	var f float64
	if err := json.Unmarshal(data, &f); err != nil {
		return Invalid("invalid_amount", "amount must be a string or a number")
	}

	scaled := f * 100
	rounded := math.Round(scaled)
	if math.Abs(scaled-rounded) > 1e-6 {
		return Invalid("invalid_amount", "amount must have at most two decimal places")
	}
	if rounded > 1e15 || rounded < -1e15 {
		return Invalid("invalid_amount", "amount is out of range")
	}

	*a = Amount(int64(rounded))
	return nil
}
