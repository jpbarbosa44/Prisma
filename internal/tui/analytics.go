package tui

import (
	"database/sql"

	"prisma/internal/app"
)

// novasTelasAnalytics monta o menu do módulo Prisma Analytics: telas só de
// visualização (somente leitura, sem CRUD de transações — RNF03 e restrições do
// escopo), uma por requisito funcional (RF). Metas (RF05) e Simulador (RF07)
// aceitam parâmetros do usuário via formulário, mas só analisam — nada é gravado.
func novasTelasAnalytics(conn *sql.DB) []tela {
	// vis cria uma tela de visualização a partir de uma função de análise.
	vis := func(titulo, resumo string, f func() error) tela {
		return tela{
			titulo:   titulo,
			resumo:   resumo,
			conteudo: func(_ []string) (string, error) { return captura(f) },
		}
	}

	// periodo cria uma tela com janela ajustável: a ação "p" pede os meses e
	// os repassa à análise como parâmetro (vazio = padrão da análise).
	periodo := func(titulo, resumo, dica string, f func(*sql.DB, ...string) error) tela {
		return tela{
			titulo:   titulo,
			resumo:   resumo,
			conteudo: func(p []string) (string, error) { return captura(func() error { return f(conn, p...) }) },
			acoes: []acao{{
				tecla: "p", rotulo: "período",
				campos: []campo{{rotulo: "meses", dica: dica}},
				params: func(v []string) []string { return []string{v[0]} },
			}},
		}
	}

	return []tela{
		vis("Hoje", "o que merece atenção", func() error { return app.AnalyticsHoje(conn) }),
		periodo("Health Score", "saúde financeira 0–100", "1 a 36 (padrão 6)", app.AnalyticsHealthScore),
		periodo("Ritmo do Mês", "gasto até hoje x normal", "meses de base, 1 a 36 (padrão 6)", app.AnalyticsRitmo),
		vis("Dia Mais Apertado", "saldo dia a dia", func() error { return app.AnalyticsDiaApertado(conn) }),
		periodo("Modo Economia", "anomalias de gasto", "meses de base, 1 a 36 (padrão 6)", app.AnalyticsAnomalias),
		periodo("Tendências", "vazamentos lentos", "meses fechados, 1 a 36 (padrão 6)", app.AnalyticsTendencias),
		{
			titulo: "Gastos Formiga",
			resumo: "pequenos gastos repetidos",
			conteudo: func(p []string) (string, error) {
				return captura(func() error { return app.AnalyticsFormiga(conn, p...) })
			},
			acoes: []acao{{
				tecla: "l", rotulo: "período e limite",
				campos: []campo{
					{rotulo: "meses", dica: "1 a 36 (padrão 3)"},
					{rotulo: "limite", dica: "valor máximo, ex.: 50,00"},
				},
				params: func(v []string) []string { return []string{v[0], v[1]} },
			}},
		},
		vis("Sazonalidade", "picos anuais", func() error { return app.AnalyticsSazonalidade(conn) }),
		vis("Runway", "projeção de caixa", func() error { return app.AnalyticsRunway(conn) }),
		periodo("Renda Comprometida", "parcelas e agendados", "meses à frente, 1 a 36 (padrão 6)", app.AnalyticsComprometimento),
		{
			titulo: "Metas",
			resumo: "viabilidade de metas",
			conteudo: func(p []string) (string, error) {
				return captura(func() error { return app.AnalyticsMetas(conn, p) })
			},
			acoes: []acao{{
				tecla: "m", rotulo: "definir meta",
				campos: []campo{
					{rotulo: "valor", dica: "ex.: 50.000,00", obrigatorio: true},
					{rotulo: "prazo", dica: "em meses, ex.: 24", obrigatorio: true},
				},
				params: func(v []string) []string { return []string{v[0], v[1]} },
			}},
		},
		vis("Assinaturas Ocultas", "recorrências prováveis", func() error { return app.AnalyticsAssinaturasOcultas(conn) }),
		{
			titulo: "Simulador",
			resumo: "what-if",
			conteudo: func(p []string) (string, error) {
				return captura(func() error { return app.AnalyticsSimulador(conn, p) })
			},
			acoes: []acao{{
				tecla: "s", rotulo: "simular",
				campos: []campo{
					{rotulo: "perda de renda", dica: "queda mensal, ex.: 2.000,00"},
					{rotulo: "nova despesa", dica: "despesa fixa nova/mês, ex.: 500,00"},
				},
				params: func(v []string) []string { return []string{v[0], v[1]} },
			}},
		},
		vis("Inflação Pessoal", "custo de vida", func() error { return app.AnalyticsInflacao(conn) }),
		periodo("Regra 50/30/20", "auditoria macro", "1 a 36 (padrão 6)", app.AnalyticsRegra502030),
		vis("Aderência a Planos", "planejado x realizado", func() error { return app.AnalyticsAderenciaPlanos(conn) }),
		vis("Patrimônio Líquido", "net worth", func() error { return app.AnalyticsPatrimonio(conn) }),
		vis("Balanço dos Grupos", "divisão de contas", func() error { return app.AnalyticsGrupos(conn) }),
		periodo("Eficiência", "contas da casa", "1 a 36 (padrão 6)", app.AnalyticsUtilidades),
		vis("Melhor Dia de Compra", "prazo dos cartões", func() error { return app.AnalyticsMelhorDiaCartao(conn) }),
		{
			titulo:   "Como usar",
			resumo:   "documentação do módulo",
			conteudo: func(_ []string) (string, error) { return textoComoUsarAnalytics, nil },
		},
	}
}
