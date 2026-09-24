package domain

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAmountFromCents(t *testing.T) {
	assert.Equal(t, int64(1050), AmountFromCents(1050).Cents())
	assert.Equal(t, int64(-500), AmountFromCents(-500).Cents())
	assert.Equal(t, int64(0), AmountFromCents(0).Cents())
}

func TestParseAmountValid(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"0", 0},
		{"0.00", 0},
		{"-0", 0},
		{"-0.00", 0},
		{"1", 100},
		{"1.5", 150},
		{"1.50", 150},
		{"1.05", 105},
		{"-1.50", -150},
		{"-0.50", -50},
		{"19.99", 1999},
		{"999999999999999", 99999999999999900},
		{"999999999999999.99", 99999999999999999},
		{"1234567890123456", 123456789012345600},
		{"92233720368547758.07", math.MaxInt64},
		{"-92233720368547758.08", math.MinInt64},
	}

	for _, c := range cases {
		got, err := ParseAmount(c.in)
		assert.NoError(t, err, c.in)
		assert.Equal(t, c.want, got.Cents(), c.in)
	}
}

func TestParseAmountInvalid(t *testing.T) {
	cases := []string{
		"",
		"1.",
		".5",
		"1.234",
		"1e2",
		" 1",
		"1 ",
		"+1",
		"1.5.5",
		"abc",
		"1,50",
		"123456789012345678",
	}

	for _, c := range cases {
		_, err := ParseAmount(c)
		assert.True(t, IsInvalid(err), c)
		assert.Equal(t, "invalid_amount", InvalidCode(err), c)
	}
}

func TestAmountString(t *testing.T) {
	assert.Equal(t, "0.00", AmountFromCents(0).String())
	assert.Equal(t, "3.00", AmountFromCents(300).String())
	assert.Equal(t, "-3.00", AmountFromCents(-300).String())
	assert.Equal(t, "-0.50", AmountFromCents(-50).String())
	assert.Equal(t, "0.50", AmountFromCents(50).String())
	assert.Equal(t, "19.99", AmountFromCents(1999).String())
}

func TestAmountStringExtremes(t *testing.T) {
	assert.Equal(t, "-92233720368547758.08", AmountFromCents(math.MinInt64).String())
	assert.Equal(t, "92233720368547758.07", AmountFromCents(math.MaxInt64).String())
}

func TestAmountIsPositive(t *testing.T) {
	assert.True(t, AmountFromCents(1).IsPositive())
	assert.False(t, AmountFromCents(0).IsPositive())
	assert.False(t, AmountFromCents(-1).IsPositive())
}

func TestAmountMarshalJSON(t *testing.T) {
	data, err := json.Marshal(AmountFromCents(1050))
	assert.NoError(t, err)
	assert.Equal(t, `"10.50"`, string(data))

	data, err = json.Marshal(AmountFromCents(-50))
	assert.NoError(t, err)
	assert.Equal(t, `"-0.50"`, string(data))
}

type amountStruct struct {
	Value   Amount  `json:"value"`
	Pointer *Amount `json:"pointer"`
}

func TestAmountMarshalJSONInStruct(t *testing.T) {
	pointer := AmountFromCents(500)
	s := amountStruct{Value: AmountFromCents(1050), Pointer: &pointer}

	data, err := json.Marshal(s)
	assert.NoError(t, err)
	assert.JSONEq(t, `{"value":"10.50","pointer":"5.00"}`, string(data))
}

func TestAmountUnmarshalJSONString(t *testing.T) {
	var a Amount
	err := json.Unmarshal([]byte(`"19.99"`), &a)
	assert.NoError(t, err)
	assert.Equal(t, int64(1999), a.Cents())

	err = json.Unmarshal([]byte(`"1.234"`), &a)
	assert.True(t, IsInvalid(err))
	assert.Equal(t, "invalid_amount", InvalidCode(err))
}

func TestAmountUnmarshalJSONNumber(t *testing.T) {
	var a Amount
	err := json.Unmarshal([]byte(`12.34`), &a)
	assert.NoError(t, err)
	assert.Equal(t, int64(1234), a.Cents())

	err = json.Unmarshal([]byte(`0.3`), &a)
	assert.NoError(t, err)
	assert.Equal(t, int64(30), a.Cents())

	err = json.Unmarshal([]byte(`1.005`), &a)
	assert.True(t, IsInvalid(err))
	assert.Equal(t, "invalid_amount", InvalidCode(err))

	err = json.Unmarshal([]byte(`10000000000001`), &a)
	assert.NoError(t, err)
	assert.Equal(t, int64(1000000000000100), a.Cents())
}

