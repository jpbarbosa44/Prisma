package app

import (
	"fmt"
	"testing"
)

func TestAssinaturaAnualListaEEdita(t *testing.T) {
	conn := abreDB(t)

	// uma assinatura mensal e uma anuidade
	silencia(t, func() error {
		return Assinaturas(conn, []string{"add", "--desc", "Netflix", "--valor", "39,90", "--dia", "20"})
	})
	silencia(t, func() error {
		return Assinaturas(conn, []string{"add", "--desc", "Amazon Prime", "--valor", "119,00", "--dia", "15", "--intervalo", "anual"})
	})

	// a anuidade foi gravada como anual
	var iv string
	if err := conn.QueryRow(`SELECT intervalo FROM recorrencias WHERE descricao = 'Amazon Prime'`).Scan(&iv); err != nil {
		t.Fatal(err)
	}
	if iv != "anual" {
		t.Errorf("Amazon Prime intervalo = %q, quer anual", iv)
	}

	// a mensal gerou vários pendentes dentro do horizonte
	var antes int
	if err := conn.QueryRow(
		`SELECT COUNT(*) FROM lancamentos l JOIN recorrencias r ON r.id = l.recorrencia_id
		 WHERE r.descricao = 'Netflix' AND l.status = 'pendente'`).Scan(&antes); err != nil {
		t.Fatal(err)
	}
	if antes < 2 {
		t.Fatalf("Netflix mensal devia ter vários pendentes, tem %d", antes)
	}

	// troca a mensal para anual: descarta os mensais e refaz com a cadência anual
	var recID int64
	if err := conn.QueryRow(`SELECT id FROM recorrencias WHERE descricao = 'Netflix'`).Scan(&recID); err != nil {
		t.Fatal(err)
	}
	silencia(t, func() error {
		return Assinaturas(conn, []string{"editar", fmt.Sprint(recID), "--intervalo", "anual"})
	})

	if err := conn.QueryRow(`SELECT intervalo FROM recorrencias WHERE id = ?`, recID).Scan(&iv); err != nil {
		t.Fatal(err)
	}
	if iv != "anual" {
		t.Errorf("após editar, Netflix intervalo = %q, quer anual", iv)
	}

	var depois int
	if err := conn.QueryRow(
		`SELECT COUNT(*) FROM lancamentos WHERE recorrencia_id = ? AND status = 'pendente'`, recID).Scan(&depois); err != nil {
		t.Fatal(err)
	}
	if depois >= antes {
		t.Errorf("anual devia ter menos pendentes que mensal: antes=%d depois=%d", antes, depois)
	}
	// todo pendente remanescente cai no mês de aniversário (o mês do início)
	rows, err := conn.Query(
		`SELECT substr(COALESCE(data_compra, vencimento),6,2) FROM lancamentos
		 WHERE recorrencia_id = ? AND status = 'pendente'`, recID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var mesIni string
	if err := conn.QueryRow(`SELECT substr(inicio,6,2) FROM recorrencias WHERE id = ?`, recID).Scan(&mesIni); err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			t.Fatal(err)
		}
		if m != mesIni {
			t.Errorf("pendente anual no mês %q, quer só o de aniversário %q", m, mesIni)
		}
	}
}
