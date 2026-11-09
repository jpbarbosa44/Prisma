package app

import (
	"testing"
	"time"
)

func TestInvestimentos(t *testing.T) {
	conn := abreDB(t)
	antes := relogio
	relogio = func() time.Time { return time.Date(2026, 11, 20, 10, 0, 0, 0, time.Local) }
	defer func() { relogio = antes }()
	conn.Exec(`INSERT INTO contas (id, nome, saldo_inicial) VALUES (1, 'Corrente', 1000000)`) // R$ 10.000

	// CDB: R$ 5.000 saídos da corrente em setembro; valia 5.100 em 31/10 e 5.180 hoje
	if err := Investir(conn, []string{"add", "--nome", "CDB Nu", "--tipo", "cdb", "--instituicao", "Nubank"}); err != nil {
		t.Fatal(err)
	}
	if err := Investir(conn, []string{"aportar", "1", "--valor", "5.000,00", "--de", "conta:1", "--data", "2026-09-01"}); err != nil {
		t.Fatal(err)
	}
	Investir(conn, []string{"atualizar", "1", "5.100,00", "--data", "2026-10-31"})
	Investir(conn, []string{"aportar", "1", "--valor", "1.000,00", "--de", "conta:1", "--data", "2026-11-10"})
	Investir(conn, []string{"atualizar", "1", "6.180,00", "--data", "2026-11-20"})
	// Tesouro que já existia antes do Prisma: sem --de, entra como saldo
	Investir(conn, []string{"add", "--nome", "Tesouro IPCA", "--tipo", "tesouro", "--valor", "2.000,00"})
	if err := Investir(conn, []string{"add", "--nome", "x", "--tipo", "imovel"}); err == nil {
		t.Fatal("tipo inválido")
	}

	is, err := ListaInvestimentos(conn, false)
	if err != nil || len(is) != 2 {
		t.Fatalf("%+v %v", is, err)
	}
	cdb := is[0]
	if cdb.Nome != "CDB Nu" || cdb.Aplicado != 600000 || cdb.Valor != 618000 || cdb.Rendimento != 18000 || cdb.Atualizado != "2026-11-20" {
		t.Fatalf("cdb: %+v", cdb)
	}
	// no mês: 6.180 − (5.100 + 1.000) = 80
	if cdb.RendMes != 8000 {
		t.Fatalf("rendimento do mês: %d", cdb.RendMes)
	}
	if ts := is[1]; ts.Aplicado != 200000 || ts.Valor != 200000 || ts.Atualizado != "" {
		t.Fatalf("tesouro: %+v", ts)
	}
	// a corrente perdeu 6.000 (transferências); a conta do CDB tem 6.000 e a do
	// Tesouro 2.000: o saldo total sobe só os 2.000 que vieram de fora
	if s, _ := saldoTotal(conn); s != 1200000 {
		t.Fatalf("saldo total: %d", s)
	}
	if s, _ := saldoConta(conn, 1); s != 400000 {
		t.Fatalf("corrente: %d", s)
	}

	conn.Exec(`INSERT INTO cartoes (id, nome, dia_fechamento, dia_vencimento) VALUES (1, 'Nu', 1, 10)`)
	conn.Exec(`INSERT INTO lancamentos (tipo, descricao, valor, vencimento, data_compra, cartao_id) VALUES ('pagar', 'mercado', 30000, '2026-12-10', '2026-11-15', 1)`)
	p, err := CalculaPatrimonio(conn)
	if err != nil {
		t.Fatal(err)
	}
	// 12.000 + 180 de rendimento − 300 de fatura
	if p.Liquido != 1188000 || p.Rendimento != 18000 || p.Faturas != 30000 || p.Investido != 800000 || p.ValorInvest != 818000 {
		t.Fatalf("patrimônio: %+v", p)
	}
	if n := len(p.Evolucao); n != 12 || p.Evolucao[n-1].Valor != p.Liquido {
		t.Fatalf("evolução: %+v", p.Evolucao)
	}
	// fim de outubro: 12.000 (o Tesouro entrou como saldo inicial) + 100 de rendimento, sem fatura
	if out := p.Evolucao[10]; out.Rotulo != "2026-10" || out.Valor != 1210000 {
		t.Fatalf("outubro: %+v", out)
	}

	// resgate para a corrente
	if err := Investir(conn, []string{"resgatar", "1", "--valor", "1.000,00", "--para", "conta:1"}); err != nil {
		t.Fatal(err)
	}
	if s, _ := saldoConta(conn, 1); s != 500000 {
		t.Fatalf("corrente depois do resgate: %d", s)
	}
	if err := Investir(conn, []string{"remover", "2"}); err != nil {
		t.Fatal(err)
	}
	if is, _ := ListaInvestimentos(conn, false); len(is) != 1 {
		t.Fatalf("remover: %+v", is)
	}
}
