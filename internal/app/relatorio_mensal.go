package app

import (
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// O fechamento de um mês, para o relatório imprimível (PDF): os mesmos
// critérios do `prisma relatorio` — só o que foi quitado, na competência
// (data da compra no cartão, data do pagamento fora dele), já com a sua parte
// nas despesas divididas.

// CategoriaMes é o gasto de uma categoria no mês e a média dos 3 anteriores.
type CategoriaMes struct {
	Rotulo string `json:"rotulo"`
	Valor  int64  `json:"valor"`
	Media  int64  `json:"media"` // média mensal dos 3 meses anteriores
}

// GastoMes é um dos maiores gastos do mês.
type GastoMes struct {
	Data      string `json:"data"`
	Descricao string `json:"descricao"`
	Categoria string `json:"categoria"`
	Valor     int64  `json:"valor"`
}

// DadosRelatorio é o fechamento de um mês.
type DadosRelatorio struct {
	Mes         string         `json:"mes"` // AAAA-MM
	Gerado      string         `json:"gerado"`
	Receitas    int64          `json:"receitas"`
	Despesas    int64          `json:"despesas"`
	Categorias  []CategoriaMes `json:"categorias"`
	Maiores     []GastoMes     `json:"maiores"`
	Mensal      []TrioMes      `json:"mensal"` // os 6 meses até este
	PendPagar   int64          `json:"pendPagar"`
	PendReceber int64          `json:"pendReceber"`
	Saldo       int64          `json:"saldo"` // saldo total na geração
	Metas       []Meta         `json:"metas"`
}

// Resultado é receitas − despesas.
func (d DadosRelatorio) Resultado() int64 { return d.Receitas - d.Despesas }

// Poupanca é a fração da receita que sobrou, em %, ou -1 sem receita.
func (d DadosRelatorio) Poupanca() int {
	if d.Receitas <= 0 {
		return -1
	}
	return int((d.Receitas - d.Despesas) * 100 / d.Receitas)
}

// RelatorioMensal monta o fechamento do mês (AAAA-MM; "" = o atual).
func RelatorioMensal(conn *sql.DB, mes string) (DadosRelatorio, error) {
	if mes == "" {
		mes = time.Now().Format("2006-01")
	}
	ini, err := time.Parse("2006-01", mes)
	if err != nil {
		return DadosRelatorio{}, fmt.Errorf("mês inválido %q (use AAAA-MM)", mes)
	}
	d := DadosRelatorio{Mes: mes, Gerado: time.Now().Format("2006-01-02 15:04"), Categorias: []CategoriaMes{}, Maiores: []GastoMes{}}
	de, ate := ini.Format("2006-01-02"), ini.AddDate(0, 1, -1).Format("2006-01-02")

	if err := conn.QueryRow(`
		SELECT COALESCE(SUM(CASE tipo WHEN 'receber' THEN `+valEf("lancamentos")+` ELSE 0 END), 0),
		       COALESCE(SUM(CASE tipo WHEN 'pagar' THEN `+valEf("lancamentos")+` ELSE 0 END), 0)
		FROM lancamentos WHERE status = 'quitado' AND `+competencia+` >= ? AND `+competencia+` <= ?`,
		de, ate).Scan(&d.Receitas, &d.Despesas); err != nil {
		return d, err
	}

	cats, err := GastosPorCategoria(conn, de, ate)
	if err != nil {
		return d, err
	}
	// média dos 3 meses anteriores, por categoria (comparar o mês com o normal)
	antDe, antAte := ini.AddDate(0, -3, 0).Format("2006-01-02"), ini.AddDate(0, 0, -1).Format("2006-01-02")
	anteriores, err := GastosPorCategoria(conn, antDe, antAte)
	if err != nil {
		return d, err
	}
	media := map[string]int64{}
	for _, c := range anteriores {
		media[c.Rotulo] = c.Valor / 3
	}
	for _, c := range cats {
		d.Categorias = append(d.Categorias, CategoriaMes{Rotulo: c.Rotulo, Valor: c.Valor, Media: media[c.Rotulo]})
	}
	sort.SliceStable(d.Categorias, func(i, j int) bool { return d.Categorias[i].Valor > d.Categorias[j].Valor })

	rows, err := conn.Query(`
		SELECT `+competencia+`, descricao, categoria, `+valEf("lancamentos")+` AS v FROM lancamentos
		WHERE tipo = 'pagar' AND status = 'quitado' AND `+competencia+` >= ? AND `+competencia+` <= ?
		ORDER BY v DESC, id LIMIT 7`, de, ate)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var g GastoMes
		if err := rows.Scan(&g.Data, &g.Descricao, &g.Categoria, &g.Valor); err != nil {
			rows.Close()
			return d, err
		}
		d.Maiores = append(d.Maiores, g)
	}
	rows.Close()

	if d.Mensal, err = receitaDespesaAte(conn, ini, 6); err != nil {
		return d, err
	}
	if d.PendPagar, d.PendReceber, err = pendentesDoMes(conn, mes); err != nil {
		return d, err
	}
	if d.Saldo, err = saldoTotal(conn); err != nil {
		return d, err
	}
	if d.Metas, err = ListaMetas(conn, false); err != nil {
		return d, err
	}
	return d, nil
}

// receitaDespesaAte devolve receitas e despesas quitadas dos n meses que
// terminam no mês de fim (inclusive), na competência.
func receitaDespesaAte(conn *sql.DB, fim time.Time, n int) ([]TrioMes, error) {
	out := make([]TrioMes, 0, n)
	for i := n - 1; i >= 0; i-- {
		m := fim.AddDate(0, -i, 0)
		de, ate := m.Format("2006-01-02"), m.AddDate(0, 1, -1).Format("2006-01-02")
		t := TrioMes{Mes: m.Format("2006-01")}
		if err := conn.QueryRow(`
			SELECT COALESCE(SUM(CASE tipo WHEN 'receber' THEN `+valEf("lancamentos")+` ELSE 0 END), 0),
			       COALESCE(SUM(CASE tipo WHEN 'pagar' THEN `+valEf("lancamentos")+` ELSE 0 END), 0)
			FROM lancamentos WHERE status = 'quitado' AND `+competencia+` >= ? AND `+competencia+` <= ?`,
			de, ate).Scan(&t.Rec, &t.Desp); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}
