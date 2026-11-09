package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Anexos: comprovantes (foto ou PDF) guardados junto a um lançamento. O
// arquivo fica na pasta "anexos" ao lado do banco; o banco guarda só o nome.
// Remover o lançamento remove o anexo (CASCADE); na próxima limpeza, o arquivo
// órfão vai para anexos/.lixeira, onde fica 60 dias — restaurar um backup
// dentro desse prazo traz o anexo de volta.

// MaxAnexo é o tamanho máximo de um anexo.
const MaxAnexo = 10 << 20

// tiposAnexo são os tipos aceitos (detectados pelo conteúdo, não pelo nome).
var tiposAnexo = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/gif":       ".gif",
	"image/webp":      ".webp",
	"application/pdf": ".pdf",
}

// Anexo é um arquivo guardado.
type Anexo struct {
	ID         int64  `json:"id"`
	Lancamento int64  `json:"lancamento"`
	Nome       string `json:"nome"`
	Arquivo    string `json:"-"`
	Tipo       string `json:"tipo"`
	Tamanho    int64  `json:"tamanho"`
	CriadoEm   string `json:"criadoEm"`
}

// PastaAnexos é a pasta dos anexos deste banco (ao lado do arquivo .db). No
// modo cliente não há arquivo local: os anexos não ficam disponíveis.
func PastaAnexos(conn *sql.DB) (string, error) {
	var f string
	if err := conn.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&f); err != nil || f == "" {
		return "", errors.New("anexos só funcionam com o banco neste computador")
	}
	return filepath.Join(filepath.Dir(f), "anexos"), nil
}

var reNomeRuim = regexp.MustCompile(`[^\p{L}\p{N}._-]+`)

// nomeSeguro tira do nome o que não serve em arquivo (e qualquer caminho).
func nomeSeguro(nome string) string {
	nome = filepath.Base(strings.ReplaceAll(nome, "\\", "/"))
	nome = strings.Trim(reNomeRuim.ReplaceAllString(nome, "-"), "-.")
	if r := []rune(nome); len(r) > 60 {
		nome = string(r[len(r)-60:])
	}
	if nome == "" {
		nome = "comprovante"
	}
	return nome
}

