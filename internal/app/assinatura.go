package app

import (
	"database/sql"
	"fmt"

	"prisma/internal/money"
)

// Uma assinatura (Netflix, Spotify, academia...) é uma recorrência de despesa
// marcada com `assinatura = 1`, em geral paga no cartão. Reaproveita toda a
// engine de recorrência: o que muda aqui é só a visão — listar só as
// assinaturas e somar o custo mensal. Criar/editar/remover delegam à
// recorrência (já com --assinatura e --tipo pagar embutidos no add).

// Assinaturas trata `prisma assinaturas [listar|add|editar|remover]`.
func Assinaturas(conn *sql.DB, args []string) error {
	if len(args) == 0 {
		args = []string{"listar"}
	}
	switch args[0] {
	case "listar", "ls":
		return assinaturasListar(conn)
	case "add", "adicionar":
		// toda assinatura é uma despesa recorrente; os flags do usuário
		// (que vêm depois) prevalecem caso queira mudar algo
		return recorrenciaAdd(conn, append([]string{"--tipo", "pagar", "--assinatura"}, args[1:]...))
	case "editar":
		return recorrenciaEditar(conn, args[1:])
	case "remover", "rm":
		return recorrenciaRemover(conn, args[1:])
	default:
		return fmt.Errorf("subcomando inválido %q (use: listar, add, editar, remover)", args[0])
	}
}

func assinaturasListar(conn *sql.DB) error {
	rows, err := conn.Query(`
		SELECT r.id, r.descricao, r.valor, r.dia, COALESCE(c.nome, ''), r.inicio, COALESCE(r.fim, ''), COALESCE(g.nome, ''),
		       (SELECT COUNT(*) FROM grupo_pessoas gp WHERE gp.grupo_id = r.grupo_id), r.intervalo
		FROM recorrencias r
		LEFT JOIN cartoes c ON c.id = r.cartao_id
		LEFT JOIN grupos g ON g.id = r.grupo_id
		WHERE r.assinatura = 1 ORDER BY r.id`)
	if err != nil {
		return err
	}
	defer rows.Close()

	w := novaTabela()
	fmt.Fprintln(w, "ID\tNOME\tVALOR\tCOBRANÇA\tCARTÃO\tGRUPO\tVIGÊNCIA\tRESTANTES")
	achou := false
	var totalMensal, totalAnual int64
	for rows.Next() {
		achou = true
		var id, valor int64
		var dia, pessoas int
		var desc, cartao, ini, fim, grupo, intervalo string
		if err := rows.Scan(&id, &desc, &valor, &dia, &cartao, &ini, &fim, &grupo, &pessoas, &intervalo); err != nil {
			return err
		}
		// com grupo, mostra a sua parte (valor ÷ pessoas), e os totais somam só ela
		grupoCol := ouTraco(grupo)
		if grupo != "" && pessoas > 0 {
			valor /= int64(pessoas)
			grupoCol = fmt.Sprintf("%s ÷%d", grupo, pessoas)
		}
		anual := intervalo == "anual"
		nome := desc
		// cobrança: mensal mostra só o dia; anual, o dia e o mês do aniversário
		cobranca := fmt.Sprintf("dia %d/mês", dia)
		if anual {
			nome += " (anual)"
			cobranca = fmt.Sprintf("dia %d de %s/ano", dia, ini[5:7])
			totalAnual += valor
		} else {
			totalMensal += valor
		}
		vig := "desde " + dataBR(ini)
		rest := "-"
		if fim != "" {
			vig = dataBR(ini) + " a " + dataBR(fim)
			rest = fmt.Sprintf("%d cobrança(s)", ocorrenciasRestantes(ini, fim, dia, intervalo))
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			id, nome, money.Format(valor), cobranca, ouTraco(cartao), grupoCol, vig, rest)
	}
	if !achou {
		fmt.Println("Nenhuma assinatura. Use: prisma assinaturas add --desc \"Netflix\" --valor 39,90 --dia 20 --cartao 1")
		fmt.Println("Para uma anuidade (1×/ano): prisma assinaturas add --desc \"Amazon Prime\" --valor 119,00 --dia 15 --intervalo anual")
		return nil
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Println()
	if totalAnual == 0 {
		fmt.Printf("Total mensal em assinaturas: %s\n", money.Format(totalMensal))
	} else {
		fmt.Printf("Mensais: %s/mês  ·  Anuais: %s/ano\n", money.Format(totalMensal), money.Format(totalAnual))
		fmt.Printf("Custo médio mensal: %s (anuais diluídas por 12)\n", money.Format(totalMensal+totalAnual/12))
	}
	return nil
}
