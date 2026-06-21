package tui

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"prisma/internal/app"
	"prisma/internal/money"
)

// sugestoesCategorias alimenta o campo-combo de categoria com o catálogo.
func sugestoesCategorias(conn *sql.DB) func() []string {
	return func() []string { return app.ListaCategorias(conn) }
}

// opcoesCategorias lista as categorias do catálogo como opções de seletor,
// precedidas das opções extras (ex.: "todas").
func opcoesCategorias(conn *sql.DB, extras ...opcao) func() []opcao {
	return func() []opcao {
		ops := append([]opcao{}, extras...)
		for _, c := range app.ListaCategorias(conn) {
			ops = append(ops, opcao{c, c})
		}
		return ops
	}
}

// valorForm formata centavos como "1.200,00" (sem "R$"), pronto para reedição.
func valorForm(c int64) string {
	return strings.TrimSpace(strings.TrimPrefix(money.Format(c), "R$"))
}

// nuloStr converte um inteiro de vínculo (conta/carteira/grupo/cartão) em texto
// para casar com a opção do seletor; 0/NULL viram "" (a opção "manter").
func nuloStr(v sql.NullInt64) string {
	if v.Valid {
		return fmt.Sprint(v.Int64)
	}
	return ""
}

// opcoesFixas monta um seletor estático a partir de pares valor, rótulo.
func opcoesFixas(pares ...string) func() []opcao {
	ops := make([]opcao, 0, len(pares)/2)
	for i := 0; i+1 < len(pares); i += 2 {
		ops = append(ops, opcao{pares[i], pares[i+1]})
	}
	return func() []opcao { return ops }
}

func simNao() func() []opcao { return opcoesFixas("", "não", "s", "sim") }

// opcoesVinculo lista contas ou carteiras como opções de seletor, precedidas
// das opções extras (ex.: "nenhuma", "manter", "desvincular").
func opcoesVinculo(conn *sql.DB, tabela string, extras ...opcao) func() []opcao {
	return func() []opcao {
		ops := append([]opcao{}, extras...)
		rows, err := conn.Query(`SELECT id, nome FROM ` + tabela + ` ORDER BY id`)
		if err != nil {
			return ops
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var nome string
			if err := rows.Scan(&id, &nome); err != nil {
				continue
			}
			ops = append(ops, opcao{fmt.Sprint(id), fmt.Sprintf("%d — %s", id, nome)})
		}
		return ops
	}
}

// opcoesGrupos lista os grupos como opções de seletor, com o número de pessoas,
// precedidos das opções extras (ex.: "nenhum", "manter", "desvincular").
func opcoesGrupos(conn *sql.DB, extras ...opcao) func() []opcao {
	return func() []opcao {
		ops := append([]opcao{}, extras...)
		rows, err := conn.Query(`
			SELECT g.id, g.nome, COUNT(gp.id)
			FROM grupos g LEFT JOIN grupo_pessoas gp ON gp.grupo_id = g.id
			GROUP BY g.id, g.nome ORDER BY g.id`)
		if err != nil {
			return ops
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var nome string
			var qtd int
			if err := rows.Scan(&id, &nome, &qtd); err != nil {
				continue
			}
			ops = append(ops, opcao{fmt.Sprint(id), fmt.Sprintf("%d — %s (÷%d)", id, nome, qtd)})
		}
		return ops
	}
}

// opcoesSocios lista os sócios da empresa como opções de seletor, com a
// participação, precedidos das opções extras.
func opcoesSocios(conn *sql.DB, extras ...opcao) func() []opcao {
	return func() []opcao {
		ops := append([]opcao{}, extras...)
		rows, err := conn.Query(`SELECT id, nome, participacao FROM socios ORDER BY id`)
		if err != nil {
			return ops
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var nome string
			var participacao float64
			if err := rows.Scan(&id, &nome, &participacao); err != nil {
				continue
			}
			ops = append(ops, opcao{fmt.Sprint(id), fmt.Sprintf("%d — %s (%.0f%%)", id, nome, participacao)})
		}
		return ops
	}
}

// opcoesCartoes lista os cartões como opções de seletor, precedidos das
// opções extras (ex.: "nenhum").
func opcoesCartoes(conn *sql.DB, extras ...opcao) func() []opcao {
	return func() []opcao {
		ops := append([]opcao{}, extras...)
		rows, err := conn.Query(`SELECT id, nome FROM cartoes ORDER BY id`)
		if err != nil {
			return ops
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var nome string
			if err := rows.Scan(&id, &nome); err != nil {
				continue
			}
			ops = append(ops, opcao{fmt.Sprint(id), fmt.Sprintf("%d — %s", id, nome)})
		}
		return ops
	}
}

// opcoesLocais lista contas E carteiras na sintaxe conta:ID / carteira:ID
// (para transferências).
func opcoesLocais(conn *sql.DB) func() []opcao {
	return func() []opcao {
		var ops []opcao
		for _, tabela := range []string{"conta", "carteira"} {
			rows, err := conn.Query(`SELECT id, nome FROM ` + tabela + `s ORDER BY id`)
			if err != nil {
				continue
			}
			for rows.Next() {
				var id int64
				var nome string
				if err := rows.Scan(&id, &nome); err != nil {
					continue
				}
				ops = append(ops, opcao{
					fmt.Sprintf("%s:%d", tabela, id),
					fmt.Sprintf("%s %d — %s", tabela, id, nome),
				})
			}
			rows.Close()
		}
		return ops
	}
}

