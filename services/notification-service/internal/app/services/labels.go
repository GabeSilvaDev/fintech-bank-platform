package services

var reasons = map[string]string{
	"insufficient_funds":   "saldo insuficiente",
	"account_not_active":   "conta inativa",
	"account_not_found":    "conta não encontrada",
	"pix_key_not_found":    "chave PIX não encontrada",
	"invalid_destination":  "conta de destino inválida",
	"boleto_not_found":     "boleto não encontrado",
	"rejected_by_provider": "recusado pela instituição",
}

func ReasonText(code string) string {
	if text, ok := reasons[code]; ok {
		return text
	}
	return code
}

func OperationLabel(transactionType string) (string, string) {
	switch transactionType {
	case "deposit":
		return "Depósito", "depósito"
	case "withdrawal":
		return "Saque", "saque"
	}
	return "Lançamento", "lançamento"
}

func AccountTypeLabel(accountType string) string {
	switch accountType {
	case "checking":
		return "corrente"
	case "savings":
		return "poupança"
	}
	return accountType
}

func MethodLabel(method string) string {
	switch method {
	case "pix":
		return "PIX"
	case "ted":
		return "TED"
	}
	return method
}

func TransferOutcome(status string) string {
	switch status {
	case "reversed":
		return "O valor foi devolvido à sua conta."
	case "reversal_failed":
		return "Não conseguimos devolver o valor automaticamente; nossa equipe entrará em contato."
	}
	return ""
}

func PaymentOutcome(status string) string {
	switch status {
	case "refunded":
		return "O valor foi estornado para sua conta."
	case "refund_failed":
		return "Não conseguimos estornar o valor automaticamente; nossa equipe entrará em contato."
	}
	return ""
}
