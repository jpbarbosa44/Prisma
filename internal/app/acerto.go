package app

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"prisma/internal/money"
)

// Acerto de contas, no estilo Splitwise, sobre os grupos que já existem:
//
//   - o que te devem: as receitas de reembolso pendentes das despesas que você
//     pagou com --recebe-pagamento (a parte das outras pessoas, dividida entre
//     elas por igual);
//   - o que você deve: as despesas do grupo que outra pessoa pagou
//     (--pago-por Ana), lançadas pendentes com a sua parte.
//
// `prisma acertar <grupo> <pessoa>` quita as duas pontas com aquela pessoa de
// uma vez. Num grupo de 3 ou mais, o reembolso de uma despesa é dividido: a
// parte de quem acertou sai do pendente e vira uma receita já recebida.

// SaldoPessoa é a conta entre você e uma pessoa de um grupo.
type SaldoPessoa struct {
	GrupoID   int64  `json:"grupoId"`
	Grupo     string `json:"grupo"`
	Pessoa    string `json:"pessoa"`
	TeDeve    int64  `json:"teDeve"`
	VoceDeve  int64  `json:"voceDeve"`
	Liquido   int64  `json:"liquido"` // > 0: a pessoa te deve; < 0: você deve
	Pendentes int    `json:"pendentes"`
}

// ehVoce reconhece o "você" na lista de pessoas do grupo ("Eu, Maria").
func ehVoce(nome string) bool {
	switch normaliza(strings.TrimSpace(nome)) {
	case "eu", "mim", "voce", "eu mesmo", "eu mesma":
		return true
	}
	return false
}

// pessoasDoGrupo devolve as pessoas do grupo e quem é você (o "Eu" da lista;
// sem ele, a primeira pessoa).
func pessoasDoGrupo(conn dbtx, grupo int64) (voce string, outras []string, err error) {
	rows, err := conn.Query(`SELECT nome FROM grupo_pessoas WHERE grupo_id = ? ORDER BY id`, grupo)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	var todas []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return "", nil, err
		}
		todas = append(todas, n)
	}
	if len(todas) == 0 {
		return "", nil, fmt.Errorf("o grupo #%d não tem pessoas", grupo)
	}
	iv := 0
	for i, n := range todas {
		if ehVoce(n) {
			iv = i
			break
		}
	}
	for i, n := range todas {
		if i != iv {
			outras = append(outras, n)
		}
	}
	return todas[iv], outras, rows.Err()
}

// pessoaDoGrupo acha a pessoa pelo nome (sem caixa nem acento); não pode ser você.
func pessoaDoGrupo(conn dbtx, grupo int64, nome string) (string, error) {
	_, outras, err := pessoasDoGrupo(conn, grupo)
	if err != nil {
		return "", err
	}
	for _, o := range outras {
		if normaliza(o) == normaliza(strings.TrimSpace(nome)) {
			return o, nil
		}
	}
	return "", fmt.Errorf("%q não está no grupo #%d (pessoas: %s)", nome, grupo, strings.Join(outras, ", "))
}

var reAcertado = regexp.MustCompile(`\[acertado: ([^\]]*)\]`)

// acertadosDe lê, da observação de um reembolso, quem já acertou a sua parte.
func acertadosDe(obs string) map[string]bool {
	out := map[string]bool{}
	if m := reAcertado.FindStringSubmatch(obs); m != nil {
		for _, n := range strings.Split(m[1], ",") {
			if n = strings.TrimSpace(n); n != "" {
				out[normaliza(n)] = true
			}
		}
	}
	return out
}

func marcaAcertado(obs, nome string) string {
	if m := reAcertado.FindStringSubmatch(obs); m != nil {
		return strings.Replace(obs, m[0], "[acertado: "+m[1]+", "+nome+"]", 1)
	}
	return strings.TrimSpace(obs + " [acertado: " + nome + "]")
}

type reembolsoPend struct {
	id         int64
	valor      int64
	desc, obs  string
	despesa    int64
	categoria  string
	vencimento string
	restantes  []string // quem ainda não acertou
}

// partes divide o valor entre as pessoas que restam; a última leva o resto.
func (r reembolsoPend) parte(pessoa string) int64 {
	n := int64(len(r.restantes))
	if n == 0 {
		return 0
	}
	cada := r.valor / n
	if r.restantes[len(r.restantes)-1] == pessoa {
		return r.valor - cada*(n-1)
	}
	return cada
}

