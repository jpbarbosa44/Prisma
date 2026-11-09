package app

import (
	"database/sql"
	"fmt"
	"sort"
	"time"

	"prisma/internal/money"
)

// Insights são os avisos automáticos que os apps de finanças dão sem ninguém
// pedir: "delivery 45% acima do normal", "a Netflix ficou mais cara",
// "o saldo fica negativo dia 25". Só leem o banco; aparecem na tela Início,
// em `prisma insights` e (os de gravidade alta) nos lembretes do app e do bot.

// Gravidades, da mais urgente à mais leve.
const (
	GravAlta  = "alta"
	GravMedia = "media"
	GravBaixa = "baixa"
)

// Insight é um aviso.
type Insight struct {
	Tipo      string `json:"tipo"` // categoria | orcamento | assinatura | incomum | saldo | fatura
	Gravidade string `json:"gravidade"`
	Texto     string `json:"texto"`
	Tela      string `json:"tela"` // tela que mostra o assunto
}

var pesoGrav = map[string]int{GravAlta: 0, GravMedia: 1, GravBaixa: 2}

// Insights calcula os avisos de hoje, dos mais graves para os mais leves.
func Insights(conn *sql.DB, hoje time.Time) ([]Insight, error) {
	out := []Insight{}
	for _, regra := range []func(*sql.DB, time.Time) ([]Insight, error){
		insightSaldo, insightOrcamento, insightCategorias, insightAssinaturas, insightIncomuns, insightFaturas, insightAcertos,
	} {
		is, err := regra(conn, hoje)
		if err != nil {
			return nil, err
		}
		out = append(out, is...)
	}
	sort.SliceStable(out, func(i, j int) bool { return pesoGrav[out[i].Gravidade] < pesoGrav[out[j].Gravidade] })
	return out, nil
}

// insightSaldo: o saldo previsto fica negativo em algum dia dos próximos 30.
func insightSaldo(conn *sql.DB, hoje time.Time) ([]Insight, error) {
	limite := hoje.AddDate(0, 0, 30).Format("2006-01-02")
	for _, mes := range []string{hoje.Format("2006-01"), hoje.AddDate(0, 1, 0).Format("2006-01")} {
		c, err := Calendario(conn, mes)
		if err != nil {
			return nil, err
		}
		for _, d := range c.Dias {
			if d.TemSaldo && d.Data <= limite && d.Saldo < 0 {
				return []Insight{{Tipo: "saldo", Gravidade: GravAlta, Tela: "Calendário",
					Texto: fmt.Sprintf("O saldo previsto fica negativo em %s (%s). Antecipe um recebimento ou adie uma conta.",
						dataBR(d.Data), money.Format(d.Saldo))}}, nil
			}
		}
	}
	return nil, nil
}

// insightOrcamento: planos em 80% ou estourados.
func insightOrcamento(conn *sql.DB, hoje time.Time) ([]Insight, error) {
	us, err := AlertasOrcamento(conn, hoje)
	if err != nil {
		return nil, err
	}
	rot := map[string]string{"mes": "do mês", "semana": "da semana"}
	var out []Insight
	for _, u := range us {
		i := Insight{Tipo: "orcamento", Tela: "Planejamento"}
		if u.Gasto > u.Limite {
			i.Gravidade = GravAlta
			i.Texto = fmt.Sprintf("Plano de %s %s estourado: %s de %s.", u.Categoria, rot[u.Periodo], money.Format(u.Gasto), money.Format(u.Limite))
		} else {
			i.Gravidade = GravMedia
			i.Texto = fmt.Sprintf("Plano de %s %s em %d%%: restam %s.", u.Categoria, rot[u.Periodo], u.Pct(), money.Format(u.Limite-u.Gasto))
		}
		out = append(out, i)
	}
	return out, nil
}

