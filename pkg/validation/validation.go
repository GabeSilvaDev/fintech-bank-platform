// ═══════════════════════════════════════════════════════════════════════════
// Package validation - Shared validators for the Fintech Platform
// ═══════════════════════════════════════════════════════════════════════════

package validation

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/go-playground/validator/v10"
)

// ═══════════════════════════════════════════════════════════════════════════
// VALIDATOR SINGLETON
// ═══════════════════════════════════════════════════════════════════════════

var validate *validator.Validate

func init() {
	validate = validator.New()
	registerCustomValidators()
}

// GetValidator returns the singleton validator instance
func GetValidator() *validator.Validate {
	return validate
}

// ═══════════════════════════════════════════════════════════════════════════
// CUSTOM VALIDATORS REGISTRATION
// ═══════════════════════════════════════════════════════════════════════════

func registerCustomValidators() {
	validate.RegisterValidation("cpf", validateCPF)
	validate.RegisterValidation("cnpj", validateCNPJ)
	validate.RegisterValidation("phone_br", validateBrazilianPhone)
	validate.RegisterValidation("currency", validateCurrency)
	validate.RegisterValidation("password_strength", validatePasswordStrength)
	validate.RegisterValidation("account_number", validateAccountNumber)
	validate.RegisterValidation("agency_number", validateAgencyNumber)
	validate.RegisterValidation("pix_key", validatePixKey)
}

// ═══════════════════════════════════════════════════════════════════════════
// VALIDATION FUNCTIONS
// ═══════════════════════════════════════════════════════════════════════════

// Validate validates a struct using the validator
func Validate(s interface{}) error {
	return validate.Struct(s)
}

// ValidateVar validates a single variable against a tag
func ValidateVar(field interface{}, tag string) error {
	return validate.Var(field, tag)
}

// ═══════════════════════════════════════════════════════════════════════════
// BRAZILIAN CPF VALIDATION
// ═══════════════════════════════════════════════════════════════════════════

// validateCPF validates Brazilian CPF (Individual Taxpayer Registry)
func validateCPF(fl validator.FieldLevel) bool {
	cpf := fl.Field().String()
	return IsValidCPF(cpf)
}

// IsValidCPF checks if a CPF is valid
func IsValidCPF(cpf string) bool {
	// Remove non-digits
	cpf = regexp.MustCompile(`\D`).ReplaceAllString(cpf, "")

	// Must have 11 digits
	if len(cpf) != 11 {
		return false
	}

	// Check for known invalid CPFs (all same digits)
	if isAllSameDigits(cpf) {
		return false
	}

	// Calculate first digit
	sum := 0
	for i := 0; i < 9; i++ {
		digit := int(cpf[i] - '0')
		sum += digit * (10 - i)
	}
	remainder := sum % 11
	firstDigit := 0
	if remainder >= 2 {
		firstDigit = 11 - remainder
	}

	if int(cpf[9]-'0') != firstDigit {
		return false
	}

	// Calculate second digit
	sum = 0
	for i := 0; i < 10; i++ {
		digit := int(cpf[i] - '0')
		sum += digit * (11 - i)
	}
	remainder = sum % 11
	secondDigit := 0
	if remainder >= 2 {
		secondDigit = 11 - remainder
	}

	return int(cpf[10]-'0') == secondDigit
}

// ═══════════════════════════════════════════════════════════════════════════
// BRAZILIAN CNPJ VALIDATION
// ═══════════════════════════════════════════════════════════════════════════

// validateCNPJ validates Brazilian CNPJ (National Registry of Legal Entities)
func validateCNPJ(fl validator.FieldLevel) bool {
	cnpj := fl.Field().String()
	return IsValidCNPJ(cnpj)
}

