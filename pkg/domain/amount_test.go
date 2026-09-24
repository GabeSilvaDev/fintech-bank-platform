package domain

import (
	"encoding/json"
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
		{"1", 100},
		{"1.5", 150},
		{"1.50", 150},
		{"1.05", 105},
		{"-1.50", -150},
		{"-0.50", -50},
		{"19.99", 1999},
		{"999999999999999", 99999999999999900},
		{"999999999999999.99", 99999999999999999},
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
		"1234567890123456",
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
	assert.True(t, IsInvalid(err))
	assert.Equal(t, "invalid_amount", InvalidCode(err))
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