// novasTelas define o menu: cada tela reaproveita os comandos da CLI,
// capturando a saída deles para exibir na interface.
func novasTelas(conn *sql.DB, modoEmpresa bool) []tela {
	exec := func(f func() error) (string, error) { return captura(f) }
	nenhuma := opcao{"", "nenhuma"}
	manter := opcao{"", "manter"}
	desvincular := opcao{"0", "desvincular"}

	telas := []tela{
		{
			titulo:   "Saldo",
			resumo:   "posição geral consolidada",
			conteudo: func(_ []string) (string, error) { return captura(func() error { return app.Saldo(conn, nil) }) },
			acoes: []acao{
				{
					tecla: "t", rotulo: "transferir",
					campos: []campo{
						{rotulo: "de", obrigatorio: true, opcoes: opcoesLocais(conn)},
						{rotulo: "para", obrigatorio: true, opcoes: opcoesLocais(conn)},
						{rotulo: "valor", dica: "ex.: 200,00", obrigatorio: true},
						{rotulo: "data", dica: "opcional (padrão: hoje)"},
						{rotulo: "descrição", dica: "opcional"},
					},
					executar: func(v []string) (string, error) {
						args := append([]string{}, par("--de", v[0])...)
						args = append(args, par("--para", v[1])...)
						args = append(args, par("--valor", v[2])...)
						args = append(args, par("--data", v[3])...)
						args = append(args, par("--desc", v[4])...)
						return exec(func() error { return app.Transferir(conn, args) })
					},
				},
				{
					tecla: "z", rotulo: "zerar banco", confirma: true,
					campos: []campo{
						{rotulo: "confirme", dica: "digite apagar (faz backup antes)", obrigatorio: true},
					},
					executar: func(v []string) (string, error) {
						if strings.ToLower(strings.TrimSpace(v[0])) != "apagar" {
							return "", fmt.Errorf("digite \"apagar\" no campo para confirmar")
						}
						return exec(func() error { return app.Resetar(conn, []string{"--sim"}) })
					},
				},
			},
		},
		{
			titulo: "Contas",
			resumo: "cadastro de contas bancárias",
			conteudo: func(p []string) (string, error) {
				if len(p) > 1 && p[0] == "extrato" {
					return captura(func() error { return app.Extrato(conn, []string{"--conta", p[1]}) })
				}
				return captura(func() error { return app.Conta(conn, []string{"listar"}) })
			},
			acoes: []acao{
				{
					tecla: "a", rotulo: "adicionar",
					campos: []campo{
						{rotulo: "nome", dica: "ex.: Nubank", obrigatorio: true},
						{rotulo: "banco", dica: "opcional"},
						{rotulo: "tipo", opcoes: opcoesFixas("corrente", "corrente", "poupanca", "poupança", "investimento", "investimento")},
						{rotulo: "saldo", dica: "ex.: 1.500,00"},
					},
					executar: func(v []string) (string, error) {
						args := []string{"add"}
						args = append(args, par("--nome", v[0])...)
						args = append(args, par("--banco", v[1])...)
						args = append(args, par("--tipo", v[2])...)
						args = append(args, par("--saldo", v[3])...)
						return exec(func() error { return app.Conta(conn, args) })
					},
				},
				{
					tecla: "e", rotulo: "editar",
					campos: []campo{
						{rotulo: "id", dica: "número da conta", obrigatorio: true},
						{rotulo: "nome", dica: "vazio mantém"},
						{rotulo: "banco", dica: "vazio mantém"},
						{rotulo: "tipo", opcoes: opcoesFixas("", "manter", "corrente", "corrente", "poupanca", "poupança", "investimento", "investimento")},
						{rotulo: "saldo inicial", dica: "vazio mantém"},
					},
					carregar: func(id string) ([]string, error) {
						var nome, banco, tipo string
						var saldo int64
						err := conn.QueryRow(`SELECT nome, banco, tipo, saldo_inicial FROM contas WHERE id = ?`, id).
							Scan(&nome, &banco, &tipo, &saldo)
						if err != nil {
							return nil, err
						}
						return []string{nome, banco, tipo, valorForm(saldo)}, nil
					},
					executar: func(v []string) (string, error) {
						args := []string{"editar", v[0]}
						args = append(args, par("--nome", v[1])...)
						args = append(args, par("--banco", v[2])...)
						args = append(args, par("--tipo", v[3])...)
						args = append(args, par("--saldo", v[4])...)
						return exec(func() error { return app.Conta(conn, args) })
					},
				},
				{
					tecla: "t", rotulo: "extrato",
					campos: []campo{{rotulo: "id", dica: "número da conta", obrigatorio: true}},
					params: func(v []string) []string { return []string{"extrato", v[0]} },
				},
				{
					tecla: "l", rotulo: "lista",
					params: func(_ []string) []string { return nil },
				},
				{
					tecla: "x", rotulo: "remover", confirma: true,
					campos: []campo{{rotulo: "id", dica: "número da conta", obrigatorio: true}},
					executar: func(v []string) (string, error) {
						return exec(func() error { return app.Conta(conn, []string{"remover", v[0]}) })
					},
				},
			},
		},
		{
			titulo: "Carteiras",
			resumo: "dinheiro fora do banco",
			conteudo: func(p []string) (string, error) {
				if len(p) > 1 && p[0] == "extrato" {
					return captura(func() error { return app.Extrato(conn, []string{"--carteira", p[1]}) })
				}
				return captura(func() error { return app.Carteira(conn, []string{"listar"}) })
			},
			acoes: []acao{
				{
					tecla: "a", rotulo: "adicionar",
					campos: []campo{
						{rotulo: "nome", dica: "ex.: Dinheiro", obrigatorio: true},
						{rotulo: "descrição", dica: "opcional"},
						{rotulo: "saldo", dica: "ex.: 200,00"},
					},
					executar: func(v []string) (string, error) {
						args := []string{"add"}
						args = append(args, par("--nome", v[0])...)
						args = append(args, par("--desc", v[1])...)
						args = append(args, par("--saldo", v[2])...)
						return exec(func() error { return app.Carteira(conn, args) })
					},
				},
				{
					tecla: "e", rotulo: "editar",
					campos: []campo{
						{rotulo: "id", dica: "número da carteira", obrigatorio: true},
						{rotulo: "nome", dica: "vazio mantém"},
						{rotulo: "descrição", dica: "vazio mantém"},
						{rotulo: "saldo inicial", dica: "vazio mantém"},
					},
					carregar: func(id string) ([]string, error) {
						var nome, desc string
						var saldo int64
						err := conn.QueryRow(`SELECT nome, descricao, saldo_inicial FROM carteiras WHERE id = ?`, id).
							Scan(&nome, &desc, &saldo)
						if err != nil {
							return nil, err
						}
						return []string{nome, desc, valorForm(saldo)}, nil
					},
					executar: func(v []string) (string, error) {
						args := []string{"editar", v[0]}
						args = append(args, par("--nome", v[1])...)
						args = append(args, par("--desc", v[2])...)
						args = append(args, par("--saldo", v[3])...)
						return exec(func() error { return app.Carteira(conn, args) })
					},
				},
				{
					tecla: "t", rotulo: "extrato",
					campos: []campo{{rotulo: "id", dica: "número da carteira", obrigatorio: true}},
					params: func(v []string) []string { return []string{"extrato", v[0]} },
				},
				{
					tecla: "l", rotulo: "lista",
					params: func(_ []string) []string { return nil },
				},
				{
					tecla: "x", rotulo: "remover", confirma: true,
					campos: []campo{{rotulo: "id", dica: "número da carteira", obrigatorio: true}},
					executar: func(v []string) (string, error) {
						return exec(func() error { return app.Carteira(conn, []string{"remover", v[0]}) })
					},
				},
			},
		},
		{
			titulo: "Grupos",
			resumo: "pessoas que dividem despesas",
			conteudo: func(p []string) (string, error) {
				if len(p) > 1 && p[0] == "ver" {
					return captura(func() error { return app.Lancamentos(conn, []string{"--grupo", p[1]}) })
				}
				return captura(func() error { return app.Grupo(conn, []string{"listar"}) })
			},
			acoes: []acao{
				{
					tecla: "a", rotulo: "adicionar",
					campos: []campo{
						{rotulo: "nome", dica: "ex.: Eu e a Maria", obrigatorio: true},
						{rotulo: "pessoas", dica: "separadas por vírgula (mín. 2)", obrigatorio: true},
					},
					executar: func(v []string) (string, error) {
						args := []string{"add"}
						args = append(args, par("--nome", v[0])...)
						args = append(args, par("--pessoas", v[1])...)
						return exec(func() error { return app.Grupo(conn, args) })
					},
				},
				{
					tecla: "e", rotulo: "editar",
					campos: []campo{
						{rotulo: "id", dica: "número do grupo", obrigatorio: true},
						{rotulo: "nome", dica: "vazio mantém"},
						{rotulo: "pessoas", dica: "lista nova substitui; vazio mantém"},
					},
					carregar: func(id string) ([]string, error) {
						var nome, pessoas string
						err := conn.QueryRow(`
							SELECT g.nome, COALESCE(GROUP_CONCAT(gp.nome, ', '), '')
							FROM grupos g LEFT JOIN grupo_pessoas gp ON gp.grupo_id = g.id
							WHERE g.id = ? GROUP BY g.id, g.nome`, id).Scan(&nome, &pessoas)
						if err != nil {
							return nil, err
						}
						return []string{nome, pessoas}, nil
					},
					executar: func(v []string) (string, error) {
						args := []string{"editar", v[0]}
						args = append(args, par("--nome", v[1])...)
						args = append(args, par("--pessoas", v[2])...)
						return exec(func() error { return app.Grupo(conn, args) })
					},
				},
				{
					tecla: "v", rotulo: "ver despesas",
					campos: []campo{{rotulo: "id", dica: "número do grupo", obrigatorio: true}},
					params: func(v []string) []string { return []string{"ver", v[0]} },
				},
				{
					tecla: "l", rotulo: "lista",
					params: func(_ []string) []string { return nil },
				},
				{
					tecla: "x", rotulo: "remover", confirma: true,
					campos: []campo{{rotulo: "id", dica: "número do grupo", obrigatorio: true}},
					executar: func(v []string) (string, error) {
						return exec(func() error { return app.Grupo(conn, []string{"remover", v[0]}) })
					},
				},
			},
		},
		{
			titulo: "Categorias",
			resumo: "catálogo de categorias",
			conteudo: func(_ []string) (string, error) {
				return captura(func() error { return app.Categoria(conn, []string{"listar"}) })
			},
			acoes: []acao{
				{
					tecla: "a", rotulo: "adicionar",
					campos: []campo{{rotulo: "nome", dica: "ex.: mercado", obrigatorio: true}},
					executar: func(v []string) (string, error) {
						return exec(func() error { return app.Categoria(conn, []string{"add", "--nome", v[0]}) })
					},
				},
				{
					tecla: "e", rotulo: "renomear",
					campos: []campo{
						{rotulo: "id", dica: "número da categoria", obrigatorio: true},
						{rotulo: "nome", dica: "novo nome", obrigatorio: true},
					},
					carregar: func(id string) ([]string, error) {
						var nome string
						if err := conn.QueryRow(`SELECT nome FROM categorias WHERE id = ?`, id).Scan(&nome); err != nil {
							return nil, err
						}
						return []string{nome}, nil
					},
					executar: func(v []string) (string, error) {
						return exec(func() error { return app.Categoria(conn, []string{"editar", v[0], "--nome", v[1]}) })
					},
				},
				{
					tecla: "x", rotulo: "remover", confirma: true,
					campos: []campo{{rotulo: "id", dica: "número da categoria", obrigatorio: true}},
					executar: func(v []string) (string, error) {
						return exec(func() error { return app.Categoria(conn, []string{"remover", v[0]}) })
					},
				},
			},
		},
		{
			titulo: "Cartões",
			resumo: "cartões de crédito e faturas",
			conteudo: func(p []string) (string, error) {
				if len(p) >= 2 && p[0] == "fatura" {
					args := []string{"--cartao", p[1]}
					if len(p) >= 3 && p[2] != "" {
						args = append(args, "--ref", p[2])
					}
					if len(p) >= 4 && p[3] == "sim" {
						args = append(args, "--abertos")
					}
					return captura(func() error { return app.Fatura(conn, args) })
				}
				return captura(func() error { return app.Cartao(conn, []string{"listar"}) })
			},
			acoes: []acao{
				{
					tecla: "a", rotulo: "adicionar",
					campos: []campo{
						{rotulo: "nome", dica: "ex.: Nubank", obrigatorio: true},
						{rotulo: "limite", dica: "ex.: 5.000,00 (opcional)"},
						{rotulo: "fechamento", dica: "dia da fatura fechar (1-31)", obrigatorio: true},
						{rotulo: "vencimento", dica: "dia da fatura vencer (1-31)", obrigatorio: true},
						{rotulo: "conta que paga", opcoes: opcoesVinculo(conn, "contas", nenhuma)},
						{rotulo: "fatura atual", dica: "valor em aberto hoje (opcional)"},
					},
					executar: func(v []string) (string, error) {
						args := []string{"add"}
						args = append(args, par("--nome", v[0])...)
						args = append(args, par("--limite", v[1])...)
						args = append(args, par("--fechamento", v[2])...)
						args = append(args, par("--vencimento", v[3])...)
						args = append(args, par("--conta", v[4])...)
						args = append(args, par("--fatura-atual", v[5])...)
						return exec(func() error { return app.Cartao(conn, args) })
					},
				},
				{
					tecla: "e", rotulo: "editar",
					campos: []campo{
						{rotulo: "id", dica: "número do cartão", obrigatorio: true},
						{rotulo: "nome", dica: "vazio mantém"},
						{rotulo: "limite", dica: "vazio mantém"},
						{rotulo: "fechamento", dica: "vazio mantém"},
						{rotulo: "vencimento", dica: "vazio mantém"},
						{rotulo: "conta que paga", opcoes: opcoesVinculo(conn, "contas", manter, desvincular)},
					},
					carregar: func(id string) ([]string, error) {
						var nome string
						var limite int64
						var fech, venc int
						var conta sql.NullInt64
						err := conn.QueryRow(`SELECT nome, limite, dia_fechamento, dia_vencimento, conta_id FROM cartoes WHERE id = ?`, id).
							Scan(&nome, &limite, &fech, &venc, &conta)
						if err != nil {
							return nil, err
						}
						return []string{nome, valorForm(limite), fmt.Sprint(fech), fmt.Sprint(venc), nuloStr(conta)}, nil
					},
					executar: func(v []string) (string, error) {
						args := []string{"editar", v[0]}
						args = append(args, par("--nome", v[1])...)
						args = append(args, par("--limite", v[2])...)
						args = append(args, par("--fechamento", v[3])...)
						args = append(args, par("--vencimento", v[4])...)
						args = append(args, par("--conta", v[5])...)
						return exec(func() error { return app.Cartao(conn, args) })
					},
				},
				{
					tecla: "t", rotulo: "ver fatura",
					campos: []campo{
						{rotulo: "id", dica: "número do cartão", obrigatorio: true},
						{rotulo: "fatura", dica: "AAAA-MM (vazio = a aberta)"},
						{rotulo: "só em aberto?", opcoes: simNao()},
					},
					params: func(v []string) []string {
						aberto := ""
						if sim(v[2]) {
							aberto = "sim"
						}
						return []string{"fatura", v[0], v[1], aberto}
					},
				},
				{
					tecla: "p", rotulo: "pagar fatura", confirma: true,
					campos: []campo{
						{rotulo: "id", dica: "número do cartão", obrigatorio: true},
						{rotulo: "fatura", dica: "AAAA-MM (vazio = a aberta)"},
						{rotulo: "data", dica: "opcional (padrão: hoje)"},
						{rotulo: "conta", opcoes: opcoesVinculo(conn, "contas", opcao{"", "a do cartão"})},
					},
					executar: func(v []string) (string, error) {
						args := []string{"pagar", "--cartao", v[0]}
						args = append(args, par("--ref", v[1])...)
						args = append(args, par("--data", v[2])...)
						args = append(args, par("--conta", v[3])...)
						return exec(func() error { return app.Fatura(conn, args) })
					},
				},
				{
					tecla: "l", rotulo: "lista",
					params: func(_ []string) []string { return nil },
				},
				{
					tecla: "x", rotulo: "remover", confirma: true,
					campos: []campo{{rotulo: "id", dica: "número do cartão", obrigatorio: true}},
					executar: func(v []string) (string, error) {
						return exec(func() error { return app.Cartao(conn, []string{"remover", v[0]}) })
					},
				},
			},
		},
		{
			titulo:      "Pagar/Receber",
			resumo:      "lançamentos e quitação",
			listaMensal: true,
			// ao abrir, mostra o mês atual (←/→ muda o mês, t alterna o tipo)
			padrao: []string{"--mes", time.Now().Format("2006-01")},
			conteudo: func(p []string) (string, error) {
				return captura(func() error { return app.Lancamentos(conn, p) })
			},
			acoes: []acao{
				{
					tecla: "p", rotulo: "a pagar",
					campos: camposLancamento(conn, nenhuma, "pagar"),
					executar: func(v []string) (string, error) {
						return exec(func() error { return app.NovoLancamento(conn, "pagar", argsLancamento(v, "pagar")) })
					},
				},
				{
					tecla: "r", rotulo: "a receber",
					campos: camposLancamento(conn, nenhuma, "receber"),
					executar: func(v []string) (string, error) {
						return exec(func() error { return app.NovoLancamento(conn, "receber", argsLancamento(v, "receber")) })
					},
				},
				{
					tecla: "u", rotulo: "quitar",
					campos: []campo{
						{rotulo: "id", dica: "número do lançamento", obrigatorio: true},
						{rotulo: "data", dica: "opcional (padrão: hoje)"},
					},
					executar: func(v []string) (string, error) {
						args := []string{v[0]}
						args = append(args, par("--data", v[1])...)
						return exec(func() error { return app.Quitar(conn, args) })
					},
				},
				{
					tecla: "e", rotulo: "editar",
					campos: []campo{
						{rotulo: "id", dica: "número do lançamento", obrigatorio: true},
						{rotulo: "descrição"},
						{rotulo: "valor"},
						{rotulo: "vencimento", dica: "AAAA-MM-DD, DD/MM/AAAA ou DD/MM"},
						{rotulo: "categoria", sugestoes: sugestoesCategorias(conn)},
						{rotulo: "conta", opcoes: opcoesVinculo(conn, "contas", manter, desvincular)},
						{rotulo: "carteira", opcoes: opcoesVinculo(conn, "carteiras", manter, desvincular)},
						{rotulo: "grupo", opcoes: opcoesGrupos(conn, manter, desvincular)},
						{rotulo: "cartão", opcoes: opcoesCartoes(conn, manter, desvincular)},
						{rotulo: "observação", dica: "use - para limpar"},
						{rotulo: "quitar no vencimento?", opcoes: opcoesFixas("", "manter", "sim", "sim", "nao", "não")},
					},
					carregar: func(id string) ([]string, error) {
						var desc, venc, cat, obs string
						var valor int64
						var conta, carteira, grupo, cartao sql.NullInt64
						var auto int
						err := conn.QueryRow(`
							SELECT descricao, valor, vencimento, categoria, conta_id, carteira_id, grupo_id, cartao_id, observacao, auto_quitar
							FROM lancamentos WHERE id = ?`, id).
							Scan(&desc, &valor, &venc, &cat, &conta, &carteira, &grupo, &cartao, &obs, &auto)
						if err != nil {
							return nil, err
						}
						contaS, carteiraS := nuloStr(conta), nuloStr(carteira)
						if cartao.Valid { // no cartão, não reenviar conta/carteira junto
							contaS, carteiraS = "", ""
						}
						aq := "nao"
						if auto == 1 {
							aq = "sim"
						}
						return []string{desc, valorForm(valor), venc, cat, contaS, carteiraS, nuloStr(grupo), nuloStr(cartao), obs, aq}, nil
					},
					executar: func(v []string) (string, error) {
						args := []string{"editar", v[0]}
						args = append(args, par("--desc", v[1])...)
						args = append(args, par("--valor", v[2])...)
						args = append(args, par("--venc", v[3])...)
						args = append(args, par("--cat", v[4])...)
						args = append(args, par("--conta", v[5])...)
						args = append(args, par("--carteira", v[6])...)
						args = append(args, par("--grupo", v[7])...)
						args = append(args, par("--cartao", v[8])...)
						args = append(args, par("--obs", v[9])...)
						args = append(args, par("--auto-quitar", v[10])...)
						return exec(func() error { return app.Lancamentos(conn, args) })
					},
				},
				{
					tecla: "x", rotulo: "remover", confirma: true,
					campos: []campo{{rotulo: "id", dica: "número do lançamento", obrigatorio: true}},
					executar: func(v []string) (string, error) {
						return exec(func() error { return app.Lancamentos(conn, []string{"remover", v[0]}) })
					},
				},
				{
					tecla: "f", rotulo: "filtrar",
					campos: []campo{
						{rotulo: "tipo", opcoes: opcoesFixas("", "todos", "pagar", "pagar", "receber", "receber")},
						{rotulo: "mês", dica: "AAAA-MM, opcional"},
						{rotulo: "de", dica: "data inicial, opcional"},
						{rotulo: "até", dica: "data final, opcional"},
						{rotulo: "categoria", opcoes: opcoesCategorias(conn, opcao{"", "todas"})},
						{rotulo: "pendentes", opcoes: simNao()},
					},
					params: func(v []string) []string {
						var p []string
						p = append(p, par("--tipo", v[0])...)
						p = append(p, par("--mes", v[1])...)
						p = append(p, par("--de", v[2])...)
						p = append(p, par("--ate", v[3])...)
						p = append(p, par("--cat", v[4])...)
						if sim(v[5]) {
							p = append(p, "--pendentes")
						}
						return p
					},
				},
			},
		},
		{
			titulo: "Recorrências",
			resumo: "salário, aluguel: todo mês, sozinho",
			padrao: []string{"listar"},
			conteudo: func(p []string) (string, error) {
				return captura(func() error { return app.Recorrencia(conn, p) })
			},
			acoes: []acao{
				{
					tecla: "a", rotulo: "nova regra",
					campos: []campo{
						{rotulo: "tipo", obrigatorio: true, opcoes: opcoesFixas("pagar", "pagar", "receber", "receber")},
						{rotulo: "descrição", dica: "ex.: Salário", obrigatorio: true},
						{rotulo: "valor", dica: "ex.: 5.000,00", obrigatorio: true},
						{rotulo: "dia", dica: "dia do mês, 1 a 31", obrigatorio: true},
						{rotulo: "categoria", dica: "opcional", sugestoes: sugestoesCategorias(conn)},
						{rotulo: "conta", opcoes: opcoesVinculo(conn, "contas", nenhuma)},
						{rotulo: "carteira", opcoes: opcoesVinculo(conn, "carteiras", nenhuma)},
						{rotulo: "grupo", dica: "divide a despesa entre as pessoas", opcoes: opcoesGrupos(conn, nenhuma)},
						{rotulo: "cartão", dica: "gera os lançamentos na fatura", opcoes: opcoesCartoes(conn, nenhuma)},
						{rotulo: "início", dica: "opcional (padrão: hoje)"},
						{rotulo: "fim", dica: "opcional (vazio = sem fim)"},
						{rotulo: "intervalo", dica: "com que frequência se repete", opcoes: opcoesFixas("mensal", "mensal", "anual", "anual")},
						{rotulo: "quitar no vencimento?", dica: "quita sozinho ao vencer", opcoes: simNao()},
						{rotulo: "quitar anteriores?", dica: "se o início for no passado", opcoes: simNao()},
					},
					executar: func(v []string) (string, error) {
						args := []string{"add"}
						args = append(args, par("--tipo", v[0])...)
						args = append(args, par("--desc", v[1])...)
						args = append(args, par("--valor", v[2])...)
						args = append(args, par("--dia", v[3])...)
						args = append(args, par("--cat", v[4])...)
						args = append(args, par("--conta", v[5])...)
						args = append(args, par("--carteira", v[6])...)
						args = append(args, par("--grupo", v[7])...)
						args = append(args, par("--cartao", v[8])...)
						args = append(args, par("--inicio", v[9])...)
						args = append(args, par("--fim", v[10])...)
						args = append(args, par("--intervalo", v[11])...)
						if sim(v[12]) {
							args = append(args, "--auto-quitar")
						}
						// a TUI sempre decide (nunca cai na pergunta interativa)
						if sim(v[13]) {
							args = append(args, "--passados", "quitar")
						} else {
							args = append(args, "--passados", "manter")
						}
						return exec(func() error { return app.Recorrencia(conn, args) })
					},
				},
				{
					tecla: "e", rotulo: "editar",
					campos: []campo{
						{rotulo: "id", dica: "número da recorrência", obrigatorio: true},
						{rotulo: "descrição", dica: "vazio mantém"},
						{rotulo: "valor", dica: "vazio mantém"},
						{rotulo: "dia", dica: "vazio mantém"},
						{rotulo: "categoria", dica: "vazio mantém", sugestoes: sugestoesCategorias(conn)},
						{rotulo: "conta", opcoes: opcoesVinculo(conn, "contas", manter, desvincular)},
						{rotulo: "carteira", opcoes: opcoesVinculo(conn, "carteiras", manter, desvincular)},
						{rotulo: "grupo", opcoes: opcoesGrupos(conn, manter, desvincular)},
						{rotulo: "cartão", opcoes: opcoesCartoes(conn, manter, desvincular)},
						{rotulo: "fim", dica: "data, \"nunca\" remove, vazio mantém"},
						{rotulo: "quitar no vencimento?", opcoes: opcoesFixas("", "manter", "sim", "sim", "nao", "não")},
					},
					carregar: func(id string) ([]string, error) {
						var desc, cat, fim string
						var valor int64
						var dia, auto int
						var conta, carteira, grupo, cartao sql.NullInt64
						err := conn.QueryRow(`
							SELECT descricao, valor, dia, categoria, conta_id, carteira_id, grupo_id, cartao_id, COALESCE(fim, ''), auto_quitar
							FROM recorrencias WHERE id = ?`, id).
							Scan(&desc, &valor, &dia, &cat, &conta, &carteira, &grupo, &cartao, &fim, &auto)
						if err != nil {
							return nil, err
						}
						contaS, carteiraS := nuloStr(conta), nuloStr(carteira)
						if cartao.Valid {
							contaS, carteiraS = "", ""
						}
						aq := "nao"
						if auto == 1 {
							aq = "sim"
						}
						return []string{desc, valorForm(valor), fmt.Sprint(dia), cat, contaS, carteiraS, nuloStr(grupo), nuloStr(cartao), fim, aq}, nil
					},
					executar: func(v []string) (string, error) {
						args := []string{"editar", v[0]}
						args = append(args, par("--desc", v[1])...)
						args = append(args, par("--valor", v[2])...)
						args = append(args, par("--dia", v[3])...)
						args = append(args, par("--cat", v[4])...)
						args = append(args, par("--conta", v[5])...)
						args = append(args, par("--carteira", v[6])...)
						args = append(args, par("--grupo", v[7])...)
						args = append(args, par("--cartao", v[8])...)
						args = append(args, par("--fim", v[9])...)
						args = append(args, par("--auto-quitar", v[10])...)
						return exec(func() error { return app.Recorrencia(conn, args) })
					},
				},
				{
					tecla: "f", rotulo: "filtrar",
					campos: []campo{
						{rotulo: "tipo", opcoes: opcoesFixas("", "todos", "pagar", "pagar", "receber", "receber")},
						{rotulo: "só vigentes?", opcoes: simNao(), dica: "esconde as encerradas"},
						{rotulo: "só assinaturas?", opcoes: simNao()},
					},
					params: func(v []string) []string {
						p := []string{"listar"}
						p = append(p, par("--tipo", v[0])...)
						if sim(v[1]) {
							p = append(p, "--vigentes")
						}
						if sim(v[2]) {
							p = append(p, "--assinaturas")
						}
						return p
					},
				},
				{
					tecla: "x", rotulo: "remover", confirma: true,
					campos: []campo{
						{rotulo: "id", dica: "número da recorrência", obrigatorio: true},
						{rotulo: "limpar", opcoes: simNao(), dica: "apaga pendentes gerados"},
					},
					executar: func(v []string) (string, error) {
						args := []string{"remover", v[0]}
						if sim(v[1]) {
							args = append(args, "--limpar")
						}
						return exec(func() error { return app.Recorrencia(conn, args) })
					},
				},
			},
		},
		{
			titulo: "Assinaturas",
			resumo: "serviços recorrentes (Netflix, Spotify...)",
			padrao: []string{"listar"},
			conteudo: func(p []string) (string, error) {
				return captura(func() error { return app.Assinaturas(conn, p) })
			},
			acoes: []acao{
				{
					tecla: "a", rotulo: "nova assinatura",
					campos: []campo{
						{rotulo: "descrição", dica: "ex.: Netflix", obrigatorio: true},
						{rotulo: "valor", dica: "ex.: 39,90", obrigatorio: true},
						{rotulo: "dia", dica: "dia da cobrança, 1 a 31", obrigatorio: true},
						{rotulo: "cartão", dica: "onde é cobrada", opcoes: opcoesCartoes(conn, nenhuma)},
						{rotulo: "conta", dica: "se não for no cartão", opcoes: opcoesVinculo(conn, "contas", nenhuma)},
						{rotulo: "grupo", dica: "divide a despesa entre as pessoas", opcoes: opcoesGrupos(conn, nenhuma)},
						{rotulo: "início", dica: "opcional (padrão: hoje)"},
						{rotulo: "fim", dica: "opcional (vazio = sem fim)"},
					},
					executar: func(v []string) (string, error) {
						args := []string{"add"}
						args = append(args, par("--desc", v[0])...)
						args = append(args, par("--valor", v[1])...)
						args = append(args, par("--dia", v[2])...)
						args = append(args, par("--cartao", v[3])...)
						args = append(args, par("--conta", v[4])...)
						args = append(args, par("--grupo", v[5])...)
						args = append(args, par("--inicio", v[6])...)
						args = append(args, par("--fim", v[7])...)
						return exec(func() error { return app.Assinaturas(conn, args) })
					},
				},
				{
					tecla: "e", rotulo: "editar",
					campos: []campo{
						{rotulo: "id", dica: "número da assinatura", obrigatorio: true},
						{rotulo: "descrição", dica: "vazio mantém"},
						{rotulo: "valor", dica: "vazio mantém"},
						{rotulo: "dia", dica: "vazio mantém"},
						{rotulo: "cartão", opcoes: opcoesCartoes(conn, manter, desvincular)},
						{rotulo: "grupo", opcoes: opcoesGrupos(conn, manter, desvincular)},
						{rotulo: "fim", dica: "data, \"nunca\" remove, vazio mantém"},
					},
					carregar: func(id string) ([]string, error) {
						var desc, fim string
						var valor int64
						var dia int
						var cartao, grupo sql.NullInt64
						err := conn.QueryRow(`
							SELECT descricao, valor, dia, cartao_id, grupo_id, COALESCE(fim, '')
							FROM recorrencias WHERE id = ?`, id).
							Scan(&desc, &valor, &dia, &cartao, &grupo, &fim)
						if err != nil {
							return nil, err
						}
						return []string{desc, valorForm(valor), fmt.Sprint(dia), nuloStr(cartao), nuloStr(grupo), fim}, nil
					},
					executar: func(v []string) (string, error) {
						args := []string{"editar", v[0]}
						args = append(args, par("--desc", v[1])...)
						args = append(args, par("--valor", v[2])...)
						args = append(args, par("--dia", v[3])...)
						args = append(args, par("--cartao", v[4])...)
						args = append(args, par("--grupo", v[5])...)
						args = append(args, par("--fim", v[6])...)
						return exec(func() error { return app.Assinaturas(conn, args) })
					},
				},
				{
					tecla: "x", rotulo: "remover", confirma: true,
					campos: []campo{
						{rotulo: "id", dica: "número da assinatura", obrigatorio: true},
						{rotulo: "limpar", opcoes: simNao(), dica: "apaga pendentes gerados"},
					},
					executar: func(v []string) (string, error) {
						args := []string{"remover", v[0]}
						if sim(v[1]) {
							args = append(args, "--limpar")
						}
						return exec(func() error { return app.Assinaturas(conn, args) })
					},
				},
			},
		},
		{
			titulo: "Emergência",
			resumo: "plano de ação para quitar dívidas",
			padrao: []string{"listar"},
			conteudo: func(p []string) (string, error) {
				return captura(func() error { return app.Emergencia(conn, p) })
			},
			acoes: []acao{
				{
					tecla: "a", rotulo: "nova dívida",
					campos: []campo{
						{rotulo: "descrição", dica: "ex.: Cartão de crédito", obrigatorio: true},
						{rotulo: "credor", dica: "opcional"},
						{rotulo: "valor", dica: "total da dívida", obrigatorio: true},
						{rotulo: "juros", dica: "% ao mês, ex.: 12"},
						{rotulo: "aporte", dica: "quanto paga por mês", obrigatorio: true},
					},
					executar: func(v []string) (string, error) {
						args := []string{"add"}
						args = append(args, par("--desc", v[0])...)
						args = append(args, par("--credor", v[1])...)
						args = append(args, par("--valor", v[2])...)
						args = append(args, par("--juros", v[3])...)
						args = append(args, par("--aporte", v[4])...)
						return exec(func() error { return app.Emergencia(conn, args) })
					},
				},
				{
					tecla: "p", rotulo: "ver plano",
					campos: []campo{{rotulo: "id", dica: "número da emergência", obrigatorio: true}},
					params: func(v []string) []string { return []string{"plano", v[0]} },
				},
				{
					tecla: "e", rotulo: "editar",
					campos: []campo{
						{rotulo: "id", dica: "número da emergência", obrigatorio: true},
						{rotulo: "descrição", dica: "vazio mantém"},
						{rotulo: "credor", dica: "vazio mantém"},
						{rotulo: "valor", dica: "vazio mantém"},
						{rotulo: "juros", dica: "% a.m., vazio mantém"},
						{rotulo: "aporte", dica: "vazio mantém"},
					},
					carregar: func(id string) ([]string, error) {
						var desc, credor string
						var valor, aporte int64
						var juros float64
						err := conn.QueryRow(`SELECT descricao, credor, valor_total, juros_mes, aporte_mensal FROM emergencias WHERE id = ?`, id).
							Scan(&desc, &credor, &valor, &juros, &aporte)
						if err != nil {
							return nil, err
						}
						return []string{desc, credor, valorForm(valor), fmt.Sprintf("%g", juros), valorForm(aporte)}, nil
					},
					executar: func(v []string) (string, error) {
						args := []string{"editar", v[0]}
						args = append(args, par("--desc", v[1])...)
						args = append(args, par("--credor", v[2])...)
						args = append(args, par("--valor", v[3])...)
						args = append(args, par("--juros", v[4])...)
						args = append(args, par("--aporte", v[5])...)
						return exec(func() error { return app.Emergencia(conn, args) })
					},
				},
				{
					tecla: "l", rotulo: "lista",
					params: func(_ []string) []string { return []string{"listar"} },
				},
				{
					tecla: "u", rotulo: "quitar",
					campos: []campo{{rotulo: "id", dica: "número da emergência", obrigatorio: true}},
					executar: func(v []string) (string, error) {
						return exec(func() error { return app.Emergencia(conn, []string{"quitar", v[0]}) })
					},
				},
				{
					tecla: "x", rotulo: "remover", confirma: true,
					campos: []campo{{rotulo: "id", dica: "número da emergência", obrigatorio: true}},
					executar: func(v []string) (string, error) {
						return exec(func() error { return app.Emergencia(conn, []string{"remover", v[0]}) })
					},
				},
			},
		},
		{
			titulo: "Planejamento",
			resumo: "limites de gasto por semana ou mês",
			padrao: []string{"status"},
			conteudo: func(p []string) (string, error) {
				return captura(func() error { return app.Plano(conn, p) })
			},
			acoes: []acao{
				{
					tecla: "a", rotulo: "novo plano",
					campos: []campo{
						{rotulo: "categoria", dica: "ex.: mercado", obrigatorio: true},
						{rotulo: "valor", dica: "limite do período", obrigatorio: true},
						{rotulo: "período", opcoes: opcoesFixas("mes", "mês", "semana", "semana")},
						{rotulo: "referência", dica: "AAAA-MM ou AAAA-Wnn (padrão: atual)"},
						{rotulo: "repetir", dica: "nº de períodos (padrão: 1)"},
					},
					executar: func(v []string) (string, error) {
						args := []string{"add"}
						args = append(args, par("--cat", v[0])...)
						args = append(args, par("--valor", v[1])...)
						args = append(args, par("--periodo", v[2])...)
						args = append(args, par("--ref", v[3])...)
						args = append(args, par("--repetir", v[4])...)
						return exec(func() error { return app.Plano(conn, args) })
					},
				},
				{
					tecla: "e", rotulo: "editar",
					campos: []campo{
						{rotulo: "id", dica: "número do plano", obrigatorio: true},
						{rotulo: "valor", dica: "novo limite, vazio mantém"},
						{rotulo: "categoria", dica: "vazio mantém", sugestoes: sugestoesCategorias(conn)},
					},
					carregar: func(id string) ([]string, error) {
						var cat string
						var limite int64
						err := conn.QueryRow(`SELECT limite, categoria FROM planejamentos WHERE id = ?`, id).
							Scan(&limite, &cat)
						if err != nil {
							return nil, err
						}
						return []string{valorForm(limite), cat}, nil
					},
					executar: func(v []string) (string, error) {
						args := []string{"editar", v[0]}
						args = append(args, par("--valor", v[1])...)
						args = append(args, par("--cat", v[2])...)
						return exec(func() error { return app.Plano(conn, args) })
					},
				},
				{
					tecla: "s", rotulo: "status",
					campos: []campo{
						{rotulo: "período", opcoes: opcoesFixas("mes", "mês", "semana", "semana")},
						{rotulo: "referência", dica: "opcional (padrão: atual)"},
					},
					params: func(v []string) []string {
						p := []string{"status"}
						p = append(p, par("--periodo", v[0])...)
						p = append(p, par("--ref", v[1])...)
						return p
					},
				},
				{
					tecla: "l", rotulo: "lista",
					params: func(_ []string) []string { return []string{"listar"} },
				},
				{
					tecla: "x", rotulo: "remover", confirma: true,
					campos: []campo{{rotulo: "id", dica: "número do plano", obrigatorio: true}},
					executar: func(v []string) (string, error) {
						return exec(func() error { return app.Plano(conn, []string{"remover", v[0]}) })
					},
				},
			},
		},
		{
			titulo: "Relatório",
			resumo: "análise do passado, por categoria",
			padrao: []string{"--meses", "6"},
			conteudo: func(p []string) (string, error) {
				return captura(func() error { return app.Relatorio(conn, p) })
			},
			acoes: []acao{
				{
					tecla: "m", rotulo: "meses",
					campos: []campo{{rotulo: "meses", dica: "1 a 36", obrigatorio: true}},
					params: func(v []string) []string { return []string{"--meses", v[0]} },
				},
			},
		},
		{
			titulo: "Estatísticas",
			resumo: "tendência, top gastos, saúde financeira",
			padrao: []string{"--meses", "6"},
			// dump completo (usado pela interface web); a TUI usa as abas abaixo
			conteudo: func(p []string) (string, error) {
				return captura(func() error { return app.Estatisticas(conn, p) })
			},
			// ←/→ alterna entre as seções da análise
			abas: []aba{
				{"Categorias", func(p []string) (string, error) {
					return captura(func() error { return app.EstatResumo(conn, mesesParam(p)) })
				}},
				{"Tendência", func(p []string) (string, error) {
					return captura(func() error { return app.EstatTendencia(conn, mesesParam(p)) })
				}},
				{"Top gastos", func(p []string) (string, error) {
					return captura(func() error { return app.EstatTopGastos(conn, mesesParam(p)) })
				}},
				{"Saúde", func(p []string) (string, error) {
					return captura(func() error { return app.EstatSaude(conn, mesesParam(p)) })
				}},
			},
			acoes: []acao{
				{
					tecla: "m", rotulo: "meses",
					campos: []campo{{rotulo: "meses", dica: "1 a 36", obrigatorio: true}},
					params: func(v []string) []string { return []string{"--meses", v[0]} },
				},
			},
		},
		{
			titulo: "Gráficos",
			resumo: "categorias, saldo, receitas × despesas",
			padrao: []string{"--meses", "6"},
			semSel: true, // rótulos numéricos de eixo não são IDs selecionáveis
			// dump completo (usado pela interface web); a TUI usa as abas abaixo
			conteudo: func(p []string) (string, error) {
				return captura(func() error { return app.Graficos(conn, p) })
			},
			// ←/→ alterna entre os tipos de gráfico
			abas: []aba{
				{"Categorias", func(p []string) (string, error) {
					return captura(func() error { return app.GraficoCategorias(conn, mesesParam(p)) })
				}},
				{"Receitas×Despesas", func(p []string) (string, error) {
					return captura(func() error { return app.GraficoRecDesp(conn, mesesParam(p)) })
				}},
				{"Saldo", func(p []string) (string, error) {
					return captura(func() error { return app.GraficoSaldo(conn, mesesParam(p)) })
				}},
				{"Grupos", func(p []string) (string, error) {
					return captura(func() error { return app.GraficoGrupos(conn, mesesParam(p)) })
				}},
			},
			acoes: []acao{
				{
					tecla: "m", rotulo: "meses",
					campos: []campo{{rotulo: "meses", dica: "1 a 36", obrigatorio: true}},
					params: func(v []string) []string { return []string{"--meses", v[0]} },
				},
			},
		},
		{
			titulo: "Previsão",
			resumo: "projeção de saldo futuro",
			padrao: []string{"--meses", "6"},
			conteudo: func(p []string) (string, error) {
				return captura(func() error { return app.Previsao(conn, p) })
			},
			acoes: []acao{
				{
					tecla: "m", rotulo: "meses",
					campos: []campo{{rotulo: "meses", dica: "1 a 36", obrigatorio: true}},
					params: func(v []string) []string { return []string{"--meses", v[0]} },
				},
			},
		},
		{
			titulo: "Simulação",
			resumo: "e se eu comprar isto?",
			conteudo: func(p []string) (string, error) {
				if len(p) == 0 {
					return textoSimulacaoVazio, nil
				}
				return captura(func() error { return app.Simular(conn, p) })
			},
			acoes: []acao{
				{
					tecla: "s", rotulo: "simular",
					campos: []campo{
						{rotulo: "descrição", dica: "ex.: Videogame"},
						{rotulo: "valor", dica: "preço total, ex.: 4.000,00", obrigatorio: true},
						{rotulo: "parcelas", dica: "ex.: 12 (vazio = à vista)"},
						{rotulo: "juros", dica: "% ao mês, opcional"},
						{rotulo: "entrada", dica: "valor à vista, opcional"},
					},
					params: func(v []string) []string {
						var p []string
						p = append(p, par("--desc", v[0])...)
						p = append(p, par("--valor", v[1])...)
						p = append(p, par("--parcelas", v[2])...)
						p = append(p, par("--juros", v[3])...)
						p = append(p, par("--entrada", v[4])...)
						return p
					},
				},
			},
		},
	}

	if modoEmpresa {
		telas = append(telas, telasEmpresa(conn, exec, nenhuma)...)
	}
	// "Como usar" fica sempre por último, em qualquer modo.
	telas = append(telas, tela{
		titulo:   "Como usar",
		resumo:   "documentação do sistema",
		conteudo: func(_ []string) (string, error) { return textoComoUsar, nil },
	})
	return telas
}

