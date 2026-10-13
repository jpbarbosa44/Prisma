package app

import (
	"database/sql"
	"strings"
	"testing"
)

// pendente insere uma conta pendente (tipo pagar/receber) com vencimento.
func pendente(t *testing.T, conn *sql.DB, tipo, desc string, centavos int64, venc string) {
	t.Helper()
	if _, err := conn.Exec(`INSERT INTO lancamentos (tipo, descricao, valor, categoria, vencimento)
		VALUES (?, ?, ?, 'geral', ?)`, tipo, desc, centavos, venc); err != nil {
		t.Fatal(err)
	}
}

func TestCalcRitmo(t *testing.T) {
	conn := abreDB(t)
	// três meses de base: R$ 100 no dia 5 e R$ 300 no dia 20 de cada mês
	for _, mes := range []string{"2026-01", "2026-02", "2026-03"} {
		gastoQuitado(t, conn, "Feira", "mercado", 10000, mes+"-05")
		gastoQuitado(t, conn, "Jantar", "restaurante", 30000, mes+"-20")
	}
	gastoQuitado(t, conn, "Feira", "mercado", 10000, "2026-04-05")
	gastoQuitado(t, conn, "Outback", "restaurante", 25000, "2026-04-08")

	fixaRelogio(t, "2026-04-10")
	r, err := CalcRitmo(conn, Opcoes{Meses: 3})
	if err != nil {
		t.Fatal(err)
	}
	if r.MesesBase != 3 || r.Dia != 10 || r.DiasNoMes != 30 {
		t.Fatalf("base/dia = %d/%d/%d", r.MesesBase, r.Dia, r.DiasNoMes)
	}
	// até o dia 10: base R$ 100/mês, atual R$ 350 → 350%
	if r.AteHoje != 35000 || r.MediaAteDia != 10000 || r.MesPassadoAteDia != 10000 || r.Indice != 350 {
		t.Fatalf("ritmo = %+v", r)
	}
	// projeção: 350 + restante médio (400 − 100 = 300), sem agendado
	if r.MediaMes != 40000 || r.Projecao != 65000 {
		t.Errorf("média/projeção = %d/%d", r.MediaMes, r.Projecao)
	}
	if len(r.Aceleraram) != 1 || r.Aceleraram[0].Categoria != "restaurante" || len(r.Aceleraram[0].Maiores) != 1 {
		t.Errorf("aceleraram = %+v", r.Aceleraram)
	}

	// agendado maior que o restante médio vira o piso da projeção
	pendente(t, conn, "pagar", "IPTU", 80000, "2026-04-25")
	if r, _ = CalcRitmo(conn, Opcoes{Meses: 3}); r.Agendado != 80000 || r.Projecao != 115000 {
		t.Errorf("com agendado: agendado=%d projeção=%d", r.Agendado, r.Projecao)
	}

	// dia 1º: só o que caiu no dia 1 conta, nos dois lados
	fixaRelogio(t, "2026-04-01")
	if r, _ = CalcRitmo(conn, Opcoes{Meses: 3}); r.AteHoje != 0 || r.MediaAteDia != 0 || r.Indice != 0 {
		t.Errorf("dia 1º: %+v", r)
	}
	// último dia de um mês de 31 contra fevereiro (28 dias, entra inteiro)
	fixaRelogio(t, "2026-03-31")
	if r, _ = CalcRitmo(conn, Opcoes{Meses: 2}); r.DiasNoMes != 31 || r.MediaAteDia != 40000 || r.AteHoje != 40000 {
		t.Errorf("fim de mês: %+v", r)
	}
}

