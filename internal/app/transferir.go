package app

import (
	"database/sql"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"prisma/internal/money"
)

// local identifica uma conta ou carteira na sintaxe "conta:1" / "carteira:2".
type local struct {
	tipo string
	id   int64
	nome string
}

func parseLocal(conn *sql.DB, s string) (local, error) {
	partes := strings.SplitN(strings.ToLower(strings.TrimSpace(s)), ":", 2)
	if len(partes) != 2 {
		return local{}, fmt.Errorf("local inválido %q (use conta:ID ou carteira:ID)", s)
	}
	tipo := partes[0]
	if tipo != "conta" && tipo != "carteira" {
		return local{}, fmt.Errorf("local inválido %q (use conta:ID ou carteira:ID)", s)
	}
	id, err := strconv.ParseInt(partes[1], 10, 64)
	if err != nil {
		return local{}, fmt.Errorf("local inválido %q (use conta:ID ou carteira:ID)", s)
	}
	tabela := tipo + "s"
	var nome string
	err = conn.QueryRow(`SELECT nome FROM `+tabela+` WHERE id = ?`, id).Scan(&nome)
	if err == sql.ErrNoRows {
		return local{}, fmt.Errorf("%s #%d não encontrada", tipo, id)
	}
	if err != nil {
		return local{}, err
	}
	return local{tipo, id, nome}, nil
}

func (l local) String() string { return fmt.Sprintf("%s %s", l.tipo, l.nome) }

func (l local) saldo(conn *sql.DB) (int64, error) {
	if l.tipo == "conta" {
		return saldoConta(conn, l.id)
	}
	return saldoCarteira(conn, l.id)
}

// Transferir move dinheiro entre contas/carteiras sem distorcer receitas
// e despesas: `prisma transferir --de conta:1 --para carteira:2 --valor 100`.
func Transferir(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("transferir", flag.ContinueOnError)
	de := fs.String("de", "", "origem: conta:ID ou carteira:ID (obrigatório)")
	para := fs.String("para", "", "destino: conta:ID ou carteira:ID (obrigatório)")
	valor := fs.String("valor", "", "valor (obrigatório)")
	data := fs.String("data", "hoje", "data da transferência")
	desc := fs.String("desc", "", "descrição")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *de == "" || *para == "" || *valor == "" {
		return fmt.Errorf("--de, --para e --valor são obrigatórios")
	}
	origem, err := parseLocal(conn, *de)
	if err != nil {
		return err
	}
	destino, err := parseLocal(conn, *para)
	if err != nil {
		return err
	}
	if origem.tipo == destino.tipo && origem.id == destino.id {
		return fmt.Errorf("origem e destino são o mesmo lugar")
	}
	centavos, err := money.Parse(*valor)
	if err != nil {
		return err
	}
	if centavos <= 0 {
		return fmt.Errorf("o valor deve ser positivo")
	}
	d, err := parseData(*data)
	if err != nil {
		return err
	}
	if *desc == "" {
		*desc = fmt.Sprintf("%s → %s", origem, destino)
	}

	_, err = conn.Exec(`
		INSERT INTO transferencias (valor, data, descricao, origem_tipo, origem_id, destino_tipo, destino_id)
		VALUES (?,?,?,?,?,?,?)`,
		centavos, d, *desc, origem.tipo, origem.id, destino.tipo, destino.id,
	)
	if err != nil {
		return err
	}

	sOrigem, err := origem.saldo(conn)
	if err != nil {
		return err
	}
	sDestino, err := destino.saldo(conn)
	if err != nil {
		return err
	}
	fmt.Printf("Transferência de %s registrada em %s.\n", money.Format(centavos), dataBR(d))
	fmt.Printf("  %s: %s | %s: %s\n", origem, money.Format(sOrigem), destino, money.Format(sDestino))
	return nil
}
