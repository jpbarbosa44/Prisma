package app

import (
	"database/sql"
	"time"
)

// Situações de um vencimento, do mais urgente ao menos.
const (
	SituacaoAtrasada = "atrasada"
	SituacaoHoje     = "hoje"
	SituacaoProxima  = "proxima"
)

// Aviso é uma conta pendente que pede atenção: atrasada, vencendo hoje ou nos
// próximos dias. Compras no cartão viram UM aviso por fatura (Tipo "fatura"),
// já que se pagam juntas — CartaoID e Ref identificam a fatura.
type Aviso struct {
	ID         int64  `json:"id"` // 0 nas faturas
	Tipo       string `json:"tipo"`
	Descricao  string `json:"descricao"`
	Valor      int64  `json:"valor"` // centavos, a parte efetiva (valEf)
	Vencimento string `json:"vencimento"`
	Situacao   string `json:"situacao"`
	CartaoID   int64  `json:"cartaoId,omitempty"`
	Ref        string `json:"ref,omitempty"`
}

// Vencimentos lista o que está pendente e vence até `dias` dias depois de
// `hoje` (inclusive), mais tudo o que já venceu — do mais antigo ao mais novo.
// É a base dos lembretes: a tela Início, as notificações do app e o bot.
func Vencimentos(conn *sql.DB, hoje time.Time, dias int) ([]Aviso, error) {
	h := hoje.Format("2006-01-02")
	limite := hoje.AddDate(0, 0, dias).Format("2006-01-02")
	rows, err := conn.Query(`
		SELECT l.id, l.tipo, l.descricao, `+valEf("l")+`, l.vencimento, COALESCE(l.cartao_id, 0), COALESCE(c.nome, '')
		FROM lancamentos l LEFT JOIN cartoes c ON c.id = l.cartao_id
		WHERE l.status = 'pendente' AND l.vencimento <= ?
		ORDER BY l.vencimento, l.id`, limite)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Aviso
	faturas := map[string]int{} // cartão+ref → índice em out
	for rows.Next() {
		var a Aviso
		var cartao string
		if err := rows.Scan(&a.ID, &a.Tipo, &a.Descricao, &a.Valor, &a.Vencimento, &a.CartaoID, &cartao); err != nil {
			return nil, err
		}
		switch {
		case a.Vencimento < h:
			a.Situacao = SituacaoAtrasada
		case a.Vencimento == h:
			a.Situacao = SituacaoHoje
		default:
			a.Situacao = SituacaoProxima
		}
		if a.CartaoID != 0 && a.Tipo == "pagar" {
			a.Ref = a.Vencimento[:7]
			chave := cartao + "|" + a.Vencimento
			if i, ok := faturas[chave]; ok {
				out[i].Valor += a.Valor
				continue
			}
			a.ID, a.Tipo, a.Descricao = 0, "fatura", "Fatura "+cartao
			faturas[chave] = len(out)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
