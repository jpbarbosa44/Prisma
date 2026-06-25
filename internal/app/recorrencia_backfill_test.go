package app

import (
	"database/sql"
	"testing"
	"time"
)

func contaOcorrencias(t *testing.T, conn *sql.DB, recID int64) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lancamentos WHERE recorrencia_id = ?`, recID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Bug do --fim: encurtar (apaga ocorrências) e depois reestender deve trazer de
// volta os meses dentro do horizonte já materializado, em vez de deixar buracos.
func TestEditarFimReestendePreencheBuracos(t *testing.T) {
	conn := abreDB(t)
	now := time.Now()
	ini := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	fimLonge := now.AddDate(1, 0, 0).Format("2006-01-02")
	// fim curto = fim do mês seguinte (apaga as ocorrências de +2 e +3 meses)
	m1 := now.AddDate(0, 1, 0)
	fimCurto := time.Date(m1.Year(), m1.Month(), 28, 0, 0, 0, 0, time.UTC).Format("2006-01-02")

	if err := recorrenciaAdd(conn, []string{
		"--tipo", "pagar", "--desc", "Plano", "--valor", "100",
		"--dia", "15", "--inicio", ini, "--fim", fimLonge, "--passados", "manter",
	}); err != nil {
		t.Fatal(err)
	}
	completo := contaOcorrencias(t, conn, 1) // mês atual + 3 = 4 (horizonte de 3 meses)
	if completo < 3 {
		t.Fatalf("esperava ao menos 3 ocorrências após add, veio %d", completo)
	}

	// encurta: apaga as que ficam após o término
	if err := recorrenciaEditar(conn, []string{"1", "--fim", fimCurto}); err != nil {
		t.Fatal(err)
	}
	encurtado := contaOcorrencias(t, conn, 1)
	if encurtado >= completo {
		t.Fatalf("encurtar fim deveria apagar ocorrências (%d -> %d)", completo, encurtado)
	}

	// reestende: deve preencher os buracos de volta
	if err := recorrenciaEditar(conn, []string{"1", "--fim", fimLonge}); err != nil {
		t.Fatal(err)
	}
	if got := contaOcorrencias(t, conn, 1); got != completo {
		t.Fatalf("reestender fim deveria restaurar %d ocorrências, veio %d (buraco permanente = bug)", completo, got)
	}

	// idempotência: regerar não duplica
	if _, err := GerarRecorrencias(conn); err != nil {
		t.Fatal(err)
	}
	if got := contaOcorrencias(t, conn, 1); got != completo {
		t.Fatalf("gerar de novo duplicou: %d (esperava %d)", got, completo)
	}
}

// Reativar uma recorrência encerrada (fim no passado → estende) preenche os
// meses que tinham sido pulados enquanto a regra estava dormente.
func TestEditarFimReativaEncerrada(t *testing.T) {
	conn := abreDB(t)
	now := time.Now()
	ini := now.AddDate(0, -2, 0)
	iniStr := time.Date(ini.Year(), ini.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	// fim já no passado: a regra nasce praticamente encerrada
	fimPassado := now.AddDate(0, -1, 0).Format("2006-01-02")

	if err := recorrenciaAdd(conn, []string{
		"--tipo", "receber", "--desc", "Bolsa", "--valor", "300",
		"--dia", "10", "--inicio", iniStr, "--fim", fimPassado, "--passados", "manter",
	}); err != nil {
		t.Fatal(err)
	}
	antes := contaOcorrencias(t, conn, 1)

	// estende bem para a frente: deve materializar os meses até o horizonte
	fimLonge := now.AddDate(1, 0, 0).Format("2006-01-02")
	if err := recorrenciaEditar(conn, []string{"1", "--fim", fimLonge}); err != nil {
		t.Fatal(err)
	}
	depois := contaOcorrencias(t, conn, 1)
	if depois <= antes {
		t.Fatalf("reativar deveria gerar novos meses (%d -> %d)", antes, depois)
	}
}
