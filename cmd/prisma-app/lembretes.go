package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"prisma/internal/app"
	"prisma/internal/money"
	"prisma/internal/notifica"
)

// horaLembrete é a partir de quando o lembrete do dia sai (como o do bot).
const horaLembrete = 9

// vigiaLembretes confere, ao abrir e a cada 10 minutos enquanto a janela está
// aberta, se é hora do lembrete do dia: uma notificação do sistema com as
// contas atrasadas, de hoje e de amanhã — uma vez por dia.
func vigiaLembretes(conn *sql.DB, pare <-chan struct{}) {
	tick := time.NewTicker(10 * time.Minute)
	defer tick.Stop()
	for {
		lembreteDoDia(conn, time.Now())
		select {
		case <-pare:
			return
		case <-tick.C:
		}
	}
}

func lembreteDoDia(conn *sql.DB, agora time.Time) {
	if agora.Hour() < horaLembrete {
		return
	}
	hoje := agora.Format("2006-01-02")
	muEstado.Lock()
	e := carregaEstado()
	if e.SemLembretes || e.UltimoAviso == hoje {
		muEstado.Unlock()
		return
	}
	e.UltimoAviso = hoje
	salvaEstado(e) // marca antes: falhando o envio, não insiste o dia todo
	muEstado.Unlock()

	avisos, err := app.Vencimentos(conn, agora, 1)
	if err != nil {
		return
	}
	atencao, _ := app.AtencaoDoDia(conn, agora, "app")
	if len(avisos) == 0 && len(atencao) == 0 {
		return
	}
	titulo, texto := textoLembrete(avisos)
	if len(avisos) == 0 {
		titulo = "Prisma: atenção"
	}
	for _, a := range atencao {
		if texto != "" {
			texto += "\n"
		}
		texto += "⚠ " + a
	}
	notifica.Enviar(titulo, texto)
}

// textoLembrete resume os vencimentos numa notificação curta.
func textoLembrete(avisos []app.Aviso) (titulo, texto string) {
	var atrasadas, hoje int
	for _, a := range avisos {
		switch a.Situacao {
		case app.SituacaoAtrasada:
			atrasadas++
		case app.SituacaoHoje:
			hoje++
		}
	}
	switch {
	case atrasadas > 0:
		titulo = fmt.Sprintf("Prisma: %d conta(s) atrasada(s)", atrasadas)
	case hoje > 0:
		titulo = fmt.Sprintf("Prisma: %d conta(s) vencem hoje", hoje)
	default:
		titulo = fmt.Sprintf("Prisma: %d conta(s) vencem amanhã", len(avisos))
	}
	quando := map[string]string{app.SituacaoAtrasada: "atrasada", app.SituacaoHoje: "hoje", app.SituacaoProxima: "amanhã"}
	var linhas []string
	for i, a := range avisos {
		if i == 4 {
			linhas = append(linhas, fmt.Sprintf("e mais %d…", len(avisos)-4))
			break
		}
		verbo := ""
		if a.Tipo == "receber" {
			verbo = "receber: "
		}
		linhas = append(linhas, fmt.Sprintf("%s%s %s (%s)", verbo, a.Descricao, money.Format(a.Valor), quando[a.Situacao]))
	}
	return titulo, strings.Join(linhas, "\n")
}
