// Package remote liga um Prisma cliente a um Prisma servidor pela rede local:
// o servidor é dono do banco SQLite e o cliente fala SQL com ele através de um
// driver database/sql remoto. Assim TUI, CLI e bot funcionam sem saber se o
// banco é local ou remoto — só o db.Open() decide qual lado é qual.
//
// Este arquivo define o protocolo de fio (HTTP + JSON) compartilhado pelos dois
// lados, incluindo a (de)serialização dos valores que trafegam como argumentos
// e como células das linhas.
package remote

import (
	"encoding/base64"
	"fmt"
	"time"
)

// Rotas HTTP do protocolo. Versionadas para permitir evoluir sem quebrar
// clientes antigos.
const (
	RotaOpen     = "/v1/open"
	RotaClose    = "/v1/close"
	RotaExec     = "/v1/exec"
	RotaQuery    = "/v1/query"
	RotaBegin    = "/v1/begin"
	RotaCommit   = "/v1/commit"
	RotaRollback = "/v1/rollback"
	RotaPing     = "/v1/ping"
)

// HeaderToken carrega o segredo compartilhado em cada requisição.
const HeaderToken = "X-Prisma-Token"

// Valor é o envelope tipado de um driver.Value indo ou voltando pela rede.
// SQLite entrega apenas int64, float64, string, []byte, bool, time.Time e nil;
// como JSON não distingue inteiro de real nem preserva []byte, marcamos o tipo
// explicitamente para reconstruir o valor exato do outro lado.
type Valor struct {
	T string `json:"t"`           // "null" | "int" | "float" | "bool" | "str" | "bytes" | "time"
	V any    `json:"v,omitempty"` // já no formato JSON-amigável do tipo T
}

// Tipos do campo Valor.T.
const (
	tNull  = "null"
	tInt   = "int"
	tFloat = "float"
	tBool  = "bool"
	tStr   = "str"
	tBytes = "bytes"
	tTime  = "time"
)

// CodificaValor traduz um valor nativo (vindo de driver.Value ou de um Scan em
// []any) para o envelope de fio.
func CodificaValor(v any) Valor {
	switch x := v.(type) {
	case nil:
		return Valor{T: tNull}
	case int64:
		return Valor{T: tInt, V: x}
	case int:
		return Valor{T: tInt, V: int64(x)}
	case float64:
		return Valor{T: tFloat, V: x}
	case bool:
		return Valor{T: tBool, V: x}
	case string:
		return Valor{T: tStr, V: x}
	case []byte:
		return Valor{T: tBytes, V: base64.StdEncoding.EncodeToString(x)}
	case time.Time:
		return Valor{T: tTime, V: x.Format(time.RFC3339Nano)}
	default:
		// Fallback seguro: representa como texto. Não deve acontecer com o
		// schema do Prisma (só INTEGER/TEXT/REAL), mas evita pânico silencioso.
		return Valor{T: tStr, V: fmt.Sprintf("%v", x)}
	}
}

// Decodifica reconstrói o valor nativo a partir do envelope. O resultado já é
// um driver.Value válido (int64, float64, bool, string, []byte, time.Time, nil).
func (val Valor) Decodifica() (any, error) {
	switch val.T {
	case tNull, "":
		return nil, nil
	case tInt:
		// JSON desserializa números como float64; convertemos de volta.
		switch n := val.V.(type) {
		case float64:
			return int64(n), nil
		case int64:
			return n, nil
		default:
			return nil, fmt.Errorf("valor int inválido: %T", val.V)
		}
	case tFloat:
		f, ok := val.V.(float64)
		if !ok {
			return nil, fmt.Errorf("valor float inválido: %T", val.V)
		}
		return f, nil
	case tBool:
		b, ok := val.V.(bool)
		if !ok {
			return nil, fmt.Errorf("valor bool inválido: %T", val.V)
		}
		return b, nil
	case tStr:
		s, ok := val.V.(string)
		if !ok {
			return nil, fmt.Errorf("valor str inválido: %T", val.V)
		}
		return s, nil
	case tBytes:
		s, ok := val.V.(string)
		if !ok {
			return nil, fmt.Errorf("valor bytes inválido: %T", val.V)
		}
		return base64.StdEncoding.DecodeString(s)
	case tTime:
		s, ok := val.V.(string)
		if !ok {
			return nil, fmt.Errorf("valor time inválido: %T", val.V)
		}
		return time.Parse(time.RFC3339Nano, s)
	default:
		return nil, fmt.Errorf("tipo de valor desconhecido: %q", val.T)
	}
}

// CodificaArgs converte os argumentos de um Exec/Query para a forma de fio.
func CodificaArgs(args []any) []Valor {
	out := make([]Valor, len(args))
	for i, a := range args {
		out[i] = CodificaValor(a)
	}
	return out
}

// DecodificaArgs reconstrói os argumentos nativos a partir da forma de fio.
func DecodificaArgs(args []Valor) ([]any, error) {
	out := make([]any, len(args))
	for i, a := range args {
		v, err := a.Decodifica()
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// --- Corpos das requisições e respostas ---

// ReqSessao identifica a sessão (uma conexão lógica do cliente, amarrada a uma
// *sql.Conn dedicada no servidor — é o que garante a semântica de transação).
type ReqSessao struct {
	SessionID string `json:"session_id"`
}

// RespOpen volta na abertura de sessão.
type RespOpen struct {
	SessionID string `json:"session_id"`
}

// ReqExec pede um Exec/Query. Args é nil para comandos sem parâmetros.
type ReqExec struct {
	SessionID string  `json:"session_id"`
	SQL       string  `json:"sql"`
	Args      []Valor `json:"args,omitempty"`
}

// RespExec volta de um Exec.
type RespExec struct {
	LastInsertID int64 `json:"last_insert_id"`
	RowsAffected int64 `json:"rows_affected"`
}

// RespQuery volta de um Query: colunas e as linhas já materializadas. Os
// resultados do Prisma são pequenos, então trazer tudo de uma vez é suficiente.
type RespQuery struct {
	Colunas []string  `json:"colunas"`
	Linhas  [][]Valor `json:"linhas"`
}

// RespErro é o corpo de qualquer resposta com status != 2xx.
type RespErro struct {
	Erro string `json:"erro"`
}