// telasEmpresa monta as telas extras de `prisma --empresa`: sócios, capital,
// imposto, investimento e lucro. Só aparecem em modoEmpresa, pra não poluir
// o menu de quem usa o Prisma só pra finanças pessoais.
func telasEmpresa(conn *sql.DB, exec func(func() error) (string, error), nenhuma opcao) []tela {
	return []tela{
		{
			titulo: "Sócios",
			resumo: "sócios da empresa e participação",
			conteudo: func(_ []string) (string, error) {
				return captura(func() error { return app.Socio(conn, []string{"listar"}) })
			},
			acoes: []acao{
				{
					tecla: "a", rotulo: "adicionar",
					campos: []campo{
						{rotulo: "nome", dica: "ex.: Você", obrigatorio: true},
						{rotulo: "participação", dica: "% do lucro/capital, ex.: 60", obrigatorio: true},
					},
					executar: func(v []string) (string, error) {
						args := []string{"add"}
						args = append(args, par("--nome", v[0])...)
						args = append(args, par("--participacao", v[1])...)
						return exec(func() error { return app.Socio(conn, args) })
					},
				},
				{
					tecla: "e", rotulo: "editar",
					campos: []campo{
						{rotulo: "id", dica: "número do sócio", obrigatorio: true},
						{rotulo: "nome", dica: "vazio mantém"},
						{rotulo: "participação", dica: "vazio mantém"},
					},
					carregar: func(id string) ([]string, error) {
						var nome string
						var participacao float64
						if err := conn.QueryRow(`SELECT nome, participacao FROM socios WHERE id = ?`, id).
							Scan(&nome, &participacao); err != nil {
							return nil, err
						}
						return []string{nome, fmt.Sprintf("%.1f", participacao)}, nil
					},
					executar: func(v []string) (string, error) {
						args := []string{"editar", v[0]}
						args = append(args, par("--nome", v[1])...)
						args = append(args, par("--participacao", v[2])...)
						return exec(func() error { return app.Socio(conn, args) })
					},
				},
				{
					tecla: "x", rotulo: "remover", confirma: true,
					campos: []campo{{rotulo: "id", dica: "número do sócio", obrigatorio: true}},
					executar: func(v []string) (string, error) {
						return exec(func() error { return app.Socio(conn, []string{"remover", v[0]}) })
					},
				},
			},
		},
		{
			titulo: "Capital",
			resumo: "aportes de capital social",
			conteudo: func(_ []string) (string, error) {
				return captura(func() error { return app.Capital(conn, []string{"listar"}) })
			},
			acoes: []acao{
				{
					tecla: "a", rotulo: "aportar",
					campos: []campo{
						{rotulo: "sócio", obrigatorio: true, opcoes: opcoesSocios(conn)},
						{rotulo: "valor", dica: "ex.: 5.000,00", obrigatorio: true},
						{rotulo: "conta", dica: "onde entrou o dinheiro", opcoes: opcoesVinculo(conn, "contas", nenhuma)},
						{rotulo: "data", dica: "opcional (padrão: hoje)"},
						{rotulo: "observação", dica: "opcional"},
					},
					executar: func(v []string) (string, error) {
						args := []string{"aportar"}
						args = append(args, par("--socio", v[0])...)
						args = append(args, par("--valor", v[1])...)
						args = append(args, par("--conta", v[2])...)
						args = append(args, par("--data", v[3])...)
						args = append(args, par("--obs", v[4])...)
						return exec(func() error { return app.Capital(conn, args) })
					},
				},
			},
		},
		{
			titulo: "Imposto",
			resumo: "impostos pagos pela empresa",
			conteudo: func(_ []string) (string, error) {
				return captura(func() error { return app.Imposto(conn, []string{"listar"}) })
			},
			acoes: []acao{
				{
					tecla: "a", rotulo: "lançar",
					campos: []campo{
						{rotulo: "descrição", dica: "ex.: DAS", obrigatorio: true},
						{rotulo: "valor", dica: "ex.: 250,00", obrigatorio: true},
						{rotulo: "vencimento", dica: "DD/MM/AAAA (padrão: hoje)"},
						{rotulo: "conta", opcoes: opcoesVinculo(conn, "contas", nenhuma)},
						{rotulo: "parcelas", dica: "divide o total em N parcelas (não use com \"todo mês\")"},
						{rotulo: "observação", dica: "opcional"},
						{rotulo: "todo mês?", dica: "vira uma regra de recorrência", opcoes: simNao()},
						{rotulo: "dia do mês", dica: "obrigatório se \"todo mês\" for sim"},
					},
					executar: func(v []string) (string, error) {
						args := []string{"add"}
						args = append(args, par("--desc", v[0])...)
						args = append(args, par("--valor", v[1])...)
						args = append(args, par("--venc", v[2])...)
						args = append(args, par("--conta", v[3])...)
						args = append(args, par("--parcelas", v[4])...)
						args = append(args, par("--obs", v[5])...)
						if sim(v[6]) {
							args = append(args, "--recorrente")
							args = append(args, par("--dia", v[7])...)
						}
						return exec(func() error { return app.Imposto(conn, args) })
					},
				},
			},
		},
		{
			titulo: "Investimento",
			resumo: "investimentos da empresa",
			conteudo: func(_ []string) (string, error) {
				return captura(func() error { return app.Investimento(conn, []string{"listar"}) })
			},
			acoes: []acao{
				{
					tecla: "a", rotulo: "lançar",
					campos: []campo{
						{rotulo: "descrição", dica: "ex.: Notebook", obrigatorio: true},
						{rotulo: "valor", dica: "ex.: 4.500,00", obrigatorio: true},
						{rotulo: "vencimento", dica: "DD/MM/AAAA (padrão: hoje)"},
						{rotulo: "conta", opcoes: opcoesVinculo(conn, "contas", nenhuma)},
						{rotulo: "parcelas", dica: "ex.: 12 (financiamento, vazio = à vista)"},
						{rotulo: "observação", dica: "opcional"},
					},
					executar: func(v []string) (string, error) {
						args := []string{"add"}
						args = append(args, par("--desc", v[0])...)
						args = append(args, par("--valor", v[1])...)
						args = append(args, par("--venc", v[2])...)
						args = append(args, par("--conta", v[3])...)
						args = append(args, par("--parcelas", v[4])...)
						args = append(args, par("--obs", v[5])...)
						return exec(func() error { return app.Investimento(conn, args) })
					},
				},
			},
		},
		{
			titulo: "Lucro",
			resumo: "cálculo e distribuição de lucro entre sócios",
			conteudo: func(_ []string) (string, error) {
				return captura(func() error { return app.Lucro(conn, []string{"listar"}) })
			},
			acoes: []acao{
				{
					tecla: "c", rotulo: "calcular",
					campos: []campo{
						{rotulo: "de", dica: "opcional (padrão: 1º dia do mês)"},
						{rotulo: "até", dica: "opcional (padrão: hoje)"},
					},
					executar: func(v []string) (string, error) {
						args := []string{"calcular"}
						args = append(args, par("--de", v[0])...)
						args = append(args, par("--ate", v[1])...)
						return exec(func() error { return app.Lucro(conn, args) })
					},
				},
				{
					tecla: "d", rotulo: "distribuir", confirma: true,
					campos: []campo{
						{rotulo: "valor", dica: "valor total a distribuir, ex.: 2.000,00", obrigatorio: true},
						{rotulo: "data", dica: "opcional (padrão: hoje)"},
						{rotulo: "observação", dica: "opcional"},
						{rotulo: "já pago?", opcoes: simNao()},
					},
					executar: func(v []string) (string, error) {
						args := []string{"distribuir"}
						args = append(args, par("--valor", v[0])...)
						args = append(args, par("--data", v[1])...)
						args = append(args, par("--obs", v[2])...)
						if sim(v[3]) {
							args = append(args, "--quitado")
						}
						return exec(func() error { return app.Lucro(conn, args) })
					},
				},
			},
		},
	}
}

