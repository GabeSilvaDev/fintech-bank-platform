package unit

import (
	"math"
	"testing"
	"testing/fstest"

	"github.com/fintech-bank-platform/notification-service/internal/app/services"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/stretchr/testify/assert"
)

func renderer(t *testing.T) *services.Renderer {
	r, err := services.NewRenderer(services.Templates())
	assert.NoError(t, err)
	return r
}

func TestRenderWelcome(t *testing.T) {
	rendered, err := renderer(t).Render("welcome", map[string]string{"name": "Ana", "account_type": "corrente", "agency": "0001", "account_number": "12345678"})
	assert.NoError(t, err)
	assert.Equal(t, "Bem-vindo(a) ao Fintech Bank", rendered.Subject)
	assert.Equal(t, "Olá, Ana!\n\nSua conta corrente (agência 0001, número 12345678) está pronta para uso.", rendered.Body)
}

func TestRenderEveryTemplate(t *testing.T) {
	r := renderer(t)
	cases := map[string]struct {
		data    map[string]string
		subject string
		body    string
	}{
		"transaction_completed": {map[string]string{"operation": "Depósito", "amount": "R$ 100,00", "balance": "R$ 150,00"}, "Depósito concluído", "Depósito de R$ 100,00 concluído. Saldo atual: R$ 150,00."},
		"transaction_failed":    {map[string]string{"operation": "Saque", "operation_lower": "saque", "amount": "R$ 10,00", "reason": "saldo insuficiente"}, "Saque não realizado", "Seu saque de R$ 10,00 não foi realizado: saldo insuficiente."},
		"transfer_sent":         {map[string]string{"amount": "R$ 30,00", "balance": "R$ 70,00"}, "Transferência enviada", "Você enviou R$ 30,00. Saldo atual: R$ 70,00."},
		"transfer_received":     {map[string]string{"amount": "R$ 30,00", "balance": "R$ 30,00"}, "Transferência recebida", "Você recebeu R$ 30,00. Saldo atual: R$ 30,00."},
		"transfer_failed":       {map[string]string{"amount": "R$ 30,00", "reason": "conta inativa", "outcome": "O valor foi devolvido à sua conta."}, "Transferência não realizada", "Sua transferência de R$ 30,00 não foi concluída: conta inativa. O valor foi devolvido à sua conta."},
		"payment_completed":     {map[string]string{"method": "PIX", "amount": "R$ 42,50"}, "Pagamento concluído", "Seu pagamento via PIX de R$ 42,50 foi concluído."},
		"payment_failed":        {map[string]string{"method": "boleto", "amount": "R$ 150,00", "reason": "boleto não encontrado", "outcome": ""}, "Pagamento não realizado", "Seu pagamento via boleto de R$ 150,00 não foi concluído: boleto não encontrado."},
	}
	for kind, c := range cases {
		rendered, err := r.Render(kind, c.data)
		assert.NoError(t, err, kind)
		assert.Equal(t, c.subject, rendered.Subject, kind)
		assert.Equal(t, c.body, rendered.Body, kind)
	}
}

func TestRenderErrors(t *testing.T) {
	_, err := renderer(t).Render("nope", nil)
	assert.ErrorIs(t, err, services.ErrUnknownTemplate)

	_, err = renderer(t).Render("transfer_sent", map[string]string{"amount": "R$ 1,00"})
	assert.Error(t, err)

	_, err = services.NewRenderer(fstest.MapFS{"broken.tmpl": {Data: []byte(`{{define "subject"}}{{end`)}})
	assert.Error(t, err)

	partial, err := services.NewRenderer(fstest.MapFS{"half.tmpl": {Data: []byte(`{{define "subject"}}x{{end}}`)}})
	assert.NoError(t, err)
	_, err = partial.Render("half", nil)
	assert.Error(t, err)
}

func TestFormatBRL(t *testing.T) {
	cases := map[int64]string{0: "R$ 0,00", 10: "R$ 0,10", 4250: "R$ 42,50", 123456: "R$ 1.234,56", 100000000: "R$ 1.000.000,00", -4250: "-R$ 42,50", 100000: "R$ 1.000,00", math.MinInt64: "-R$ 92.233.720.368.547.758,08"}
	for cents, want := range cases {
		assert.Equal(t, want, services.FormatBRL(domain.AmountFromCents(cents)), cents)
	}
}

func TestLabels(t *testing.T) {
	assert.Equal(t, "saldo insuficiente", services.ReasonText("insufficient_funds"))
	assert.Equal(t, "conta inativa", services.ReasonText("account_not_active"))
	assert.Equal(t, "conta não encontrada", services.ReasonText("account_not_found"))
	assert.Equal(t, "limite de saldo excedido", services.ReasonText("balance_limit_exceeded"))
	assert.Equal(t, "chave PIX não encontrada", services.ReasonText("pix_key_not_found"))
	assert.Equal(t, "conta de destino inválida", services.ReasonText("invalid_destination"))
	assert.Equal(t, "boleto não encontrado", services.ReasonText("boleto_not_found"))
	assert.Equal(t, "recusado pela instituição", services.ReasonText("rejected_by_provider"))
	assert.Equal(t, "something_else", services.ReasonText("something_else"))

	title, lower := services.OperationLabel("deposit")
	assert.Equal(t, []string{"Depósito", "depósito"}, []string{title, lower})
	title, lower = services.OperationLabel("withdrawal")
	assert.Equal(t, []string{"Saque", "saque"}, []string{title, lower})
	title, lower = services.OperationLabel("other")
	assert.Equal(t, []string{"Lançamento", "lançamento"}, []string{title, lower})

	assert.Equal(t, "corrente", services.AccountTypeLabel("checking"))
	assert.Equal(t, "poupança", services.AccountTypeLabel("savings"))
	assert.Equal(t, "business", services.AccountTypeLabel("business"))

	assert.Equal(t, "PIX", services.MethodLabel("pix"))
	assert.Equal(t, "TED", services.MethodLabel("ted"))
	assert.Equal(t, "boleto", services.MethodLabel("boleto"))
	assert.Equal(t, "card", services.MethodLabel("card"))

	assert.Equal(t, "O valor foi devolvido à sua conta.", services.TransferOutcome("reversed"))
	assert.Equal(t, "Não conseguimos devolver o valor automaticamente; nossa equipe entrará em contato.", services.TransferOutcome("reversal_failed"))
	assert.Equal(t, "", services.TransferOutcome("failed"))
	assert.Equal(t, "O valor foi estornado para sua conta.", services.PaymentOutcome("refunded"))
	assert.Equal(t, "Não conseguimos estornar o valor automaticamente; nossa equipe entrará em contato.", services.PaymentOutcome("refund_failed"))
	assert.Equal(t, "", services.PaymentOutcome("failed"))
}
