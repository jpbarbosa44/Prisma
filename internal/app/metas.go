package app

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"prisma/internal/money"
)

// Metas (cofrinhos): um valor a juntar, com prazo opcional, e os aportes que
// o aproximam. O aporte é um registro — não mexe no saldo das contas; para
// separar o dinheiro de fato, transfira para uma carteira ("Viagem").

// Meta é uma meta com o quanto já foi guardado.
type Meta struct {
	ID       int64  `json:"id"`
	Nome     string `json:"nome"`
	Alvo     int64  `json:"alvo"`     // centavos
	Guardado int64  `json:"guardado"` // soma dos aportes − retiradas
	Prazo    string `json:"prazo"`    // AAAA-MM-DD ("" = sem prazo)
	Status   string `json:"status"`   // ativa | concluida
}

// Falta é quanto ainda falta juntar (0 se já chegou).
func (m Meta) Falta() int64 {
	if f := m.Alvo - m.Guardado; f > 0 {
		return f
	}
	return 0
}

// Percentual é o progresso de 0 a 100.
func (m Meta) Percentual() int {
	if m.Alvo <= 0 {
		return 0
	}
	p := int(m.Guardado * 100 / m.Alvo)
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

// MesesRestantes conta os meses até o prazo, incluindo o atual (mínimo 1);
// 0 = sem prazo.
func (m Meta) MesesRestantes(hoje time.Time) int {
	if m.Prazo == "" {
		return 0
	}
	p, err := time.Parse("2006-01-02", m.Prazo)
	if err != nil {
		return 0
	}
	n := (p.Year()-hoje.Year())*12 + int(p.Month()-hoje.Month()) + 1
	if n < 1 {
		n = 1
	}
	return n
}

// PorMes é o aporte mensal que fecha a meta no prazo (0 sem prazo ou já
// alcançada), arredondado para cima.
func (m Meta) PorMes(hoje time.Time) int64 {
	n := int64(m.MesesRestantes(hoje))
	if n == 0 || m.Falta() == 0 {
		return 0
	}
	return (m.Falta() + n - 1) / n
}

// ListaMetas devolve as metas (só as ativas, ou todas), as de prazo mais
// próximo primeiro.
func ListaMetas(conn *sql.DB, todas bool) ([]Meta, error) {
	filtro := ` WHERE m.status = 'ativa'`
	if todas {
		filtro = ""
	}
	rows, err := conn.Query(`
		SELECT m.id, m.nome, m.alvo, COALESCE((SELECT SUM(valor) FROM metas_aportes a WHERE a.meta_id = m.id), 0), m.prazo, m.status
		FROM metas m` + filtro + `
		ORDER BY m.status, CASE m.prazo WHEN '' THEN 1 ELSE 0 END, m.prazo, m.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Meta{}
	for rows.Next() {
		var m Meta
		if err := rows.Scan(&m.ID, &m.Nome, &m.Alvo, &m.Guardado, &m.Prazo, &m.Status); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// parsePrazo aceita AAAA-MM (vira o último dia do mês), DD/MM/AAAA, AAAA-MM-DD
// e "" (sem prazo).
func parsePrazo(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return "", nil
	}
	if t, err := time.Parse("2006-01", s); err == nil {
		return t.AddDate(0, 1, -1).Format("2006-01-02"), nil
	}
	if t, err := time.Parse("01/2006", s); err == nil {
		return t.AddDate(0, 1, -1).Format("2006-01-02"), nil
	}
	return parseData(s)
}

// MetaCmd trata `prisma meta add|listar|aportar|retirar|editar|concluir|remover`.
func MetaCmd(conn *sql.DB, args []string) error {
	if len(args) == 0 {
		args = []string{"listar"}
	}
	switch args[0] {
	case "add", "adicionar", "nova":
		return metaAdd(conn, args[1:])
	case "listar", "ls":
		return metaListar(conn, len(args) > 1 && args[1] == "--todas")
	case "aportar", "guardar":
		return metaAporte(conn, args[1:], 1)
	case "retirar":
		return metaAporte(conn, args[1:], -1)
	case "editar":
		return metaEditar(conn, args[1:])
	case "concluir":
		return metaStatus(conn, args[1:], "concluida")
	case "reabrir":
		return metaStatus(conn, args[1:], "ativa")
	case "remover", "rm":
		return metaRemover(conn, args[1:])
	}
	return fmt.Errorf("subcomando inválido %q (use: add, listar, aportar, retirar, editar, concluir, reabrir, remover)", args[0])
}

func metaAdd(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("meta add", flag.ContinueOnError)
	nome := fs.String("nome", "", "nome da meta (ex.: Viagem)")
	valor := fs.String("valor", "", "quanto juntar")
	prazo := fs.String("prazo", "", "até quando (AAAA-MM ou DD/MM/AAAA; opcional)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*nome) == "" || *valor == "" {
		return errors.New(`uso: prisma meta add --nome "Viagem" --valor 5000 [--prazo 2027-07]`)
	}
	alvo, err := money.Parse(*valor)
	if err != nil || alvo <= 0 {
		return fmt.Errorf("valor inválido: %q", *valor)
	}
	p, err := parsePrazo(*prazo)
	if err != nil {
		return err
	}
	res, err := conn.Exec(`INSERT INTO metas (nome, alvo, prazo) VALUES (?, ?, ?)`, strings.TrimSpace(*nome), alvo, p)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	m := Meta{Alvo: alvo, Prazo: p}
	fmt.Printf("Meta #%d criada: %s — %s", id, strings.TrimSpace(*nome), money.Format(alvo))
	if p != "" {
		fmt.Printf(" até %s (%s/mês)", dataBR(p), money.Format(m.PorMes(time.Now())))
	}
	fmt.Println(".")
	return nil
}

// barraProgresso desenha "█████░░░░░" com 20 posições.
func barraProgresso(pct int) string {
	cheio := pct / 5
	return strings.Repeat("█", cheio) + strings.Repeat("░", 20-cheio)
}

func metaListar(conn *sql.DB, todas bool) error {
	metas, err := ListaMetas(conn, todas)
	if err != nil {
		return err
	}
	if len(metas) == 0 {
		fmt.Println(`Nenhuma meta. Crie com: prisma meta add --nome "Viagem" --valor 5000 --prazo 2027-07`)
		return nil
	}
	hoje := time.Now()
	w := novaTabela()
	fmt.Fprintln(w, "ID\tMETA\tPROGRESSO\tGUARDADO\tALVO\tPRAZO\tPOR MÊS")
	for _, m := range metas {
		prazo, porMes := "-", "-"
		if m.Prazo != "" {
			prazo = dataBR(m.Prazo)
		}
		switch {
		case m.Status == "concluida":
			porMes = "concluída ✓"
		case m.Falta() == 0:
			porMes = "alcançada ✓"
		case m.Prazo != "":
			porMes = money.Format(m.PorMes(hoje))
		}
		fmt.Fprintf(w, "%d\t%s\t%s %3d%%\t%s\t%s\t%s\t%s\n", m.ID, m.Nome, barraProgresso(m.Percentual()), m.Percentual(),
			money.Format(m.Guardado), money.Format(m.Alvo), prazo, porMes)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	var total int64
	for _, m := range metas {
		if m.Status == "ativa" {
			total += m.PorMes(hoje)
		}
	}
	if total > 0 {
		fmt.Printf("\nPara cumprir todas no prazo: %s por mês.\n", money.Format(total))
	}
	return nil
}

func idMeta(conn *sql.DB, s string) (int64, string, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimSpace(s), "#"), 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("id de meta inválido: %q", s)
	}
	var nome string
	if err := conn.QueryRow(`SELECT nome FROM metas WHERE id = ?`, id).Scan(&nome); err != nil {
		return 0, "", fmt.Errorf("meta #%d não encontrada", id)
	}
	return id, nome, nil
}

// metaAporte registra um aporte (sinal 1) ou uma retirada (-1).
func metaAporte(conn *sql.DB, args []string, sinal int64) error {
	if len(args) == 0 {
		return errors.New("uso: prisma meta aportar|retirar <id> --valor N [--data] [--obs]")
	}
	id, nome, err := idMeta(conn, args[0])
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("meta aportar", flag.ContinueOnError)
	valor := fs.String("valor", "", "quanto")
	data := fs.String("data", "hoje", "quando")
	obs := fs.String("obs", "", "observação")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	v, err := money.Parse(*valor)
	if err != nil || v <= 0 {
		return fmt.Errorf("informe --valor positivo")
	}
	d, err := parseData(*data)
	if err != nil {
		return err
	}
	if _, err := conn.Exec(`INSERT INTO metas_aportes (meta_id, valor, data, obs) VALUES (?, ?, ?, ?)`, id, sinal*v, d, *obs); err != nil {
		return err
	}
	metas, err := ListaMetas(conn, true)
	if err != nil {
		return err
	}
	for _, m := range metas {
		if m.ID != id {
			continue
		}
		verbo := "Guardado"
		if sinal < 0 {
			verbo = "Retirado"
		}
		fmt.Printf("%s %s em %s: agora %s de %s (%d%%).\n", verbo, money.Format(v), nome, money.Format(m.Guardado), money.Format(m.Alvo), m.Percentual())
		if m.Falta() == 0 && sinal > 0 {
			fmt.Println("🎉 Meta alcançada! Marque como concluída com: prisma meta concluir", id)
		}
	}
	return nil
}

func metaEditar(conn *sql.DB, args []string) error {
	if len(args) == 0 {
		return errors.New("uso: prisma meta editar <id> [--nome] [--valor] [--prazo]")
	}
	id, _, err := idMeta(conn, args[0])
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("meta editar", flag.ContinueOnError)
	nome := fs.String("nome", "", "novo nome")
	valor := fs.String("valor", "", "novo alvo")
	prazo := fs.String("prazo", "", `novo prazo ("-" tira o prazo)`)
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	informado := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { informado[f.Name] = true })
	var sets []string
	var params []any
	if informado["nome"] && strings.TrimSpace(*nome) != "" {
		sets, params = append(sets, "nome = ?"), append(params, strings.TrimSpace(*nome))
	}
	if informado["valor"] && *valor != "" {
		v, err := money.Parse(*valor)
		if err != nil || v <= 0 {
			return fmt.Errorf("valor inválido: %q", *valor)
		}
		sets, params = append(sets, "alvo = ?"), append(params, v)
	}
	if informado["prazo"] && *prazo != "" {
		p, err := parsePrazo(*prazo)
		if err != nil {
			return err
		}
		sets, params = append(sets, "prazo = ?"), append(params, p)
	}
	if len(sets) == 0 {
		return errors.New("nada a alterar: informe --nome, --valor ou --prazo")
	}
	params = append(params, id)
	if _, err := conn.Exec(`UPDATE metas SET `+strings.Join(sets, ", ")+` WHERE id = ?`, params...); err != nil {
		return err
	}
	fmt.Printf("Meta #%d atualizada.\n", id)
	return nil
}

func metaStatus(conn *sql.DB, args []string, status string) error {
	if len(args) == 0 {
		return errors.New("informe o id da meta")
	}
	id, nome, err := idMeta(conn, args[0])
	if err != nil {
		return err
	}
	if _, err := conn.Exec(`UPDATE metas SET status = ? WHERE id = ?`, status, id); err != nil {
		return err
	}
	if status == "concluida" {
		fmt.Printf("Meta #%d (%s) concluída. 🎉\n", id, nome)
	} else {
		fmt.Printf("Meta #%d (%s) reaberta.\n", id, nome)
	}
	return nil
}

func metaRemover(conn *sql.DB, args []string) error {
	if len(args) == 0 {
		return errors.New("informe o id da meta")
	}
	id, nome, err := idMeta(conn, args[0])
	if err != nil {
		return err
	}
	if _, err := conn.Exec(`DELETE FROM metas WHERE id = ?`, id); err != nil {
		return err
	}
	fmt.Printf("Meta #%d (%s) removida, com os aportes.\n", id, nome)
	return nil
}

// imprimeViabilidadeMetas mostra, no Analytics, se o superávit médio cobre os
// aportes mensais das metas salvas.
func imprimeViabilidadeMetas(conn *sql.DB, superavit int64) error {
	metas, err := ListaMetas(conn, false)
	if err != nil || len(metas) == 0 {
		return err
	}
	hoje := time.Now()
	fmt.Println("SUAS METAS")
	fmt.Println()
	var total int64
	for _, m := range metas {
		pm := m.PorMes(hoje)
		total += pm
		situacao := "sem prazo"
		switch {
		case m.Falta() == 0:
			situacao = "alcançada ✓"
		case pm > 0:
			situacao = money.Format(pm) + "/mês até " + dataBR(m.Prazo)
		}
		fmt.Printf("  • %-18s %3d%%  falta %-14s %s\n", m.Nome, m.Percentual(), money.Format(m.Falta()), situacao)
	}
	fmt.Println()
	fmt.Printf("  Aportes necessários: %s/mês · superávit previsto: %s/mês\n", money.Format(total), money.Format(superavit))
	if total > 0 {
		if superavit >= total {
			fmt.Printf("  ✓ Cabe: sobra %s/mês depois das metas.\n", money.Format(superavit-total))
		} else {
			fmt.Printf("  ⚠ Não cabe no ritmo atual: faltam %s/mês — estique um prazo ou corte gastos.\n", money.Format(total-superavit))
		}
	}
	fmt.Println()
	return nil
}