// Anexar guarda um arquivo no lançamento.
func Anexar(conn *sql.DB, lancamento int64, nome string, dados []byte) (Anexo, error) {
	if len(dados) == 0 {
		return Anexo{}, errors.New("arquivo vazio")
	}
	if len(dados) > MaxAnexo {
		return Anexo{}, fmt.Errorf("arquivo grande demais (máximo %d MB)", MaxAnexo>>20)
	}
	tipo := strings.Split(http.DetectContentType(dados), ";")[0]
	ext, ok := tiposAnexo[tipo]
	if !ok {
		return Anexo{}, fmt.Errorf("tipo de arquivo não aceito (%s): use foto (JPG, PNG, WEBP) ou PDF", tipo)
	}
	var existe int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM lancamentos WHERE id = ?`, lancamento).Scan(&existe); err != nil {
		return Anexo{}, err
	}
	if existe == 0 {
		return Anexo{}, fmt.Errorf("lançamento #%d não encontrado", lancamento)
	}
	pasta, err := PastaAnexos(conn)
	if err != nil {
		return Anexo{}, err
	}
	if err := os.MkdirAll(pasta, 0o700); err != nil {
		return Anexo{}, err
	}
	nome = nomeSeguro(nome)
	if !strings.EqualFold(filepath.Ext(nome), ext) && !(ext == ".jpg" && strings.EqualFold(filepath.Ext(nome), ".jpeg")) {
		nome = strings.TrimSuffix(nome, filepath.Ext(nome)) + ext
	}
	sorte := make([]byte, 4)
	rand.Read(sorte)
	arquivo := fmt.Sprintf("%d-%s-%s", lancamento, hex.EncodeToString(sorte), nome)
	caminho := filepath.Join(pasta, arquivo)
	if err := os.WriteFile(caminho, dados, 0o600); err != nil {
		return Anexo{}, err
	}
	res, err := conn.Exec(`INSERT INTO anexos (lancamento_id, nome, arquivo, tipo, tamanho) VALUES (?,?,?,?,?)`,
		lancamento, nome, arquivo, tipo, len(dados))
	if err != nil {
		os.Remove(caminho)
		return Anexo{}, err
	}
	id, _ := res.LastInsertId()
	return Anexo{ID: id, Lancamento: lancamento, Nome: nome, Arquivo: arquivo, Tipo: tipo, Tamanho: int64(len(dados))}, nil
}

// AnexosDe lista os anexos de um lançamento (0 = de todos).
func AnexosDe(conn *sql.DB, lancamento int64) ([]Anexo, error) {
	q := `SELECT id, lancamento_id, nome, arquivo, tipo, tamanho, criado_em FROM anexos`
	var args []any
	if lancamento > 0 {
		q += ` WHERE lancamento_id = ?`
		args = append(args, lancamento)
	}
	rows, err := conn.Query(q+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Anexo{}
	for rows.Next() {
		var a Anexo
		if err := rows.Scan(&a.ID, &a.Lancamento, &a.Nome, &a.Arquivo, &a.Tipo, &a.Tamanho, &a.CriadoEm); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AbrirAnexo devolve o anexo e o caminho do arquivo.
func AbrirAnexo(conn *sql.DB, id int64) (Anexo, string, error) {
	var a Anexo
	err := conn.QueryRow(`SELECT id, lancamento_id, nome, arquivo, tipo, tamanho, criado_em FROM anexos WHERE id = ?`, id).
		Scan(&a.ID, &a.Lancamento, &a.Nome, &a.Arquivo, &a.Tipo, &a.Tamanho, &a.CriadoEm)
	if err == sql.ErrNoRows {
		return a, "", fmt.Errorf("anexo #%d não encontrado", id)
	}
	if err != nil {
		return a, "", err
	}
	pasta, err := PastaAnexos(conn)
	if err != nil {
		return a, "", err
	}
	caminho := filepath.Join(pasta, filepath.Base(a.Arquivo))
	if _, err := os.Stat(caminho); errors.Is(err, os.ErrNotExist) {
		// foi para a lixeira (banco restaurado de um backup): volta para o lugar
		os.Rename(filepath.Join(pasta, lixeira, filepath.Base(a.Arquivo)), caminho)
	}
	return a, caminho, nil
}

const (
	lixeira     = ".lixeira"
	diasLixeira = 60
)

// RemoverAnexo apaga o anexo e o arquivo.
func RemoverAnexo(conn *sql.DB, id int64) error {
	_, caminho, err := AbrirAnexo(conn, id)
	if err != nil {
		return err
	}
	if _, err := conn.Exec(`DELETE FROM anexos WHERE id = ?`, id); err != nil {
		return err
	}
	if err := os.Remove(caminho); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// LimpaAnexosOrfaos move para a lixeira os arquivos que nenhum anexo cita
// mais (o lançamento foi removido ou o banco foi zerado) e esvazia da lixeira
// o que está lá há mais de 60 dias. Devolve quantos moveu.
func LimpaAnexosOrfaos(conn *sql.DB) (int, error) {
	pasta, err := PastaAnexos(conn)
	if err != nil {
		return 0, nil
	}
	ents, err := os.ReadDir(pasta)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	usados := map[string]bool{}
	rows, err := conn.Query(`SELECT arquivo FROM anexos`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			rows.Close()
			return 0, err
		}
		usados[a] = true
	}
	rows.Close()
	n := 0
	for _, e := range ents {
		if e.IsDir() || usados[e.Name()] {
			continue
		}
		// recém-gravado: pode ser um anexo cujo registro ainda vai entrar
		if info, err := e.Info(); err != nil || time.Since(info.ModTime()) < 10*time.Minute {
			continue
		}
		if err := os.MkdirAll(filepath.Join(pasta, lixeira), 0o700); err != nil {
			return n, err
		}
		destino := filepath.Join(pasta, lixeira, e.Name())
		if os.Rename(filepath.Join(pasta, e.Name()), destino) == nil {
			agora := time.Now()
			os.Chtimes(destino, agora, agora) // o prazo conta da ida para a lixeira
			n++
		}
	}
	velhos, _ := os.ReadDir(filepath.Join(pasta, lixeira))
	for _, e := range velhos {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > diasLixeira*24*time.Hour {
			os.Remove(filepath.Join(pasta, lixeira, e.Name()))
		}
	}
	return n, nil
}

// AnexarCmd trata `prisma anexar <id do lançamento> <arquivo>`.
func AnexarCmd(conn *sql.DB, args []string) error {
	if len(args) != 2 {
		return errors.New("uso: prisma anexar <id do lançamento> <arquivo (foto ou PDF)>")
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("id inválido: %q", args[0])
	}
	dados, err := os.ReadFile(args[1])
	if err != nil {
		return err
	}
	a, err := Anexar(conn, id, filepath.Base(args[1]), dados)
	if err != nil {
		return err
	}
	fmt.Printf("Anexo #%d (%s, %s) guardado no lançamento #%d.\n", a.ID, a.Nome, tamanhoBR(a.Tamanho), id)
	return nil
}

// AnexosCmd trata `prisma anexos [id do lançamento] | remover <id> | salvar <id> <destino>`.
func AnexosCmd(conn *sql.DB, args []string) error {
	if _, err := LimpaAnexosOrfaos(conn); err != nil {
		return err
	}
	if len(args) >= 1 && (args[0] == "remover" || args[0] == "rm") {
		if len(args) != 2 {
			return errors.New("uso: prisma anexos remover <id do anexo>")
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return fmt.Errorf("id inválido: %q", args[1])
		}
		if err := RemoverAnexo(conn, id); err != nil {
			return err
		}
		fmt.Printf("Anexo #%d removido.\n", id)
		return nil
	}
	if len(args) >= 1 && args[0] == "salvar" {
		if len(args) != 3 {
			return errors.New("uso: prisma anexos salvar <id do anexo> <arquivo de destino>")
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return fmt.Errorf("id inválido: %q", args[1])
		}
		_, caminho, err := AbrirAnexo(conn, id)
		if err != nil {
			return err
		}
		dados, err := os.ReadFile(caminho)
		if err != nil {
			return err
		}
		if err := os.WriteFile(args[2], dados, 0o644); err != nil {
			return err
		}
		fmt.Printf("Anexo #%d salvo em %s.\n", id, args[2])
		return nil
	}
	var lanc int64
	if len(args) == 1 {
		var err error
		if lanc, err = strconv.ParseInt(args[0], 10, 64); err != nil {
			return fmt.Errorf("id inválido: %q", args[0])
		}
	}
	as, err := AnexosDe(conn, lanc)
	if err != nil {
		return err
	}
	if len(as) == 0 {
		fmt.Println("Nenhum anexo. Guarde um com: prisma anexar <id do lançamento> foto.jpg")
		return nil
	}
	w := novaTabela()
	fmt.Fprintln(w, "ID\tLANÇAMENTO\tARQUIVO\tTAMANHO\tDESDE")
	for _, a := range as {
		fmt.Fprintf(w, "%d\t#%d\t%s\t%s\t%s\n", a.ID, a.Lancamento, a.Nome, tamanhoBR(a.Tamanho), dataBR(a.CriadoEm))
	}
	return w.Flush()
}

func tamanhoBR(n int64) string {
	if n < 1<<20 {
		return fmt.Sprintf("%d KB", (n+1023)>>10)
	}
	return strings.Replace(fmt.Sprintf("%.1f MB", float64(n)/(1<<20)), ".", ",", 1)
}
