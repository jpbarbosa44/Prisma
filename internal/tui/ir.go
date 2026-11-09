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

	"prisma/internal/app"
	"prisma/internal/money"
)

// A ajuda para o IR imprimível: a web serve em /api/ir (o app abre no
// navegador do sistema) e a CLI grava o mesmo HTML (`prisma ir --html arq`).

//go:embed ir.html
var irHTMLModelo string

var tmplIR = template.Must(template.New("ir").Funcs(template.FuncMap{
	"brl":  money.Format,
	"join": func(s []string) string { return strings.Join(s, ", ") },
	"data": func(d string) string {
		if len(d) < 10 {
			return d
		}
		return d[8:10] + "/" + d[5:7]
	},
}).Parse(irHTMLModelo))

type paginaIR struct {
	D                         app.DadosIR
	Gerado                    string
	Declaracao                int
	AnoAnterior, AnoProximo   int
	LinkAnterior, LinkProximo string
	TotalBens                 int64
}

// IRHTML escreve a ajuda para o IR do ano-calendário. link monta o endereço
// de outro ano (navegação ◀ ▶).
func IRHTML(conn *sql.DB, ano int, link func(ano int) string, w io.Writer) error {
	d, err := app.RelatorioIR(conn, ano)
	if err != nil {
		return err
	}
	p := paginaIR{D: d, Declaracao: d.Ano + 1, AnoAnterior: d.Ano - 1, AnoProximo: d.Ano + 1,
		LinkAnterior: link(d.Ano - 1), LinkProximo: link(d.Ano + 1),
		Gerado: strings.Replace(d.Gerado[8:10]+"/"+d.Gerado[5:7]+"/"+d.Gerado[:4]+d.Gerado[10:], " ", " às ", 1)}
	for _, b := range d.Bens {
		p.TotalBens += b.Atual
	}
	return tmplIR.Execute(w, p)
}

// SalvaIR grava a ajuda para o IR na pasta de downloads (na TUI).
func SalvaIR(conn *sql.DB, anoStr string) (string, error) {
	ano, err := app.AnoIR(anoStr)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	link := func(a int) string { return fmt.Sprintf("prisma-ir-%d.html", a) }
	if err := IRHTML(conn, ano, link, &b); err != nil {
		return "", err
	}
	arq := filepath.Join(pastaDownloads(), link(ano))
	if err := os.WriteFile(arq, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return arq, nil
}
