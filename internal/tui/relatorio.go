package tui

import (
	"database/sql"
	_ "embed"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"prisma/internal/app"
	"prisma/internal/money"
)

// O relatório mensal imprimível: a web serve em /api/relatorio (o app abre no
// navegador do sistema, que imprime em PDF) e a CLI grava o mesmo HTML num
// arquivo (`prisma relatorio --html arq.html`).

//go:embed relatorio.html
var relatorioHTML string

var mesesPT = [...]string{"janeiro", "fevereiro", "março", "abril", "maio", "junho",
	"julho", "agosto", "setembro", "outubro", "novembro", "dezembro"}

var tmplRelatorio = template.Must(template.New("relatorio").Funcs(template.FuncMap{
	"brl": money.Format,
	"data": func(d string) string {
		if len(d) < 10 {
			return d
		}
		return d[8:10] + "/" + d[5:7]
	},
}).Parse(relatorioHTML))

// barraRel é uma coluna dupla (receitas, despesas) do gráfico de 6 meses.
type barraRel struct {
	Rotulo                   string
	Rec, Desp                int64
	X, X2, XRotulo           int
	YRec, HRec, YDesp, HDesp int
}

// catRel é uma linha da tabela de categorias.
type catRel struct {
	app.CategoriaMes
	Pct, Largura int
	Seta, Classe string // ↑ desp (gastou mais que a média), ↓ rec, = neutro
}

// paginaRel é o que o template recebe.
type paginaRel struct {
	D                         app.DadosRelatorio
	Titulo, Gerado            string
	Barras                    []barraRel
	Categorias                []catRel
	MesAnterior, MesProximo   string
	LinkAnterior, LinkProximo string
}

func nomeMes(t time.Time) string { return mesesPT[t.Month()-1] + "/" + fmt.Sprint(t.Year()) }

// RelatorioHTML escreve o fechamento do mês (AAAA-MM; "" = o atual) como uma
// página para imprimir. link monta o endereço de outro mês (navegação ◀ ▶).
func RelatorioHTML(conn *sql.DB, mes string, link func(mes string) string, w io.Writer) error {
	d, err := app.RelatorioMensal(conn, mes)
	if err != nil {
		return err
	}
	t, _ := time.Parse("2006-01", d.Mes)
	ant, prox := t.AddDate(0, -1, 0), t.AddDate(0, 1, 0)
	p := paginaRel{
		D: d, Titulo: nomeMes(t), Gerado: strings.Replace(d.Gerado[8:10]+"/"+d.Gerado[5:7]+"/"+d.Gerado[:4]+d.Gerado[10:], " ", " às ", 1),
		MesAnterior: nomeMes(ant), MesProximo: nomeMes(prox),
		LinkAnterior: link(ant.Format("2006-01")), LinkProximo: link(prox.Format("2006-01")),
	}

	// gráfico: 6 pares de barras numa área de 760×145
	var maior int64 = 1
	for _, m := range d.Mensal {
		maior = max(maior, m.Rec, m.Desp)
	}
	const alto, base = 130, 145
	for i, m := range d.Mensal {
		mt, _ := time.Parse("2006-01", m.Mes)
		hr, hd := int(m.Rec*alto/maior), int(m.Desp*alto/maior)
		x := 30 + i*125
		p.Barras = append(p.Barras, barraRel{
			Rotulo: mesesPT[mt.Month()-1][:3] + "/" + fmt.Sprint(mt.Year())[2:], Rec: m.Rec, Desp: m.Desp,
			X: x, X2: x + 38, XRotulo: x + 36, YRec: base - hr, HRec: hr, YDesp: base - hd, HDesp: hd,
		})
	}

	var topo int64 = 1
	if len(d.Categorias) > 0 {
		topo = max(topo, d.Categorias[0].Valor)
	}
	for _, c := range d.Categorias {
		r := catRel{CategoriaMes: c, Largura: int(c.Valor * 100 / topo)}
		if d.Despesas > 0 {
			r.Pct = int(c.Valor * 100 / d.Despesas)
		}
		if c.Media > 0 {
			switch {
			case c.Valor > c.Media*11/10: // 10% acima da média
				r.Seta, r.Classe = "↑", "desp"
			case c.Valor < c.Media*9/10:
				r.Seta, r.Classe = "↓", "rec"
			default:
				r.Seta, r.Classe = "=", "dim"
			}
		}
		p.Categorias = append(p.Categorias, r)
	}
	return tmplRelatorio.Execute(w, p)
}

// SalvaRelatorio grava o fechamento do mês em HTML na pasta de downloads e
// devolve o caminho (na TUI, que não tem navegador embutido).
func SalvaRelatorio(conn *sql.DB, mes string) (string, error) {
	if mes == "" {
		mes = time.Now().Format("2006-01")
	}
	var b strings.Builder
	link := func(m string) string { return "prisma-fechamento-" + m + ".html" }
	if err := RelatorioHTML(conn, mes, link, &b); err != nil {
		return "", err
	}
	arq := filepath.Join(pastaDownloads(), link(mes))
	if err := os.WriteFile(arq, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return arq, nil
}