// IsValidCNPJ checks if a CNPJ is valid
func IsValidCNPJ(cnpj string) bool {
	// Remove non-digits
	cnpj = regexp.MustCompile(`\D`).ReplaceAllString(cnpj, "")

	// Must have 14 digits
	if len(cnpj) != 14 {
		return false
	}

	// Check for known invalid CNPJs (all same digits)
	if isAllSameDigits(cnpj) {
		return false
	}

	// Weights for first digit
	weights1 := []int{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	// Weights for second digit
	weights2 := []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}

	// Calculate first digit
	sum := 0
	for i := 0; i < 12; i++ {
		digit := int(cnpj[i] - '0')
		sum += digit * weights1[i]
	}
	remainder := sum % 11
	firstDigit := 0
	if remainder >= 2 {
		firstDigit = 11 - remainder
	}

	if int(cnpj[12]-'0') != firstDigit {
		return false
	}

	// Calculate second digit
	sum = 0
	for i := 0; i < 13; i++ {
		digit := int(cnpj[i] - '0')
		sum += digit * weights2[i]
	}
	remainder = sum % 11
	secondDigit := 0
	if remainder >= 2 {
		secondDigit = 11 - remainder
	}

	return int(cnpj[13]-'0') == secondDigit
}

// ═══════════════════════════════════════════════════════════════════════════
// BRAZILIAN PHONE VALIDATION
// ═══════════════════════════════════════════════════════════════════════════

// validateBrazilianPhone validates Brazilian phone numbers
func validateBrazilianPhone(fl validator.FieldLevel) bool {
	phone := fl.Field().String()
	return IsValidBrazilianPhone(phone)
}

// IsValidBrazilianPhone checks if a Brazilian phone number is valid
func IsValidBrazilianPhone(phone string) bool {
	// Remove non-digits
	phone = regexp.MustCompile(`\D`).ReplaceAllString(phone, "")

	// Accept formats: 11 digits (with 9) or 10 digits (without 9)
	// With country code: 13 digits (55 + DDD + 9 + number) or 12 digits
	if len(phone) == 10 || len(phone) == 11 {
		return true
	}

	// With country code
	if (len(phone) == 12 || len(phone) == 13) && strings.HasPrefix(phone, "55") {
		return true
	}

	return false
}

// ═══════════════════════════════════════════════════════════════════════════
// CURRENCY VALIDATION
// ═══════════════════════════════════════════════════════════════════════════

// validateCurrency validates ISO 4217 currency codes
func validateCurrency(fl validator.FieldLevel) bool {
	currency := fl.Field().String()
	return IsValidCurrency(currency)
}

// IsValidCurrency checks if a currency code is valid (common codes)
func IsValidCurrency(code string) bool {
	validCurrencies := map[string]bool{
		"BRL": true, "USD": true, "EUR": true, "GBP": true,
		"JPY": true, "CNY": true, "ARS": true, "CLP": true,
		"COP": true, "MXN": true, "PEN": true, "UYU": true,
	}
	return validCurrencies[strings.ToUpper(code)]
}

// ═══════════════════════════════════════════════════════════════════════════
// PASSWORD STRENGTH VALIDATION
// ═══════════════════════════════════════════════════════════════════════════

// validatePasswordStrength validates password strength
func validatePasswordStrength(fl validator.FieldLevel) bool {
	password := fl.Field().String()
	return IsStrongPassword(password)
}

// IsStrongPassword checks if a password meets strength requirements
// Requirements: min 8 chars, 1 uppercase, 1 lowercase, 1 digit, 1 special char
func IsStrongPassword(password string) bool {
	if len(password) < 8 {
		return false
	}

	var hasUpper, hasLower, hasDigit, hasSpecial bool

	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsDigit(char):
			hasDigit = true
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			hasSpecial = true
		}
	}

	return hasUpper && hasLower && hasDigit && hasSpecial
}

// ═══════════════════════════════════════════════════════════════════════════
// BANKING VALIDATION
// ═══════════════════════════════════════════════════════════════════════════

// validateAccountNumber validates bank account numbers
func validateAccountNumber(fl validator.FieldLevel) bool {
	account := fl.Field().String()
	return IsValidAccountNumber(account)
}

// IsValidAccountNumber checks if an account number is valid
// Format: 5-12 digits with optional dash and check digit
func IsValidAccountNumber(account string) bool {
	// Remove formatting
	account = regexp.MustCompile(`[\s\-]`).ReplaceAllString(account, "")

	// Must be numeric and between 5-12 digits
	if len(account) < 5 || len(account) > 12 {
		return false
	}

	return regexp.MustCompile(`^\d+$`).MatchString(account)
}

