package notifica

import (
	"os"
	"testing"
)

// TestEnviar mostra uma notificação de verdade: só roda com
// PRISMA_TESTE_NOTIFICA=1 (num computador com área de trabalho).
func TestEnviar(t *testing.T) {
	if os.Getenv("PRISMA_TESTE_NOTIFICA") != "1" {
		t.Skip("defina PRISMA_TESTE_NOTIFICA=1 para ver a notificação")
	}
	if err := Enviar("Prisma: 1 conta(s) vencem hoje", "Aluguel R$ 1.500,00 (hoje)\nreceber: Freela R$ 800,00 (amanhã)"); err != nil {
		t.Fatal(err)
	}
}