func TestCalcDiaApertado(t *testing.T) {
	conn := abreDB(t)
	if _, err := conn.Exec(`INSERT INTO contas (nome, saldo_inicial) VALUES ('Banco', 100000)`); err != nil {
		t.Fatal(err)
	}
	fixaRelogio(t, "2026-05-01")
	// fatura vence dia 3, antes do salário do dia 5: o saldo fica negativo
	pendente(t, conn, "pagar", "Fatura Roxinho", 150000, "2026-05-03")
	pendente(t, conn, "receber", "Salário", 500000, "2026-05-05")
	pendente(t, conn, "pagar", "Conta vencida", 10000, "2026-04-28") // conta hoje
	pendente(t, conn, "pagar", "Longe", 999999, "2026-07-30")       // fora da janela

	d, err := CalcDiaApertado(conn, Opcoes{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Saldo != 100000 || d.Serie[0] != 90000 || len(d.Serie) != horizonteApertado+1 {
		t.Fatalf("início = saldo %d, dia0 %d, %d dias", d.Saldo, d.Serie[0], len(d.Serie))
	}
	if d.PrimeiroNegativo != "2026-05-03" || d.MenorData != "2026-05-03" || d.Menor != -60000 {
		t.Fatalf("apertado = %s / %s %d", d.PrimeiroNegativo, d.MenorData, d.Menor)
	}
	if len(d.Saidas) != 2 || d.Saidas[0].Descricao != "Fatura Roxinho" {
		t.Errorf("saídas = %+v", d.Saidas)
	}
	if fim := d.Serie[len(d.Serie)-1]; fim != 440000 {
		t.Errorf("saldo no fim da janela = %d", fim)
	}
	p, err := CalcPainel(conn, Opcoes{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Alertas) == 0 || !p.Alertas[0].Grave || p.Alertas[0].Tela != "Dia Mais Apertado" {
		t.Errorf("painel deveria abrir com o saldo negativo: %+v", p.Alertas)
	}
}

func TestCalcFormiga(t *testing.T) {
	conn := abreDB(t)
	fixaRelogio(t, "2026-06-20")
	for i, d := range []string{"2026-04-02", "2026-04-15", "2026-05-03", "2026-05-20", "2026-06-10"} {
		desc := []string{"iFood", "IFOOD ", "ifood *1234", "iFood (2/3)", "iFood"}[i]
		gastoQuitado(t, conn, desc, "restaurante", 4000, d)
	}
	gastoQuitado(t, conn, "Uber", "transporte", 2000, "2026-06-01") // só 1 vez
	gastoQuitado(t, conn, "TV", "eletronicos", 300000, "2026-05-01") // acima do limite

	g, err := CalcFormiga(conn, Opcoes{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Itens) != 1 || g.Itens[0].Vezes != 5 || g.Itens[0].Descricao != "iFood" || g.Itens[0].Total != 20000 {
		t.Fatalf("formiga = %+v", g.Itens)
	}
	if g.Meses != 3 || g.Itens[0].MediaMes != 6666 || g.TotalDespesas != 322000 {
		t.Errorf("meses/média/total = %d/%d/%d", g.Meses, g.Itens[0].MediaMes, g.TotalDespesas)
	}
	// limite menor que R$ 40 exclui todos
	if g, _ = CalcFormiga(conn, Opcoes{}, 3000); len(g.Itens) != 0 {
		t.Errorf("limite R$ 30 ainda achou %+v", g.Itens)
	}
	for in, want := range map[string]string{"UBER *TRIP 98765": "uber trip", "Parcela (3/10)": "parcela", "  Padaria  Pão ": "padaria pão"} {
		if got := normalizaDescricao(in); got != want {
			t.Errorf("normalizaDescricao(%q) = %q, quer %q", in, got, want)
		}
	}
}

func TestPainelSemAlertas(t *testing.T) {
	conn := abreDB(t)
	fixaRelogio(t, "2026-06-20")
	out := capturaSaida(t, func() error { return AnalyticsHoje(conn) })
	if !strings.Contains(out, "Tudo dentro do padrão") {
		t.Errorf("banco vazio deveria dar painel limpo:\n%s", out)
	}
	for _, f := range []func() error{
		func() error { return AnalyticsRitmo(conn) },
		func() error { return AnalyticsDiaApertado(conn) },
		func() error { return AnalyticsFormiga(conn, "", "30,00") },
	} {
		silencia(t, f)
	}
	if err := AnalyticsFormiga(conn, "3", "-5"); err == nil {
		t.Error("limite negativo deveria falhar")
	}
}
