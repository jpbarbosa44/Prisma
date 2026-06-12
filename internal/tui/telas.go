package tui

import (
	"database/sql"
	"fmt"
	"strings"

	"prisma/internal/app"
)

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
func novasTelas(conn *sql.DB) []tela {
	exec := func(f func() error) (string, error) { return captura(f) }
	nenhuma := opcao{"", "nenhuma"}
	manter := opcao{"", "manter"}
	desvincular := opcao{"0", "desvincular"}

	return []tela{
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
			titulo: "Pagar/Receber",
			resumo: "lançamentos e quitação",
			conteudo: func(p []string) (string, error) {
				return captura(func() error { return app.Lancamentos(conn, p) })
			},
			acoes: []acao{
				{
					tecla: "p", rotulo: "a pagar",
					campos: camposLancamento(conn, nenhuma),
					executar: func(v []string) (string, error) {
						return exec(func() error { return app.NovoLancamento(conn, "pagar", argsLancamento(v)) })
					},
				},
				{
					tecla: "r", rotulo: "a receber",
					campos: camposLancamento(conn, nenhuma),
					executar: func(v []string) (string, error) {
						return exec(func() error { return app.NovoLancamento(conn, "receber", argsLancamento(v)) })
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
						{rotulo: "descrição", dica: "vazio mantém"},
						{rotulo: "valor", dica: "vazio mantém"},
						{rotulo: "vencimento", dica: "vazio mantém"},
						{rotulo: "categoria", dica: "vazio mantém"},
						{rotulo: "conta", opcoes: opcoesVinculo(conn, "contas", manter, desvincular)},
						{rotulo: "carteira", opcoes: opcoesVinculo(conn, "carteiras", manter, desvincular)},
					},
					executar: func(v []string) (string, error) {
						args := []string{"editar", v[0]}
						args = append(args, par("--desc", v[1])...)
						args = append(args, par("--valor", v[2])...)
						args = append(args, par("--venc", v[3])...)
						args = append(args, par("--cat", v[4])...)
						args = append(args, par("--conta", v[5])...)
						args = append(args, par("--carteira", v[6])...)
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
						{rotulo: "categoria", dica: "opcional"},
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
						{rotulo: "categoria", dica: "opcional"},
						{rotulo: "conta", opcoes: opcoesVinculo(conn, "contas", nenhuma)},
						{rotulo: "carteira", opcoes: opcoesVinculo(conn, "carteiras", nenhuma)},
						{rotulo: "início", dica: "opcional (padrão: hoje)"},
						{rotulo: "fim", dica: "opcional (vazio = sem fim)"},
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
						args = append(args, par("--inicio", v[7])...)
						args = append(args, par("--fim", v[8])...)
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
						{rotulo: "categoria", dica: "vazio mantém"},
						{rotulo: "conta", opcoes: opcoesVinculo(conn, "contas", manter, desvincular)},
						{rotulo: "carteira", opcoes: opcoesVinculo(conn, "carteiras", manter, desvincular)},
						{rotulo: "fim", dica: "data, \"nunca\" remove, vazio mantém"},
					},
					executar: func(v []string) (string, error) {
						args := []string{"editar", v[0]}
						args = append(args, par("--desc", v[1])...)
						args = append(args, par("--valor", v[2])...)
						args = append(args, par("--dia", v[3])...)
						args = append(args, par("--cat", v[4])...)
						args = append(args, par("--conta", v[5])...)
						args = append(args, par("--carteira", v[6])...)
						args = append(args, par("--fim", v[7])...)
						return exec(func() error { return app.Recorrencia(conn, args) })
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
						{rotulo: "categoria", dica: "vazio mantém"},
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
	}
}

// camposLancamento monta o formulário de novo lançamento, com conta e
// carteira como seletores (sem precisar saber o id de cor).
func camposLancamento(conn *sql.DB, nenhuma opcao) []campo {
	return []campo{
		{rotulo: "descrição", dica: "ex.: Aluguel", obrigatorio: true},
		{rotulo: "valor", dica: "ex.: 1.200,00 (total, se parcelado)", obrigatorio: true},
		{rotulo: "vencimento", dica: "DD/MM/AAAA (padrão: hoje)"},
		{rotulo: "categoria", dica: "ex.: moradia (padrão: geral)"},
		{rotulo: "conta", opcoes: opcoesVinculo(conn, "contas", nenhuma)},
		{rotulo: "carteira", opcoes: opcoesVinculo(conn, "carteiras", nenhuma)},
		{rotulo: "repetir", dica: "repete o valor por N meses"},
		{rotulo: "parcelas", dica: "divide o total em N parcelas"},
		{rotulo: "já quitado?", opcoes: simNao()},
	}
}

func argsLancamento(v []string) []string {
	args := []string{"add"}
	args = append(args, par("--desc", v[0])...)
	args = append(args, par("--valor", v[1])...)
	args = append(args, par("--venc", v[2])...)
	args = append(args, par("--cat", v[3])...)
	args = append(args, par("--conta", v[4])...)
	args = append(args, par("--carteira", v[5])...)
	args = append(args, par("--repetir", v[6])...)
	args = append(args, par("--parcelas", v[7])...)
	if sim(v[8]) {
		args = append(args, "--quitado")
	}
	return args
}