func TestParseAmountOutOfRange(t *testing.T) {
	for _, c := range []string{"92233720368547758.08", "-92233720368547758.09", "99999999999999999.99", "-99999999999999999"} {
		_, err := ParseAmount(c)
		assert.EqualError(t, err, "invalid_amount: amount is out of range", c)
	}
}

func TestAmountUnmarshalJSONLargeNumbersAreExact(t *testing.T) {
	cases := map[string]int64{
		`152626798.92`:          15262679892,
		`576091627.17`:          57609162717,
		`9292641860.54`:         929264186054,
		`-9292641860.54`:        -929264186054,
		`1.500`:                 150,
		`1.0`:                   100,
		`0`:                     0,
		`92233720368547758.07`:  math.MaxInt64,
		`-92233720368547758.08`: math.MinInt64,
	}
	for literal, want := range cases {
		var a Amount
		assert.NoError(t, json.Unmarshal([]byte(literal), &a), literal)
		assert.Equal(t, want, a.Cents(), literal)
	}
}

func TestAmountUnmarshalJSONRejectsBadNumberLiterals(t *testing.T) {
	cases := map[string]string{
		`1.505`:                 "invalid_amount: amount must have at most two decimal places",
		`0.001`:                 "invalid_amount: amount must have at most two decimal places",
		`92233720368547758.08`:  "invalid_amount: amount is out of range",
		`-92233720368547758.09`: "invalid_amount: amount is out of range",
		`100000000000000000`:    "invalid_amount: amount is out of range",
		`1.5e-3`:                "invalid_amount: amount must have at most two decimal places",
		`1e17`:                  "invalid_amount: amount is out of range",
		`-1e17`:                 "invalid_amount: amount is out of range",
	}
	for literal, want := range cases {
		var a Amount
		assert.EqualError(t, json.Unmarshal([]byte(literal), &a), want, literal)
	}
}

func TestAmountUnmarshalJSONExponentNumbers(t *testing.T) {
	var a Amount
	assert.NoError(t, json.Unmarshal([]byte(`1e2`), &a))
	assert.Equal(t, int64(10000), a.Cents())

	assert.NoError(t, json.Unmarshal([]byte(`-1.5E1`), &a))
	assert.Equal(t, int64(-1500), a.Cents())
}

func TestAmountExtremesRoundTrip(t *testing.T) {
	for _, cents := range []int64{math.MaxInt64, math.MinInt64} {
		data, err := json.Marshal(AmountFromCents(cents))
		assert.NoError(t, err)

		var decoded Amount
		assert.NoError(t, json.Unmarshal(data, &decoded), string(data))
		assert.Equal(t, cents, decoded.Cents())
	}
}

func TestAmountUnmarshalJSONNull(t *testing.T) {
	var a Amount
	err := a.UnmarshalJSON([]byte("null"))
	assert.True(t, IsInvalid(err))
	assert.Equal(t, "invalid_amount", InvalidCode(err))
	assert.EqualError(t, err, "invalid_amount: amount is required")
}

func TestAmountUnmarshalJSONWrongType(t *testing.T) {
	var a Amount
	err := json.Unmarshal([]byte("true"), &a)
	assert.True(t, IsInvalid(err))
	assert.Equal(t, "invalid_amount", InvalidCode(err))
}

func TestAmountUnmarshalJSONNullInStruct(t *testing.T) {
	var s amountStruct
	err := json.Unmarshal([]byte(`{"value":null,"pointer":null}`), &s)
	assert.True(t, IsInvalid(err))
	assert.Equal(t, "invalid_amount", InvalidCode(err))
}

func TestAmountUnmarshalJSONNullPointerFieldStaysNil(t *testing.T) {
	type pointerOnly struct {
		Pointer *Amount `json:"pointer"`
	}

	var s pointerOnly
	err := json.Unmarshal([]byte(`{"pointer":null}`), &s)
	assert.NoError(t, err)
	assert.Nil(t, s.Pointer)
}

func TestAmountRoundTrip(t *testing.T) {
	original := AmountFromCents(123456)
	data, err := json.Marshal(original)
	assert.NoError(t, err)

	var decoded Amount
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, original, decoded)
}