func reembolsosPendentes(conn dbtx, grupo int64, outras []string) ([]reembolsoPend, error) {
	rows, err := conn.Query(`
		SELECT r.id, r.valor, r.descricao, r.observacao, r.reembolso_de, r.categoria, r.vencimento
		FROM lancamentos r JOIN lancamentos d ON d.id = r.reembolso_de
		WHERE d.grupo_id = ? AND r.tipo = 'receber' AND r.status = 'pendente'
		ORDER BY r.vencimento, r.id`, grupo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []reembolsoPend
	for rows.Next() {
		var r reembolsoPend
		if err := rows.Scan(&r.id, &r.valor, &r.desc, &r.obs, &r.despesa, &r.categoria, &r.vencimento); err != nil {
			return nil, err
		}
		ja := acertadosDe(r.obs)
		for _, o := range outras {
			if !ja[normaliza(o)] {
				r.restantes = append(r.restantes, o)
			}
		}
		if len(r.restantes) > 0 {
			out = append(out, r)
		}
	}
	return out, rows.Err()
}

// Acerto calcula o saldo com cada pessoa (grupo 0 = todos os grupos). Só
// devolve quem tem algo pendente.
func Acerto(conn *sql.DB, grupo int64) ([]SaldoPessoa, error) {
	q := `SELECT id, nome FROM grupos`
	var args []any
	if grupo > 0 {
		q += ` WHERE id = ?`
		args = append(args, grupo)
	}
	rows, err := conn.Query(q+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	type g struct {
		id   int64
		nome string
	}
	var gs []g
	for rows.Next() {
		var x g
		if err := rows.Scan(&x.id, &x.nome); err != nil {
			rows.Close()
			return nil, err
		}
		gs = append(gs, x)
	}
	rows.Close()
	out := []SaldoPessoa{}
	for _, gr := range gs {
		_, outras, err := pessoasDoGrupo(conn, gr.id)
		if err != nil || len(outras) == 0 {
			continue
		}
		por := map[string]*SaldoPessoa{}
		for _, o := range outras {
			por[o] = &SaldoPessoa{GrupoID: gr.id, Grupo: gr.nome, Pessoa: o}
		}
		rs, err := reembolsosPendentes(conn, gr.id, outras)
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			for _, p := range r.restantes {
				por[p].TeDeve += r.parte(p)
				por[p].Pendentes++
			}
		}
		dr, err := conn.Query(`SELECT pago_por, `+valEf("lancamentos")+` FROM lancamentos
			WHERE grupo_id = ? AND tipo = 'pagar' AND status = 'pendente' AND pago_por <> ''`, gr.id)
		if err != nil {
			return nil, err
		}
		for dr.Next() {
			var p string
			var v int64
			if err := dr.Scan(&p, &v); err != nil {
				dr.Close()
				return nil, err
			}
			for _, o := range outras {
				if normaliza(o) == normaliza(p) {
					por[o].VoceDeve += v
					por[o].Pendentes++
				}
			}
		}
		dr.Close()
		for _, o := range outras {
			s := por[o]
			s.Liquido = s.TeDeve - s.VoceDeve
			if s.Pendentes > 0 {
				out = append(out, *s)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return abs64(out[i].Liquido) > abs64(out[j].Liquido) })
	return out, nil
}

// Acertar quita, de uma vez, o que a pessoa te deve e o que você deve a ela
// no grupo. conta (0 = nenhuma) é onde o dinheiro entra/sai.
func Acertar(conn *sql.DB, grupo int64, pessoa string, conta int64, data string) (recebido, pago int64, err error) {
	nome, err := pessoaDoGrupo(conn, grupo, pessoa)
	if err != nil {
		return 0, 0, err
	}
	_, outras, err := pessoasDoGrupo(conn, grupo)
	if err != nil {
		return 0, 0, err
	}
	var contaArg any
	if conta > 0 {
		if err := existe(conn, "contas", conta); err != nil {
			return 0, 0, err
		}
		contaArg = conta
	}
	tx, err := conn.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	rs, err := reembolsosPendentes(tx, grupo, outras)
	if err != nil {
		return 0, 0, err
	}
	for _, r := range rs {
		tem := false
		for _, p := range r.restantes {
			tem = tem || p == nome
		}
		if !tem {
			continue
		}
		parte := r.parte(nome)
		obs := marcaAcertado(r.obs, nome)
		if len(r.restantes) == 1 {
			// a última pessoa: o reembolso inteiro (o que sobrou) está recebido
			if _, err := tx.Exec(`UPDATE lancamentos SET status = 'quitado', quitado_em = ?, conta_id = COALESCE(?, conta_id), observacao = ? WHERE id = ?`,
				data, contaArg, obs, r.id); err != nil {
				return 0, 0, err
			}
		} else {
			if _, err := tx.Exec(`UPDATE lancamentos SET valor = valor - ?, observacao = ? WHERE id = ?`, parte, obs, r.id); err != nil {
				return 0, 0, err
			}
			if _, err := tx.Exec(`INSERT INTO lancamentos (tipo, descricao, valor, categoria, vencimento, status, quitado_em, conta_id, reembolso_de)
				VALUES ('receber', ?, ?, ?, ?, 'quitado', ?, ?, ?)`,
				r.desc+" ("+nome+")", parte, r.categoria, r.vencimento, data, contaArg, r.despesa); err != nil {
				return 0, 0, err
			}
		}
		recebido += parte
	}
	dr, err := tx.Query(`SELECT id, pago_por, `+valEf("lancamentos")+` FROM lancamentos
		WHERE grupo_id = ? AND tipo = 'pagar' AND status = 'pendente' AND pago_por <> ''`, grupo)
	if err != nil {
		return 0, 0, err
	}
	var ids []int64
	for dr.Next() {
		var id, v int64
		var p string
		if err := dr.Scan(&id, &p, &v); err != nil {
			dr.Close()
			return 0, 0, err
		}
		if normaliza(p) == normaliza(nome) {
			ids = append(ids, id)
			pago += v
		}
	}
	dr.Close()
	for _, id := range ids {
		if _, err := tx.Exec(`UPDATE lancamentos SET status = 'quitado', quitado_em = ?, conta_id = COALESCE(?, conta_id) WHERE id = ?`,
			data, contaArg, id); err != nil {
			return 0, 0, err
		}
	}
	if recebido == 0 && pago == 0 {
		return 0, 0, fmt.Errorf("nada pendente com %s no grupo #%d", nome, grupo)
	}
	return recebido, pago, tx.Commit()
}

// AcertoCmd trata `prisma acerto [grupo]`.
func AcertoCmd(conn *sql.DB, args []string) error {
	var grupo int64
	if len(args) > 0 {
		var err error
		if grupo, err = strconv.ParseInt(args[0], 10, 64); err != nil {
			return fmt.Errorf("grupo inválido: %q", args[0])
		}
	}
	ss, err := Acerto(conn, grupo)
	if err != nil {
		return err
	}
	if len(ss) == 0 {
		fmt.Println("Ninguém deve nada a ninguém. ✓")
		fmt.Println("(despesas com --recebe-pagamento viram o que te devem; com --pago-por Ana, o que você deve)")
		return nil
	}
	w := novaTabela()
	fmt.Fprintln(w, "GRUPO\tPESSOA\tTE DEVE\tVOCÊ DEVE\tSALDO")
	for _, s := range ss {
		saldo := "quites"
		switch {
		case s.Liquido > 0:
			saldo = s.Pessoa + " te deve " + money.Format(s.Liquido)
		case s.Liquido < 0:
			saldo = "você deve " + money.Format(-s.Liquido) + " a " + s.Pessoa
		}
		fmt.Fprintf(w, "#%d %s\t%s\t%s\t%s\t%s\n", s.GrupoID, s.Grupo, s.Pessoa, money.Format(s.TeDeve), money.Format(s.VoceDeve), saldo)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Println("\nAcerte com: prisma acertar <grupo> <pessoa> [--conta N]")
	return nil
}

// AcertarCmd trata `prisma acertar <grupo> <pessoa> [--conta N] [--data]`.
func AcertarCmd(conn *sql.DB, args []string) error {
	if len(args) < 2 {
		return errors.New("uso: prisma acertar <grupo> <pessoa> [--conta N] [--data DD/MM/AAAA]")
	}
	grupo, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("grupo inválido: %q", args[0])
	}
	fs := flag.NewFlagSet("acertar", flag.ContinueOnError)
	conta := fs.Int64("conta", 0, "conta onde entra/sai o dinheiro")
	data := fs.String("data", "hoje", "data do acerto")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	d, err := parseData(*data)
	if err != nil {
		return err
	}
	nome, err := pessoaDoGrupo(conn, grupo, args[1])
	if err != nil {
		return err
	}
	rec, pag, err := Acertar(conn, grupo, nome, *conta, d)
	if err != nil {
		return err
	}
	fmt.Printf("Acerto com %s em %s: recebido %s, pago %s.\n", nome, dataBR(d), money.Format(rec), money.Format(pag))
	switch dif := rec - pag; {
	case dif > 0:
		fmt.Printf("%s te passou %s.\n", nome, money.Format(dif))
	case dif < 0:
		fmt.Printf("Você passou %s a %s.\n", money.Format(-dif), nome)
	default:
		fmt.Println("Ficou elas por elas.")
	}
	return nil
}
