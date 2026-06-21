package remote_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"prisma/internal/remote"
)

// TestValorRoundTrip confere que cada tipo que o SQLite entrega sobrevive à ida
// e volta pela serialização de fio (inclusive passando por JSON, onde inteiros
// viram float e []byte vira base64). É o ponto onde um bug silencioso corromperia
// valores financeiros sem ninguém notar.
func TestValorRoundTrip(t *testing.T) {
	agora := time.Date(2026, 6, 21, 15, 4, 5, 123456789, time.UTC)
	casos := []struct {
		nome string
		in   any
		quer any
	}{
		{"nil", nil, nil},
		{"int64", int64(150000), int64(150000)},
		{"int negativo", int64(-42), int64(-42)},
		{"float", 3.14, 3.14},
		{"bool true", true, true},
		{"bool false", false, false},
		{"string", "Nubank · café", "Nubank · café"},
		{"bytes", []byte{0, 1, 2, 255}, []byte{0, 1, 2, 255}},
		{"time", agora, agora},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			// passa por JSON para simular a viagem real pela rede
			fio, err := json.Marshal(remote.CodificaValor(c.in))
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var v remote.Valor
			if err := json.Unmarshal(fio, &v); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			got, err := v.Decodifica()
			if err != nil {
				t.Fatalf("decodifica: %v", err)
			}
			if !iguais(got, c.quer) {
				t.Fatalf("round-trip de %v deu %#v (%T); quero %#v (%T)", c.nome, got, got, c.quer, c.quer)
			}
		})
	}
}

// TestArgsRoundTrip confere a (de)serialização do vetor de argumentos.
func TestArgsRoundTrip(t *testing.T) {
	in := []any{int64(1), "dois", 3.0, nil, []byte("x")}
	got, err := remote.DecodificaArgs(remote.CodificaArgs(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(in) {
		t.Fatalf("voltaram %d args; quero %d", len(got), len(in))
	}
	for i := range in {
		if !iguais(got[i], in[i]) {
			t.Errorf("arg %d: %#v; quero %#v", i, got[i], in[i])
		}
	}
}

// TestValorTipoInvalido garante que um envelope corrompido vira erro, não pânico.
func TestValorTipoInvalido(t *testing.T) {
	if _, err := (remote.Valor{T: "inexistente"}).Decodifica(); err == nil {
		t.Error("tipo desconhecido deveria dar erro")
	}
}

func iguais(a, b any) bool {
	ba, oka := a.([]byte)
	bb, okb := b.([]byte)
	if oka || okb {
		return oka && okb && bytes.Equal(ba, bb)
	}
	ta, oka := a.(time.Time)
	tb, okb := b.(time.Time)
	if oka || okb {
		return oka && okb && ta.Equal(tb)
	}
	return a == b
}