// insightCategorias: categoria que, no mês, já passou 30% da média dos 3
// meses anteriores — ou que, no ritmo atual, vai passar 50%.
func insightCategorias(conn *sql.DB, hoje time.Time) ([]Insight, error) {
	ini := time.Date(hoje.Year(), hoje.Month(), 1, 0, 0, 0, 0, hoje.Location())
	atual, err := GastosPorCategoria(conn, ini.Format("2006-01-02"), hoje.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	ant, err := GastosPorCategoria(conn, ini.AddDate(0, -3, 0).Format("2006-01-02"), ini.AddDate(0, 0, -1).Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	media := map[string]int64{}
	for _, c := range ant {
		media[c.Rotulo] = c.Valor / 3
	}
	diasMes := ini.AddDate(0, 1, -1).Day()
	var out []Insight
	for _, c := range atual {
		m := media[c.Rotulo]
		if m < 5000 || c.Valor < 5000 { // menos de R$ 50: ruído
			continue
		}
		switch {
		case c.Valor > m*13/10:
			out = append(out, Insight{Tipo: "categoria", Gravidade: GravMedia, Tela: "Relatório",
				Texto: fmt.Sprintf("%s: %s no mês, %d%% acima da média dos 3 meses anteriores (%s).",
					c.Rotulo, money.Format(c.Valor), (c.Valor-m)*100/m, money.Format(m))})
		case hoje.Day() >= 10 && c.Valor*int64(diasMes)/int64(hoje.Day()) > m*3/2:
			proj := c.Valor * int64(diasMes) / int64(hoje.Day())
			out = append(out, Insight{Tipo: "categoria", Gravidade: GravBaixa, Tela: "Relatório",
				Texto: fmt.Sprintf("%s: no ritmo atual, o mês fecha em %s (a média é %s).",
					c.Rotulo, money.Format(proj), money.Format(m))})
		}
	}
	return out, nil
}

// insightAssinaturas: a cobrança mais recente de uma assinatura veio mais cara
// que a anterior (nos últimos 45 dias).
func insightAssinaturas(conn *sql.DB, hoje time.Time) ([]Insight, error) {
	rows, err := conn.Query(`
		SELECT r.descricao, l.valor, l.vencimento FROM recorrencias r
		JOIN lancamentos l ON l.recorrencia_id = r.id
		WHERE r.assinatura = 1 AND l.vencimento <= ?
		ORDER BY r.id, COALESCE(l.data_compra, l.vencimento) DESC`, hoje.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type cobranca struct {
		valor int64
		data  string
	}
	por := map[string][]cobranca{}
	var ordem []string
	for rows.Next() {
		var desc string
		var c cobranca
		if err := rows.Scan(&desc, &c.valor, &c.data); err != nil {
			return nil, err
		}
		if _, ok := por[desc]; !ok {
			ordem = append(ordem, desc)
		}
		if len(por[desc]) < 2 {
			por[desc] = append(por[desc], c)
		}
	}
	corte := hoje.AddDate(0, 0, -45).Format("2006-01-02")
	var out []Insight
	for _, d := range ordem {
		cs := por[d]
		if len(cs) == 2 && cs[0].valor > cs[1].valor && cs[0].data >= corte {
			out = append(out, Insight{Tipo: "assinatura", Gravidade: GravMedia, Tela: "Assinaturas",
				Texto: fmt.Sprintf("%s ficou mais cara: de %s para %s (+%d%%).", d,
					money.Format(cs[1].valor), money.Format(cs[0].valor), (cs[0].valor-cs[1].valor)*100/cs[1].valor)})
		}
	}
	return out, rows.Err()
}

// insightIncomuns: gasto dos últimos 7 dias maior que 3× a mediana da
// categoria nos 6 meses anteriores (com pelo menos 4 gastos de base).
func insightIncomuns(conn *sql.DB, hoje time.Time) ([]Insight, error) {
	de := hoje.AddDate(0, 0, -7).Format("2006-01-02")
	h := hoje.Format("2006-01-02")
	rows, err := conn.Query(`
		SELECT id, descricao, categoria, `+valEf("lancamentos")+`, COALESCE(data_compra, vencimento) AS d
		FROM lancamentos WHERE tipo = 'pagar' AND d >= ? AND d <= ? AND categoria <> 'reembolso'
		ORDER BY d DESC`, de, h)
	if err != nil {
		return nil, err
	}
	type gasto struct {
		id        int64
		desc, cat string
		valor     int64
		data      string
	}
	var recentes []gasto
	for rows.Next() {
		var g gasto
		if err := rows.Scan(&g.id, &g.desc, &g.cat, &g.valor, &g.data); err != nil {
			rows.Close()
			return nil, err
		}
		recentes = append(recentes, g)
	}
	rows.Close()
	base := hoje.AddDate(0, -6, 0).Format("2006-01-02")
	var out []Insight
	for _, g := range recentes {
		if g.valor < 10000 { // menos de R$ 100 não chama atenção
			continue
		}
		vs, err := conn.Query(`SELECT `+valEf("lancamentos")+` FROM lancamentos
			WHERE tipo = 'pagar' AND categoria = ? AND COALESCE(data_compra, vencimento) >= ? AND COALESCE(data_compra, vencimento) < ?`,
			g.cat, base, de)
		if err != nil {
			return nil, err
		}
		var serie []int64
		for vs.Next() {
			var v int64
			if err := vs.Scan(&v); err != nil {
				vs.Close()
				return nil, err
			}
			serie = append(serie, v)
		}
		vs.Close()
		if len(serie) < 4 {
			continue
		}
		if med := mediana(serie); med > 0 && g.valor > 3*med {
			out = append(out, Insight{Tipo: "incomum", Gravidade: GravBaixa, Tela: "Pagar/Receber",
				Texto: fmt.Sprintf("Gasto fora do comum em %s: %s (#%d, %s), %dx o normal da categoria.",
					g.cat, truncar(g.desc, 30), g.id, money.Format(g.valor), g.valor/med)})
		}
	}
	return out, nil
}

// insightFaturas: a próxima fatura de um cartão está 30% acima da média das 3
// anteriores.
func insightFaturas(conn *sql.DB, hoje time.Time) ([]Insight, error) {
	rows, err := conn.Query(`
		SELECT k.nome, l.cartao_id, l.vencimento, SUM(` + valEf("l") + `)
		FROM lancamentos l JOIN cartoes k ON k.id = l.cartao_id
		WHERE l.tipo = 'pagar' GROUP BY l.cartao_id, l.vencimento ORDER BY l.cartao_id, l.vencimento`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type fat struct {
		venc  string
		total int64
	}
	por := map[int64][]fat{}
	nomes := map[int64]string{}
	var ids []int64
	for rows.Next() {
		var nome string
		var id int64
		var f fat
		if err := rows.Scan(&nome, &id, &f.venc, &f.total); err != nil {
			return nil, err
		}
		if _, ok := nomes[id]; !ok {
			ids = append(ids, id)
		}
		nomes[id] = nome
		por[id] = append(por[id], f)
	}
	h := hoje.Format("2006-01-02")
	var out []Insight
	for _, id := range ids {
		fs := por[id]
		prox := -1
		for i, f := range fs {
			if f.venc >= h {
				prox = i
				break
			}
		}
		if prox < 3 { // sem próxima fatura ou sem 3 anteriores para comparar
			continue
		}
		media := (fs[prox-1].total + fs[prox-2].total + fs[prox-3].total) / 3
		if media > 0 && fs[prox].total > media*13/10 && fs[prox].total-media >= 5000 {
			out = append(out, Insight{Tipo: "fatura", Gravidade: GravMedia, Tela: "Cartões",
				Texto: fmt.Sprintf("Fatura %s de %s: %s, %d%% acima da média das 3 anteriores (%s).",
					nomes[id], dataBR(fs[prox].venc), money.Format(fs[prox].total), (fs[prox].total-media)*100/media, money.Format(media))})
		}
	}
	return out, rows.Err()
}

// AtencaoDoDia junta, para os lembretes diários, os alertas de orçamento ainda
// não avisados neste canal e os insights graves (o saldo que fica negativo):
// uma linha curta para cada.
func AtencaoDoDia(conn *sql.DB, hoje time.Time, canal string) ([]string, error) {
	var out []string
	novos, err := NovosAlertasOrcamento(conn, hoje, canal)
	if err != nil {
		return nil, err
	}
	rot := map[string]string{"mes": "do mês", "semana": "da semana"}
	for _, u := range novos {
		if u.Gasto > u.Limite {
			out = append(out, fmt.Sprintf("Plano de %s %s estourado: %s de %s", u.Categoria, rot[u.Periodo], money.Format(u.Gasto), money.Format(u.Limite)))
		} else {
			out = append(out, fmt.Sprintf("Plano de %s %s em %d%%", u.Categoria, rot[u.Periodo], u.Pct()))
		}
	}
	saldo, err := insightSaldo(conn, hoje)
	if err != nil {
		return nil, err
	}
	for _, i := range saldo {
		out = append(out, i.Texto)
	}
	return out, nil
}

// insightAcertos: quem te deve (ou a quem você deve) nos grupos.
func insightAcertos(conn *sql.DB, _ time.Time) ([]Insight, error) {
	ss, err := Acerto(conn, 0)
	if err != nil {
		return nil, err
	}
	var out []Insight
	for _, s := range ss {
		switch {
		case s.Liquido >= 100:
			out = append(out, Insight{Tipo: "acerto", Gravidade: GravBaixa, Tela: "Grupos",
				Texto: fmt.Sprintf("%s te deve %s (%s).", s.Pessoa, money.Format(s.Liquido), s.Grupo)})
		case s.Liquido <= -100:
			out = append(out, Insight{Tipo: "acerto", Gravidade: GravBaixa, Tela: "Grupos",
				Texto: fmt.Sprintf("Você deve %s a %s (%s).", money.Format(-s.Liquido), s.Pessoa, s.Grupo)})
		}
	}
	return out, nil
}

// InsightsCmd trata `prisma insights`.
func InsightsCmd(conn *sql.DB, _ []string) error {
	is, err := Insights(conn, relogio())
	if err != nil {
		return err
	}
	if len(is) == 0 {
		fmt.Println("Nada fora do comum. ✓")
		return nil
	}
	icone := map[string]string{GravAlta: "⚠", GravMedia: "•", GravBaixa: "·"}
	for _, i := range is {
		fmt.Printf("%s %s\n", icone[i.Gravidade], i.Texto)
	}
	return nil
}
