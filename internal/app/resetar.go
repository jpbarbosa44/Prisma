package app

import (
	"bufio"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"prisma/internal/db"
)

// Resetar apaga TODOS os dados do banco: `prisma resetar [--sim] [--sem-backup]`.
// Sem --sim, mostra o que será perdido e exige digitar "apagar".
// Antes de zerar, salva uma cópia do arquivo ao lado do original.
func Resetar(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("resetar", flag.ContinueOnError)
	sim := fs.Bool("sim", false, "não pede confirmação (cuidado!)")
	semBackup := fs.Bool("sem-backup", false, "não cria a cópia de segurança")
	if err := fs.Parse(args); err != nil {
		return err
	}

	tabelas := []struct{ nome, rotulo string }{
		{"contas", "conta(s)"},
		{"carteiras", "carteira(s)"},
		{"lancamentos", "lançamento(s)"},
		{"transferencias", "transferência(s)"},
		{"recorrencias", "recorrência(s)"},
		{"emergencias", "emergência(s)"},
		{"planejamentos", "plano(s)"},
		{"grupos", "grupo(s)"},
		{"cartoes", "cartão(ões)"},
	}
	total := 0
	resumo := make([]string, 0, len(tabelas))
	for _, t := range tabelas {
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM ` + t.nome).Scan(&n); err != nil {
			return err
		}
		total += n
		if n > 0 {
			resumo = append(resumo, fmt.Sprintf("%d %s", n, t.rotulo))
		}
	}
	if total == 0 {
		fmt.Println("O banco já está vazio; nada a fazer.")
		return nil
	}

	fmt.Printf("Isso vai apagar PERMANENTEMENTE: %s.\n", strings.Join(resumo, ", "))
	if !*sim {
		fmt.Print("Digite \"apagar\" para confirmar: ")
		linha, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return fmt.Errorf("confirmação cancelada")
		}
		if strings.ToLower(strings.TrimSpace(linha)) != "apagar" {
			fmt.Println("Cancelado: nada foi apagado.")
			return nil
		}
	}

	if !*semBackup {
		caminho, err := db.Path()
		if err != nil {
			return err
		}
		backup := caminho + ".bak-" + time.Now().Format("20060102-150405")
		// snapshot consistente (VACUUM INTO), em vez de copiar o arquivo cru
		if _, err := conn.Exec("VACUUM INTO ?", backup); err != nil {
			return fmt.Errorf("criando backup: %w", err)
		}
		fmt.Printf("Backup salvo em %s\n", backup)
	}

	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	for _, t := range tabelas {
		if _, err := tx.Exec(`DELETE FROM ` + t.nome); err != nil {
			tx.Rollback()
			return err
		}
	}
	// zera os contadores de id (a tabela só existe após a primeira inserção)
	if _, err := tx.Exec(`DELETE FROM sqlite_sequence`); err != nil &&
		!strings.Contains(err.Error(), "no such table") {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if _, err := conn.Exec(`VACUUM`); err != nil {
		return err
	}
	fmt.Println("Banco zerado. O Prisma está como recém-instalado.")
	return nil
}