// validateAgencyNumber validates bank agency numbers
func validateAgencyNumber(fl validator.FieldLevel) bool {
	agency := fl.Field().String()
	return IsValidAgencyNumber(agency)
}

// IsValidAgencyNumber checks if an agency number is valid
// Format: 4 digits with optional dash and check digit
func IsValidAgencyNumber(agency string) bool {
	// Remove formatting
	agency = regexp.MustCompile(`[\s\-]`).ReplaceAllString(agency, "")

	// Must be 4-5 digits (with optional check digit)
	if len(agency) < 4 || len(agency) > 5 {
		return false
	}

	return regexp.MustCompile(`^\d+$`).MatchString(agency)
}

// ═══════════════════════════════════════════════════════════════════════════
// PIX KEY VALIDATION
// ═══════════════════════════════════════════════════════════════════════════

// validatePixKey validates PIX keys (all types)
func validatePixKey(fl validator.FieldLevel) bool {
	key := fl.Field().String()
	return IsValidPixKey(key)
}

// IsValidPixKey checks if a PIX key is valid (CPF, CNPJ, Email, Phone, or Random)
func IsValidPixKey(key string) bool {
	// Check if it's a CPF
	if IsValidCPF(key) {
		return true
	}

	// Check if it's a CNPJ
	if IsValidCNPJ(key) {
		return true
	}

	// Check if it's an email
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	if emailRegex.MatchString(key) {
		return true
	}

	// Check if it's a Brazilian phone
	if IsValidBrazilianPhone(key) {
		return true
	}

	// Check if it's a random key (EVP - UUID format)
	uuidRegex := regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
	if uuidRegex.MatchString(strings.ToLower(key)) {
		return true
	}

	return false
}

// ═══════════════════════════════════════════════════════════════════════════
// HELPER FUNCTIONS
// ═══════════════════════════════════════════════════════════════════════════

// isAllSameDigits checks if all characters in a string are the same digit
func isAllSameDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	first := s[0]
	for i := 1; i < len(s); i++ {
		if s[i] != first {
			return false
		}
	}
	return true
}

// FormatCPF formats a CPF string to XXX.XXX.XXX-XX
func FormatCPF(cpf string) string {
	cpf = regexp.MustCompile(`\D`).ReplaceAllString(cpf, "")
	if len(cpf) != 11 {
		return cpf
	}
	return cpf[:3] + "." + cpf[3:6] + "." + cpf[6:9] + "-" + cpf[9:]
}

// FormatCNPJ formats a CNPJ string to XX.XXX.XXX/XXXX-XX
func FormatCNPJ(cnpj string) string {
	cnpj = regexp.MustCompile(`\D`).ReplaceAllString(cnpj, "")
	if len(cnpj) != 14 {
		return cnpj
	}
	return cnpj[:2] + "." + cnpj[2:5] + "." + cnpj[5:8] + "/" + cnpj[8:12] + "-" + cnpj[12:]
}

// FormatPhone formats a Brazilian phone to (XX) XXXXX-XXXX or (XX) XXXX-XXXX
func FormatPhone(phone string) string {
	phone = regexp.MustCompile(`\D`).ReplaceAllString(phone, "")

	// Remove country code if present
	if strings.HasPrefix(phone, "55") && len(phone) > 11 {
		phone = phone[2:]
	}

	if len(phone) == 11 {
		return "(" + phone[:2] + ") " + phone[2:7] + "-" + phone[7:]
	} else if len(phone) == 10 {
		return "(" + phone[:2] + ") " + phone[2:6] + "-" + phone[6:]
	}

	return phone
}

// SanitizeCPF removes formatting from CPF
func SanitizeCPF(cpf string) string {
	return regexp.MustCompile(`\D`).ReplaceAllString(cpf, "")
}

// SanitizeCNPJ removes formatting from CNPJ
func SanitizeCNPJ(cnpj string) string {
	return regexp.MustCompile(`\D`).ReplaceAllString(cnpj, "")
}

// SanitizePhone removes formatting from phone number
func SanitizePhone(phone string) string {
	return regexp.MustCompile(`\D`).ReplaceAllString(phone, "")
}
