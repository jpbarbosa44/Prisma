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

	return []tela{
		vis("Health Score", "saúde financeira 0–100", func() error { return app.AnalyticsHealthScore(conn) }),
		vis("Modo Economia", "anomalias de gasto", func() error { return app.AnalyticsAnomalias(conn) }),
		vis("Sazonalidade", "picos anuais", func() error { return app.AnalyticsSazonalidade(conn) }),
		vis("Runway", "projeção de caixa", func() error { return app.AnalyticsRunway(conn) }),
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
		vis("Regra 50/30/20", "auditoria macro", func() error { return app.AnalyticsRegra502030(conn) }),
		vis("Patrimônio Líquido", "net worth", func() error { return app.AnalyticsPatrimonio(conn) }),
		vis("Eficiência", "contas da casa", func() error { return app.AnalyticsUtilidades(conn) }),
		{
			titulo:   "Como usar",
			resumo:   "documentação do módulo",
			conteudo: func(_ []string) (string, error) { return textoComoUsarAnalytics, nil },
		},
	}
}
