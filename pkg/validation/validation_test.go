// ═══════════════════════════════════════════════════════════════════════════
// Package validation - Tests
// ═══════════════════════════════════════════════════════════════════════════

package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ═══════════════════════════════════════════════════════════════════════════
// VALIDATOR TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestGetValidator(t *testing.T) {
	v := GetValidator()
	assert.NotNil(t, v)
}

func TestValidate(t *testing.T) {
	type TestStruct struct {
		Email string `validate:"required,email"`
	}

	t.Run("valid struct", func(t *testing.T) {
		s := TestStruct{Email: "test@example.com"}
		err := Validate(s)
		assert.NoError(t, err)
	})

	t.Run("invalid struct", func(t *testing.T) {
		s := TestStruct{Email: "invalid"}
		err := Validate(s)
		assert.Error(t, err)
	})
}

func TestValidateVar(t *testing.T) {
	t.Run("valid email", func(t *testing.T) {
		err := ValidateVar("test@example.com", "email")
		assert.NoError(t, err)
	})

	t.Run("invalid email", func(t *testing.T) {
		err := ValidateVar("invalid", "email")
		assert.Error(t, err)
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// CPF TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestIsValidCPF(t *testing.T) {
	tests := []struct {
		name     string
		cpf      string
		expected bool
	}{
		{"valid CPF without formatting", "52998224725", true},
		{"valid CPF with formatting", "529.982.247-25", true},
		{"invalid CPF wrong check digit", "12345678901", false},
		{"invalid CPF all same digits", "11111111111", false},
		{"invalid CPF too short", "1234567890", false},
		{"invalid CPF too long", "123456789012", false},
		{"invalid CPF empty", "", false},
		{"valid CPF 2", "11144477735", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidCPF(tt.cpf)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCPFValidator(t *testing.T) {
	type TestStruct struct {
		CPF string `validate:"cpf"`
	}

	t.Run("valid CPF", func(t *testing.T) {
		s := TestStruct{CPF: "52998224725"}
		err := Validate(s)
		assert.NoError(t, err)
	})

	t.Run("invalid CPF", func(t *testing.T) {
		s := TestStruct{CPF: "12345678901"}
		err := Validate(s)
		assert.Error(t, err)
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// CNPJ TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestIsValidCNPJ(t *testing.T) {
	tests := []struct {
		name     string
		cnpj     string
		expected bool
	}{
		{"valid CNPJ without formatting", "11222333000181", true},
		{"valid CNPJ with formatting", "11.222.333/0001-81", true},
		{"invalid CNPJ wrong check digit", "12345678000199", false},
		{"invalid CNPJ all same digits", "11111111111111", false},
		{"invalid CNPJ too short", "1234567800018", false},
		{"invalid CNPJ too long", "112223330001810", false},
		{"invalid CNPJ empty", "", false},
		{"invalid CNPJ wrong first digit", "11222333000191", false},
		{"invalid CNPJ wrong second digit", "11222333000182", false},
		{"valid CNPJ with remainder less than 2", "11222333000100", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidCNPJ(tt.cnpj)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCNPJValidator(t *testing.T) {
	type TestStruct struct {
		CNPJ string `validate:"cnpj"`
	}

	t.Run("valid CNPJ", func(t *testing.T) {
		s := TestStruct{CNPJ: "11222333000181"}
		err := Validate(s)
		assert.NoError(t, err)
	})

	t.Run("invalid CNPJ", func(t *testing.T) {
		s := TestStruct{CNPJ: "12345678000199"}
		err := Validate(s)
		assert.Error(t, err)
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// BRAZILIAN PHONE TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestIsValidBrazilianPhone(t *testing.T) {
	tests := []struct {
		name     string
		phone    string
		expected bool
	}{
		{"valid mobile 11 digits", "11999887766", true},
		{"valid landline 10 digits", "1133224455", true},
		{"valid with country code 13 digits", "5511999887766", true},
		{"valid with country code 12 digits", "551133224455", true},
		{"valid with formatting", "(11) 99988-7766", true},
		{"invalid too short", "119998877", false},
		{"invalid too long", "119998877660000", false},
		{"invalid empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidBrazilianPhone(tt.phone)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBrazilianPhoneValidator(t *testing.T) {
	type TestStruct struct {
		Phone string `validate:"phone_br"`
	}

	t.Run("valid phone", func(t *testing.T) {
		s := TestStruct{Phone: "11999887766"}
		err := Validate(s)
		assert.NoError(t, err)
	})

	t.Run("invalid phone", func(t *testing.T) {
		s := TestStruct{Phone: "123"}
		err := Validate(s)
		assert.Error(t, err)
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// CURRENCY TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestIsValidCurrency(t *testing.T) {
	tests := []struct {
		name     string
		currency string
		expected bool
	}{
		{"valid BRL", "BRL", true},
		{"valid USD", "USD", true},
		{"valid EUR lowercase", "eur", true},
		{"invalid currency", "XXX", false},
		{"invalid empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidCurrency(tt.currency)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCurrencyValidator(t *testing.T) {
	type TestStruct struct {
		Currency string `validate:"currency"`
	}

	t.Run("valid currency", func(t *testing.T) {
		s := TestStruct{Currency: "BRL"}
		err := Validate(s)
		assert.NoError(t, err)
	})

	t.Run("invalid currency", func(t *testing.T) {
		s := TestStruct{Currency: "XXX"}
		err := Validate(s)
		assert.Error(t, err)
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// PASSWORD STRENGTH TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestIsStrongPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		expected bool
	}{
		{"strong password", "Password1!", true},
		{"strong password complex", "MyP@ssw0rd", true},
		{"weak no uppercase", "password1!", false},
		{"weak no lowercase", "PASSWORD1!", false},
		{"weak no digit", "Password!", false},
		{"weak no special", "Password1", false},
		{"weak too short", "Pass1!", false},
		{"weak empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsStrongPassword(tt.password)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPasswordStrengthValidator(t *testing.T) {
	type TestStruct struct {
		Password string `validate:"password_strength"`
	}

	t.Run("strong password", func(t *testing.T) {
		s := TestStruct{Password: "Password1!"}
		err := Validate(s)
		assert.NoError(t, err)
	})

	t.Run("weak password", func(t *testing.T) {
		s := TestStruct{Password: "weak"}
		err := Validate(s)
		assert.Error(t, err)
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// BANKING VALIDATION TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestIsValidAccountNumber(t *testing.T) {
	tests := []struct {
		name     string
		account  string
		expected bool
	}{
		{"valid 5 digits", "12345", true},
		{"valid 12 digits", "123456789012", true},
		{"valid with dash", "12345-6", true},
		{"invalid too short", "1234", false},
		{"invalid too long", "1234567890123", false},
		{"invalid with letters", "1234A", false},
		{"invalid empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidAccountNumber(tt.account)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsValidAgencyNumber(t *testing.T) {
	tests := []struct {
		name     string
		agency   string
		expected bool
	}{
		{"valid 4 digits", "1234", true},
		{"valid 5 digits", "12345", true},
		{"valid with dash", "1234-5", true},
		{"invalid too short", "123", false},
		{"invalid too long", "123456", false},
		{"invalid with letters", "123A", false},
		{"invalid empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidAgencyNumber(tt.agency)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAccountNumberValidator(t *testing.T) {
	type TestStruct struct {
		Account string `validate:"account_number"`
	}

	t.Run("valid account", func(t *testing.T) {
		s := TestStruct{Account: "12345678"}
		err := Validate(s)
		assert.NoError(t, err)
	})

	t.Run("invalid account", func(t *testing.T) {
		s := TestStruct{Account: "123"}
		err := Validate(s)
		assert.Error(t, err)
	})
}

func TestAgencyNumberValidator(t *testing.T) {
	type TestStruct struct {
		Agency string `validate:"agency_number"`
	}

	t.Run("valid agency", func(t *testing.T) {
		s := TestStruct{Agency: "1234"}
		err := Validate(s)
		assert.NoError(t, err)
	})

	t.Run("invalid agency", func(t *testing.T) {
		s := TestStruct{Agency: "12"}
		err := Validate(s)
		assert.Error(t, err)
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// PIX KEY TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestIsValidPixKey(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected bool
	}{
		{"valid CPF key", "52998224725", true},
		{"valid CNPJ key", "11222333000181", true},
		{"valid email key", "user@example.com", true},
		{"valid phone key", "11999887766", true},
		{"valid random key (EVP)", "123e4567-e89b-12d3-a456-426614174000", true},
		{"invalid key", "invalid-key", false},
		{"invalid empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidPixKey(tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPixKeyValidator(t *testing.T) {
	type TestStruct struct {
		PixKey string `validate:"pix_key"`
	}

	t.Run("valid pix key CPF", func(t *testing.T) {
		s := TestStruct{PixKey: "52998224725"}
		err := Validate(s)
		assert.NoError(t, err)
	})

	t.Run("valid pix key email", func(t *testing.T) {
		s := TestStruct{PixKey: "user@example.com"}
		err := Validate(s)
		assert.NoError(t, err)
	})

	t.Run("invalid pix key", func(t *testing.T) {
		s := TestStruct{PixKey: "invalid"}
		err := Validate(s)
		assert.Error(t, err)
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// FORMATTER TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestFormatCPF(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"format valid CPF", "52998224725", "529.982.247-25"},
		{"already formatted", "529.982.247-25", "529.982.247-25"},
		{"invalid length", "123456789", "123456789"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatCPF(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatCNPJ(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"format valid CNPJ", "11222333000181", "11.222.333/0001-81"},
		{"already formatted", "11.222.333/0001-81", "11.222.333/0001-81"},
		{"invalid length", "123456789", "123456789"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatCNPJ(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatPhone(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"format mobile 11 digits", "11999887766", "(11) 99988-7766"},
		{"format landline 10 digits", "1133224455", "(11) 3322-4455"},
		{"with country code", "5511999887766", "(11) 99988-7766"},
		{"invalid length", "123456789", "123456789"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatPhone(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeCPF(t *testing.T) {
	result := SanitizeCPF("529.982.247-25")
	assert.Equal(t, "52998224725", result)
}

func TestSanitizeCNPJ(t *testing.T) {
	result := SanitizeCNPJ("11.222.333/0001-81")
	assert.Equal(t, "11222333000181", result)
}

func TestSanitizePhone(t *testing.T) {
	result := SanitizePhone("(11) 99988-7766")
	assert.Equal(t, "11999887766", result)
}

// ═══════════════════════════════════════════════════════════════════════════
// HELPER FUNCTION TESTS
// ═══════════════════════════════════════════════════════════════════════════

func TestIsAllSameDigits(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"empty string", "", false},
		{"single digit", "1", true},
		{"all same digits", "11111", true},
		{"different digits", "12345", false},
		{"all zeros", "00000", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAllSameDigits(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
