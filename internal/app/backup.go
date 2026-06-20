package app

import (
	"bufio"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"prisma/internal/db"
)

// Este arquivo cobre a recuperação de dados: `prisma restaurar` (volta a uma
// cópia de backup, guardando antes o estado atual) e `prisma verificar`
// (integridade do banco e dos backups). São operações sobre o arquivo local.

// caminhoBanco devolve o caminho do banco em uso (pessoal ou da empresa).
func caminhoBanco(modoEmpresa bool) (string, error) {
	if modoEmpresa {
		return db.PathEmpresa()
	}
	return db.Path()
}

// backupInfo descreve uma cópia de backup disponível.
type backupInfo struct {
	caminho string
	quando  time.Time
	tamanho int64
}

// listarBackups reúne as cópias disponíveis ao lado do banco: os backups diários
// em backups/ e as cópias .bak-*/.pre-restauracao-* feitas antes de reset e
// restauração, da mais nova para a mais velha.
func listarBackups(dbPath string) []backupInfo {
	dir := filepath.Dir(dbPath)
	padroes := []string{
		filepath.Join(dir, "backups", "*.db"),
		filepath.Join(dir, filepath.Base(dbPath)+".bak-*"),
	}
	visto := map[string]bool{}
	var out []backupInfo
	for _, p := range padroes {
		nomes, _ := filepath.Glob(p)
		for _, n := range nomes {
			if visto[n] {
				continue
			}
			info, err := os.Stat(n)
			if err != nil || info.IsDir() {
				continue
			}
			visto[n] = true
			out = append(out, backupInfo{n, info.ModTime(), info.Size()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].quando.After(out[j].quando) })
	return out
}

// Restaurar substitui o banco por uma cópia de backup, guardando antes uma cópia
// de segurança do estado atual (para a restauração ser reversível):
// `prisma restaurar [--arquivo CAMINHO]`. Sem --arquivo, lista os backups e
// pede o número. Roda ANTES de abrir o banco (não há conexão aberta).
func Restaurar(args []string, modoEmpresa bool) error {
	fs := flag.NewFlagSet("restaurar", flag.ContinueOnError)
	arquivo := fs.String("arquivo", "", "caminho do backup a restaurar (pula a seleção)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dbPath, err := caminhoBanco(modoEmpresa)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("o banco %q ainda não existe — nada a restaurar", dbPath)
	}

	escolhido := *arquivo
	if escolhido == "" {
		backups := listarBackups(dbPath)
		if len(backups) == 0 {
			return fmt.Errorf("nenhum backup encontrado em %s", filepath.Join(filepath.Dir(dbPath), "backups"))
		}
		fmt.Println("Backups disponíveis (mais recente primeiro):")
		for i, b := range backups {
			fmt.Printf("  %2d) %s   %-9s   %s\n", i+1, b.quando.Format("2006-01-02 15:04"), humano(b.tamanho), filepath.Base(b.caminho))
		}
		fmt.Print("\nDigite o número do backup para restaurar (enter cancela): ")
		linha, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		linha = strings.TrimSpace(linha)
		if linha == "" {
			fmt.Println("Cancelado: nada foi alterado.")
			return nil
		}
		n, err := strconv.Atoi(linha)
		if err != nil || n < 1 || n > len(backups) {
			return fmt.Errorf("escolha inválida: %q", linha)
		}
		escolhido = backups[n-1].caminho
	}
	if _, err := os.Stat(escolhido); err != nil {
		return fmt.Errorf("backup %q não encontrado", escolhido)
	}
	// confere que o backup é um SQLite íntegro ANTES de mexer no banco atual
	if err := integridadeArquivo(escolhido); err != nil {
		return fmt.Errorf("o backup escolhido não passou na verificação de integridade: %w", err)
	}

	// 1) cópia de segurança do estado atual, para poder desfazer a restauração
	seguranca := filepath.Join(filepath.Dir(dbPath), "backups",
		filepath.Base(dbPath)+".pre-restauracao-"+time.Now().Format("20060102-150405")+".db")
	if err := os.MkdirAll(filepath.Dir(seguranca), 0o755); err != nil {
		return err
	}
	if err := db.Snapshot(dbPath, seguranca); err != nil {
		return fmt.Errorf("criando cópia de segurança do estado atual: %w", err)
	}

	// 2) substitui o banco pelo backup de forma atômica (rename) e remove o WAL
	//    antigo, para o SQLite não reaplicar mudanças sobre o conteúdo restaurado
	tmp := dbPath + ".restauracao-tmp"
	if err := copiaArquivo(escolhido, tmp); err != nil {
		return fmt.Errorf("copiando o backup: %w", err)
	}
	if err := os.Rename(tmp, dbPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("substituindo o banco: %w", err)
	}
	os.Remove(dbPath + "-wal")
	os.Remove(dbPath + "-shm")

	fmt.Printf("Banco restaurado a partir de %s.\n", filepath.Base(escolhido))
	fmt.Printf("Estado anterior guardado em %s\n", seguranca)
	fmt.Println("(restaure essa cópia para desfazer). Se o bot/servidor estiver no ar, reinicie-o.")
	return nil
}

// Verificar checa a integridade do banco em uso e dos backups:
// `prisma verificar`.
func Verificar(conn *sql.DB, modoEmpresa bool) error {
	fmt.Println("VERIFICAÇÃO DE INTEGRIDADE")
	fmt.Println()

	probs, err := integridade(conn)
	if err != nil {
		return err
	}
	if len(probs) == 0 {
		fmt.Println("Banco atual:          ✓ íntegro")
	} else {
		fmt.Println("Banco atual:          ✗ problemas encontrados:")
		for _, p := range probs {
			fmt.Printf("    - %s\n", p)
		}
	}

	fk, err := chavesEstrangeiras(conn)
	if err != nil {
		return err
	}
	if len(fk) == 0 {
		fmt.Println("Chaves estrangeiras:  ✓ sem violações")
	} else {
		fmt.Printf("Chaves estrangeiras:  ✗ %d violação(ões):\n", len(fk))
		for _, v := range fk {
			fmt.Printf("    - %s\n", v)
		}
	}

	dbPath, err := caminhoBanco(modoEmpresa)
	if err != nil {
		return err
	}
	backups := listarBackups(dbPath)
	fmt.Printf("\nBackups (%d):\n", len(backups))
	if len(backups) == 0 {
		fmt.Println("  nenhum backup encontrado ainda")
	}
	for _, b := range backups {
		status := "✓"
		if err := integridadeArquivo(b.caminho); err != nil {
			status = "✗ " + err.Error()
		}
		fmt.Printf("  %s  %s   %-9s   %s\n", status, b.quando.Format("2006-01-02 15:04"), humano(b.tamanho), filepath.Base(b.caminho))
	}
	return nil
}

// integridade roda PRAGMA integrity_check e devolve os problemas ("ok" = vazio).
func integridade(conn *sql.DB) ([]string, error) {
	rows, err := conn.Query("PRAGMA integrity_check")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var probs []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		if s != "ok" {
			probs = append(probs, s)
		}
	}
	return probs, rows.Err()
}

// chavesEstrangeiras roda PRAGMA foreign_key_check e descreve cada violação.
func chavesEstrangeiras(conn *sql.DB) ([]string, error) {
	rows, err := conn.Query("PRAGMA foreign_key_check")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var tabela, parent sql.NullString
		var rowid, fkid sql.NullInt64
		if err := rows.Scan(&tabela, &rowid, &parent, &fkid); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("%s (linha %d) aponta para %s inexistente",
			tabela.String, rowid.Int64, parent.String))
	}
	return out, rows.Err()
}

// integridadeArquivo abre um arquivo SQLite só para leitura e roda o
// integrity_check; devolve erro se não estiver íntegro.
func integridadeArquivo(path string) error {
	conn, err := sql.Open("sqlite", path+"?_pragma=query_only(true)&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer conn.Close()
	var res string
	if err := conn.QueryRow("PRAGMA integrity_check").Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return fmt.Errorf("corrompido")
	}
	return nil
}

// copiaArquivo copia src sobre dst (criando/truncando dst).
func copiaArquivo(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// humano formata um tamanho em bytes de forma legível.
func humano(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