// camposLancamento monta o formulário de novo lançamento, com conta e
// carteira como seletores (sem precisar saber o id de cor). O campo de
// reembolso (recebe-pagamento) só aparece para despesas (tipo "pagar"), já
// que receitas não podem ser divididas por grupo.
func camposLancamento(conn *sql.DB, nenhuma opcao, tipo string) []campo {
	// índice do campo "cartão" (8º), usado para ocultar o vencimento quando há cartão
	const idxCartao = 7
	campos := []campo{
		{rotulo: "descrição", dica: "ex.: Aluguel", obrigatorio: true},
		{rotulo: "valor", dica: "ex.: 1.200,00 (total, se parcelado)", obrigatorio: true},
		// no cartão o vencimento é o da fatura: sugere a data (campo travado)
		{rotulo: "vencimento", dica: "DD/MM/AAAA ou DD/MM (padrão: hoje)",
			auto: func(vals []string) string {
				if len(vals) <= idxCartao || vals[idxCartao] == "" {
					return "" // sem cartão: data normal (a data da compra)
				}
				if d := app.VencimentoFatura(conn, vals[idxCartao]); d != "" {
					return "fatura vence em " + d
				}
				return ""
			}},
		{rotulo: "categoria", dica: "ex.: moradia (padrão: geral)", sugestoes: sugestoesCategorias(conn)},
		{rotulo: "conta", opcoes: opcoesVinculo(conn, "contas", nenhuma)},
		{rotulo: "carteira", opcoes: opcoesVinculo(conn, "carteiras", nenhuma)},
		{rotulo: "grupo", dica: "divide a despesa entre as pessoas", opcoes: opcoesGrupos(conn, nenhuma)},
		{rotulo: "cartão", dica: "vai pra fatura (compra entra como hoje)", opcoes: opcoesCartoes(conn, nenhuma)},
		{rotulo: "repetir", dica: "repete o valor por N meses (não use com parcelas)"},
		{rotulo: "parcelas", dica: "divide o total em N parcelas (não use com repetir)"},
		{rotulo: "observação", dica: "opcional"},
		{rotulo: "quitar no vencimento?", dica: "quita sozinho ao vencer", opcoes: simNao()},
		{rotulo: "já quitado?", opcoes: simNao()},
	}
	if tipo == "pagar" {
		campos = append(campos, campo{
			rotulo: "outros do grupo te pagam?",
			dica:   "a despesa fica só com a sua parte; o resto vira receita de reembolso pendente",
			opcoes: simNao(),
		})
	}
	return campos
}

func argsLancamento(v []string, tipo string) []string {
	args := []string{"add"}
	args = append(args, par("--desc", v[0])...)
	args = append(args, par("--valor", v[1])...)
	args = append(args, par("--venc", v[2])...)
	args = append(args, par("--cat", v[3])...)
	args = append(args, par("--conta", v[4])...)
	args = append(args, par("--carteira", v[5])...)
	args = append(args, par("--grupo", v[6])...)
	args = append(args, par("--cartao", v[7])...)
	args = append(args, par("--repetir", v[8])...)
	args = append(args, par("--parcelas", v[9])...)
	args = append(args, par("--obs", v[10])...)
	if sim(v[11]) {
		args = append(args, "--auto-quitar")
	}
	if sim(v[12]) {
		args = append(args, "--quitado")
	}
	if tipo == "pagar" && len(v) > 13 && sim(v[13]) {
		args = append(args, "--recebe-pagamento")
	}
	return args
}
