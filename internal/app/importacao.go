package app

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"prisma/internal/db"
)

// Importação em lote com prévia e desfazer — o caminho do app/web e do
// assistente (MCP). Cada importação vira um lote: a marca "[lote X]" vai na
// observação de cada lançamento, e desfazer apaga o lote inteiro. Antes de
// gravar, o banco ganha um snapshot em backups/<origem>-antes-*.db.

const (
	// MaxExtrato é o tamanho máximo de um extrato importado (2 MB).
	MaxExtrato = 2 << 20
	// maxSnapshots é quantos snapshots "<origem>-antes-*" ficam guardados.
	maxSnapshots = 10
)

// ReLote reconhece o nome de um lote: origem (mcp = assistente, web = app ou
// navegador) e o instante da gravação.
var ReLote = regexp.MustCompile(`^(mcp|web)-\d{8}-\d{6}$`)

// MarcaLote é o sufixo da observação que identifica o lote de importação.
func MarcaLote(lote string) string { return "[lote " + lote + "]" }

// NovoLote dá nome a um lote novo da origem (mcp ou web).
func NovoLote(origem string) string { return origem + "-" + time.Now().Format("20060102-150405") }

// DecodificaExtrato devolve o texto em UTF-8 e a codificação detectada:
// arquivos de banco brasileiros muitas vezes vêm em Latin-1/Windows-1252, que
// não é UTF-8 válido.
func DecodificaExtrato(b []byte) (texto, codificacao string) {
	b = []byte(strings.TrimPrefix(string(b), "\ufeff"))
	if utf8.Valid(b) {
		return string(b), "UTF-8"
	}
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r), "Latin-1 convertido para UTF-8"
}

// JaImportado diz se um lançamento igual (tipo, descrição, valor e data — de
// vencimento ou de compra) já está no banco: reimportar um extrato que se
// sobrepõe ao anterior não duplica nada.
func JaImportado(q dbtx, p LancamentoParams) (bool, error) {
	var n int
	err := q.QueryRow(`
		SELECT COUNT(*) FROM lancamentos
		WHERE tipo = ? AND valor = ? AND lower(trim(descricao)) = lower(?)
		  AND (vencimento = ? OR data_compra = ?)`,
		p.Tipo, p.Valor, p.Desc, p.Venc, p.Venc).Scan(&n)
	return n > 0, err
}

// SnapshotAntes copia o banco local (banco = caminho do arquivo) para
// backups/<origem>-antes-AAAAMMDD-HHMMSS.db antes de uma gravação em lote,
// guardando os maxSnapshots mais recentes daquela origem.
func SnapshotAntes(banco, origem string) (string, error) {
	dir := filepath.Join(filepath.Dir(banco), "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	alvo := filepath.Join(dir, origem+"-antes-"+time.Now().Format("20060102-150405")+".db")
	if _, err := os.Stat(alvo); err == nil {
		return alvo, nil // já tirado neste mesmo segundo
	}
	if err := db.Snapshot(banco, alvo); err != nil {
		return "", err
	}
	if nomes, err := filepath.Glob(filepath.Join(dir, origem+"-antes-*.db")); err == nil && len(nomes) > maxSnapshots {
		sort.Strings(nomes)
		for _, n := range nomes[:len(nomes)-maxSnapshots] {
			os.Remove(n)
		}
	}
	return alvo, nil
}

// DesfazerLote apaga todos os lançamentos de um lote; devolve quantos.
func DesfazerLote(conn *sql.DB, lote string) (int64, error) {
	if !ReLote.MatchString(lote) {
		return 0, fmt.Errorf("lote inválido %q (formato: mcp-AAAAMMDD-HHMMSS ou web-AAAAMMDD-HHMMSS)", lote)
	}
	res, err := conn.Exec(`DELETE FROM lancamentos WHERE observacao LIKE ?`, "%"+MarcaLote(lote))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return 0, fmt.Errorf("nenhum lançamento do lote %s", lote)
	}
	return n, nil
}

// Lote resume uma importação gravada.
type Lote struct {
	Lote     string `json:"lote"`
	Itens    int    `json:"itens"`
	Despesas int64  `json:"despesas"` // centavos
	Receitas int64  `json:"receitas"` // centavos
	De       string `json:"de"`
	Ate      string `json:"ate"`
}

