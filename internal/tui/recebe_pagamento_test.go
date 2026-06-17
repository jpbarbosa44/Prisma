package tui

import "testing"

// TestValidaRecebePagamentoExigeGrupo garante que "outros do grupo te pagam?"
// só passa se um grupo estiver selecionado.
func TestValidaRecebePagamentoExigeGrupo(t *testing.T) {
	campos := []campo{
		{rotulo: "grupo"},
		{rotulo: "outros do grupo te pagam?"},
	}
	casos := []struct {
		grupo, recebe string
		querErro      bool
	}{
		{"", "não", false},
		{"", "sim", true},
		{"1", "sim", false},
		{"1", "não", false},
	}
	for _, c := range casos {
		aviso := validaRecebePagamento(campos, []string{c.grupo, c.recebe})
		if (aviso != "") != c.querErro {
			t.Errorf("grupo=%q recebe=%q: aviso=%q, quer erro=%v", c.grupo, c.recebe, aviso, c.querErro)
		}
	}
}
