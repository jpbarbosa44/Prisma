package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTagsDe(t *testing.T) {
	casos := map[string][]string{
		"Hotel #Viagem-2026 e #praia": {"viagem-2026", "praia"},
		"#viagem #viagem":             {"viagem"},
		"Lançamento #12 sem tag":      nil,
		"Reforma #obra/cozinha.":      {"obra/cozinha"},
		"Presente #Natal-":            {"natal"},
		"mercado #açaí":               {"açaí"},
	}
	for d, quer := range casos {
		if got := TagsDe(d); !reflect.DeepEqual(got, quer) {
			t.Errorf("%q: %v, quero %v", d, got, quer)
		}
	}
}

func TestResumoTags(t *testing.T) {
	conn := abreDB(t)
	for _, p := range []LancamentoParams{
		{Tipo: "pagar", Desc: "Hotel #viagem", Valor: 100000, Cat: "lazer", Venc: "2026-07-01"},
		{Tipo: "pagar", Desc: "Passagem #viagem #ferias", Valor: 50000, Cat: "transporte", Venc: "2026-06-01"},
		{Tipo: "receber", Desc: "Reembolso #viagem", Valor: 20000, Venc: "2026-07-10"},
		{Tipo: "pagar", Desc: "Sem tag", Valor: 999, Venc: "2026-07-10"},
	} {
		if _, _, _, err := CriarLancamentos(conn, p); err != nil {
			t.Fatal(err)
		}
	}
	rs, err := ResumoTags(conn)
	if err != nil || len(rs) != 2 {
		t.Fatalf("%+v %v", rs, err)
	}
	if v := rs[0]; v.Tag != "viagem" || v.Qtd != 3 || v.Despesas != 150000 || v.Receitas != 20000 || v.De != "2026-06-01" || v.Ate != "2026-07-10" {
		t.Errorf("viagem: %+v", v)
	}
	if rs[1].Tag != "ferias" || rs[1].Despesas != 50000 {
		t.Errorf("ferias: %+v", rs[1])
	}
}

// png de 1x1 (cabeçalho suficiente para o DetectContentType)
var pngMinimo = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

func TestAnexos(t *testing.T) {
	conn := abreDB(t)
	cria, _, _, err := CriarLancamentos(conn, LancamentoParams{Tipo: "pagar", Desc: "Luz", Valor: 100, Venc: "2026-11-10"})
	if err != nil {
		t.Fatal(err)
	}
	id := cria[0].ID
	if _, err := Anexar(conn, id, "x.txt", []byte("só texto")); err == nil || !strings.Contains(err.Error(), "não aceito") {
		t.Fatalf("texto: %v", err)
	}
	if _, err := Anexar(conn, 999, "a.png", pngMinimo); err == nil {
		t.Fatal("lançamento inexistente")
	}
	if _, err := Anexar(conn, id, "a.png", make([]byte, MaxAnexo+1)); err == nil {
		t.Fatal("grande demais")
	}
	a, err := Anexar(conn, id, "../../etc/conta de luz.PNG", pngMinimo)
	if err != nil {
		t.Fatal(err)
	}
	if a.Tipo != "image/png" || strings.ContainsAny(a.Nome, "/ ") || a.Nome != "conta-de-luz.PNG" {
		t.Errorf("anexo: %+v", a)
	}
	pdf, err := Anexar(conn, id, "boleto", []byte("%PDF-1.4\n%..."))
	if err != nil || pdf.Nome != "boleto.pdf" {
		t.Fatalf("pdf: %+v %v", pdf, err)
	}
	_, caminho, err := AbrirAnexo(conn, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(caminho); string(b) != string(pngMinimo) {
		t.Fatal("conteúdo do arquivo")
	}
	if st, _ := os.Stat(caminho); st.Mode().Perm() != 0o600 {
		t.Errorf("permissão %v", st.Mode().Perm())
	}
	if as, _ := AnexosDe(conn, id); len(as) != 2 {
		t.Fatalf("lista: %+v", as)
	}
	if err := RemoverAnexo(conn, pdf.ID); err != nil {
		t.Fatal(err)
	}
	// remover o lançamento leva o anexo (CASCADE) e a limpeza apaga o arquivo
	if _, err := conn.Exec(`DELETE FROM lancamentos WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if as, _ := AnexosDe(conn, 0); len(as) != 0 {
		t.Fatalf("cascade: %+v", as)
	}
	pasta, _ := PastaAnexos(conn)
	os.WriteFile(filepath.Join(pasta, "lixo"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(pasta, "novo"), []byte("x"), 0o600) // recém-gravado: fica
	velho := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(pasta, "lixo"), velho, velho)
	os.Chtimes(caminho, velho, velho)
	if n, err := LimpaAnexosOrfaos(conn); err != nil || n != 2 {
		t.Fatalf("órfãos: %d %v", n, err)
	}
	if _, err := os.Stat(caminho); !os.IsNotExist(err) {
		t.Fatal("o arquivo órfão sai da pasta")
	}
	// banco restaurado de um backup: o anexo volta a existir e o arquivo sai da lixeira
	if _, err := conn.Exec(`INSERT INTO lancamentos (id, tipo, descricao, valor, vencimento) VALUES (?, 'pagar', 'Luz', 100, '2026-11-10')`, id); err != nil {
		t.Fatal(err)
	}
	conn.Exec(`INSERT INTO anexos (id, lancamento_id, nome, arquivo, tipo, tamanho) VALUES (?, ?, ?, ?, ?, ?)`,
		a.ID, id, a.Nome, a.Arquivo, a.Tipo, a.Tamanho)
	_, de, err := AbrirAnexo(conn, a.ID)
	if b, _ := os.ReadFile(de); err != nil || string(b) != string(pngMinimo) {
		t.Fatalf("restaurar da lixeira: %v", err)
	}
}