// Lotes lista as importações gravadas, da mais recente para a mais antiga.
func Lotes(conn *sql.DB) ([]Lote, error) {
	rows, err := conn.Query(`
		SELECT substr(observacao, instr(observacao, '[lote ') + 6, 19) AS lote, COUNT(*),
		       COALESCE(SUM(CASE tipo WHEN 'pagar' THEN valor ELSE 0 END), 0),
		       COALESCE(SUM(CASE tipo WHEN 'receber' THEN valor ELSE 0 END), 0),
		       MIN(vencimento), MAX(vencimento)
		FROM lancamentos WHERE observacao LIKE '%[lote mcp-%' OR observacao LIKE '%[lote web-%'
		GROUP BY lote ORDER BY substr(lote, 5) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Lote{}
	for rows.Next() {
		var l Lote
		if err := rows.Scan(&l.Lote, &l.Itens, &l.Despesas, &l.Receitas, &l.De, &l.Ate); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ItemExtrato é uma linha do extrato como entraria no banco.
type ItemExtrato struct {
	Data      string `json:"data"`
	Descricao string `json:"descricao"`
	Tipo      string `json:"tipo"`  // pagar (saída) ou receber (entrada)
	Valor     int64  `json:"valor"` // centavos, positivo
	Categoria string `json:"categoria"`
	Duplicado bool   `json:"duplicado"` // já está no banco: será ignorado
}

// PreviaExtrato é o que uma importação de extrato faria, sem gravar nada.
type PreviaExtrato struct {
	Itens       []ItemExtrato `json:"itens"`
	Novos       int           `json:"novos"`
	Duplicados  int           `json:"duplicados"`
	Entradas    int64         `json:"entradas"` // só dos novos
	Saidas      int64         `json:"saidas"`   // só dos novos
	Codificacao string        `json:"codificacao"`
	Destino     string        `json:"destino"` // "conta Nubank"
}

// DestinoExtrato é onde os movimentos do extrato entram: uma conta OU uma carteira.
type DestinoExtrato struct {
	ContaID, CarteiraID int64
}

// rotulo confere que o destino existe e devolve o nome para a prévia.
func (d DestinoExtrato) rotulo(conn *sql.DB) (string, error) {
	tabela, id, nome := "contas", d.ContaID, "conta"
	if (d.ContaID == 0) == (d.CarteiraID == 0) {
		return "", fmt.Errorf("escolha uma conta OU uma carteira de destino")
	}
	if d.CarteiraID != 0 {
		tabela, id, nome = "carteiras", d.CarteiraID, "carteira"
	}
	var n string
	if err := conn.QueryRow(`SELECT nome FROM `+tabela+` WHERE id = ?`, id).Scan(&n); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("%s #%d não encontrada", nome, id)
		}
		return "", err
	}
	return nome + " " + n, nil
}

// PreverExtrato lê o extrato (nome decide OFX × CSV) e mostra o que entraria
// no destino: os movimentos viram lançamentos quitados (saída = pagar,
// entrada = receber) na categoria cat ("" = a da regra de categoria que casar,
// ou importado); os já existentes
// ficam marcados como duplicados.
func PreverExtrato(conn *sql.DB, nome string, bruto []byte, dest DestinoExtrato, cat string) (PreviaExtrato, error) {
	var p PreviaExtrato
	if len(bruto) > MaxExtrato {
		return p, fmt.Errorf("arquivo grande demais (máximo 2 MB)")
	}
	switch strings.ToLower(filepath.Ext(nome)) {
	case ".csv", ".ofx", ".txt", ".tsv":
	default:
		return p, fmt.Errorf("formato não suportado: use o extrato em .ofx ou .csv")
	}
	var err error
	if p.Destino, err = dest.rotulo(conn); err != nil {
		return p, err
	}
	texto, codif := DecodificaExtrato(bruto)
	p.Codificacao = codif
	movs, err := InterpretaExtrato(nome, texto)
	if err != nil {
		return p, err
	}
	regras, err := listaRegras(conn)
	if err != nil {
		return p, err
	}
	p.Itens = []ItemExtrato{}
	for _, m := range movs {
		desc := strings.Join(strings.Fields(m.Descricao), " ")
		it := ItemExtrato{Data: m.Data, Descricao: desc, Tipo: "receber", Valor: m.Valor, Categoria: categoriaImportada(regras, desc, cat)}
		if m.Valor < 0 {
			it.Tipo, it.Valor = "pagar", -m.Valor
		}
		if it.Valor == 0 {
			continue
		}
		if it.Duplicado, err = JaImportado(conn, LancamentoParams{Tipo: it.Tipo, Desc: it.Descricao, Valor: it.Valor, Venc: it.Data}); err != nil {
			return p, err
		}
		if it.Duplicado {
			p.Duplicados++
		} else {
			p.Novos++
			if it.Tipo == "pagar" {
				p.Saidas += it.Valor
			} else {
				p.Entradas += it.Valor
			}
		}
		p.Itens = append(p.Itens, it)
	}
	if len(p.Itens) == 0 {
		return p, fmt.Errorf("nenhum movimento reconhecido no arquivo")
	}
	return p, nil
}

// GravarExtrato grava os itens novos de uma prévia num lote (origem "web" ou
// "mcp"), tudo numa transação: ou o extrato entra inteiro, ou nada entra.
func GravarExtrato(conn *sql.DB, p PreviaExtrato, dest DestinoExtrato, origem string) (lote string, criados int, err error) {
	var conta, carteira any
	if dest.ContaID != 0 {
		conta = dest.ContaID
	} else {
		carteira = dest.CarteiraID
	}
	lote = NovoLote(origem)
	tx, err := conn.Begin()
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()
	for _, it := range p.Itens {
		// confere de novo dentro da transação: o banco pode ter mudado desde a prévia
		dup, err := JaImportado(tx, LancamentoParams{Tipo: it.Tipo, Desc: it.Descricao, Valor: it.Valor, Venc: it.Data})
		if err != nil {
			return "", 0, err
		}
		if dup {
			continue
		}
		if _, err := tx.Exec(`
			INSERT INTO lancamentos (tipo, descricao, valor, categoria, vencimento, status, quitado_em, conta_id, carteira_id, observacao)
			VALUES (?,?,?,?,?,'quitado',?,?,?,?)`,
			it.Tipo, it.Descricao, it.Valor, it.Categoria, it.Data, it.Data, conta, carteira, MarcaLote(lote)); err != nil {
			return "", 0, err
		}
		criados++
	}
	if criados == 0 {
		return "", 0, fmt.Errorf("nada a gravar: todos os movimentos já estão no banco")
	}
	if err := tx.Commit(); err != nil {
		return "", 0, err
	}
	for _, it := range p.Itens {
		registraCategoria(conn, it.Categoria)
	}
	return lote, criados, nil
}
