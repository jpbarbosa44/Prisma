package tui

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"prisma/internal/app"
)

// Rotas de arquivos da página: importar extrato (com prévia e desfazer) e
// exportar os lançamentos em CSV. Ficam fora do modo Analytics (só leitura).

// modoEscrita é o modo da requisição, recusando o Analytics (só leitura).
func (s *servidorWeb) modoEscrita(w http.ResponseWriter, r *http.Request) (*modoWeb, bool) {
	if s.nomeModo(r) == webAnalytics {
		respondeErro(w, http.StatusBadRequest, errors.New("o Analytics é somente leitura"))
		return nil, false
	}
	m, err := s.modo(r)
	if err != nil {
		respondeErro(w, http.StatusBadRequest, err)
		return nil, false
	}
	return m, true
}

// arquivoDoBanco é o caminho do arquivo SQLite aberto em conn, se for local
// (no modo cliente o banco está no servidor: não há o que copiar daqui).
func arquivoDoBanco(conn *sql.DB) string {
	var f string
	if err := conn.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&f); err != nil || f == "" {
		return ""
	}
	if _, err := os.Stat(f); err != nil {
		return ""
	}
	return f
}

// parseDestino lê "conta:N" ou "carteira:N" (as opções de opcoesLocais).
func parseDestino(v string) (app.DestinoExtrato, error) {
	tipo, id, _ := strings.Cut(v, ":")
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return app.DestinoExtrato{}, errors.New("escolha a conta ou carteira de destino")
	}
	switch tipo {
	case "conta":
		return app.DestinoExtrato{ContaID: n}, nil
	case "carteira":
		return app.DestinoExtrato{CarteiraID: n}, nil
	}
	return app.DestinoExtrato{}, errors.New("destino inválido")
}

// apiDestinos lista as contas e carteiras (destinos de um extrato).
func (s *servidorWeb) apiDestinos(w http.ResponseWriter, r *http.Request) {
	m, ok := s.modoEscrita(w, r)
	if !ok {
		return
	}
	type opcaoJSON struct {
		Valor  string `json:"valor"`
		Rotulo string `json:"rotulo"`
	}
	s.mu.Lock()
	ops := opcoesLocais(m.conn)()
	s.mu.Unlock()
	saida := make([]opcaoJSON, len(ops))
	for i, o := range ops {
		saida[i] = opcaoJSON{o.valor, o.rotulo}
	}
	responde(w, saida)
}

// apiImportar recebe o extrato (multipart: arquivo, destino, categoria). Sem
// confirmar=1 devolve a prévia; com ele, grava num lote "web-..." depois de um
// snapshot do banco. O arquivo vem de novo na confirmação: o servidor não
// guarda estado entre as duas chamadas.
func (s *servidorWeb) apiImportar(w http.ResponseWriter, r *http.Request) {
	m, ok := s.modoEscrita(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, app.MaxExtrato+64<<10)
	if err := r.ParseMultipartForm(app.MaxExtrato + 64<<10); err != nil {
		respondeErro(w, http.StatusBadRequest, errors.New("arquivo grande demais (máximo 2 MB) ou envio inválido"))
		return
	}
	f, cab, err := r.FormFile("arquivo")
	if err != nil {
		respondeErro(w, http.StatusBadRequest, errors.New("escolha o arquivo do extrato (.ofx ou .csv)"))
		return
	}
	var buf bytes.Buffer
	_, err = io.Copy(&buf, io.LimitReader(f, app.MaxExtrato+1))
	f.Close()
	if err != nil {
		respondeErro(w, http.StatusBadRequest, err)
		return
	}
	dest, err := parseDestino(r.FormValue("destino"))
	if err != nil {
		respondeErro(w, http.StatusBadRequest, err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	previa, err := app.PreverExtrato(m.conn, cab.Filename, buf.Bytes(), dest, r.FormValue("categoria"))
	if err != nil {
		respondeErro(w, http.StatusUnprocessableEntity, err)
		return
	}
	if r.FormValue("confirmar") != "1" {
		responde(w, previa)
		return
	}
	backup := ""
	if banco := arquivoDoBanco(m.conn); banco != "" {
		if backup, err = app.SnapshotAntes(banco, "web"); err != nil {
			respondeErro(w, http.StatusInternalServerError, fmt.Errorf("backup antes de importar: %w", err))
			return
		}
	}
	lote, criados, err := app.GravarExtrato(m.conn, previa, dest, "web")
	if err != nil {
		respondeErro(w, http.StatusUnprocessableEntity, err)
		return
	}
	msg := fmt.Sprintf("%d movimento(s) importado(s) para %s", criados, previa.Destino)
	if previa.Duplicados > 0 {
		msg += fmt.Sprintf(" · %d já existiam e foram ignorados", previa.Duplicados)
	}
	responde(w, map[string]any{"lote": lote, "criados": criados, "backup": backup, "msg": msg})
}

// apiLotes lista as importações (do app e do assistente), para desfazer.
func (s *servidorWeb) apiLotes(w http.ResponseWriter, r *http.Request) {
	m, ok := s.modoEscrita(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	lotes, err := app.Lotes(m.conn)
	s.mu.Unlock()
	if err != nil {
		respondeErro(w, http.StatusInternalServerError, err)
		return
	}
	responde(w, lotes)
}

// apiDesfazerLote apaga uma importação inteira (corpo: {"lote": "web-..."}).
func (s *servidorWeb) apiDesfazerLote(w http.ResponseWriter, r *http.Request) {
	m, ok := s.modoEscrita(w, r)
	if !ok {
		return
	}
	var corpo struct {
		Lote string `json:"lote"`
	}
	if err := lerJSON(w, r, &corpo); err != nil {
		respondeErro(w, http.StatusBadRequest, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if banco := arquivoDoBanco(m.conn); banco != "" {
		if _, err := app.SnapshotAntes(banco, "web"); err != nil {
			respondeErro(w, http.StatusInternalServerError, fmt.Errorf("backup antes de desfazer: %w", err))
			return
		}
	}
	n, err := app.DesfazerLote(m.conn, corpo.Lote)
	if err != nil {
		respondeErro(w, http.StatusUnprocessableEntity, err)
		return
	}
	responde(w, map[string]any{"msg": fmt.Sprintf("Importação %s desfeita: %d lançamento(s) apagado(s)", corpo.Lote, n)})
}

// nomeExportacao é o nome do CSV exportado (com o mês, se houver).
func nomeExportacao(mes string) string {
	if mes == "" {
		return "prisma-lancamentos.csv"
	}
	return "prisma-lancamentos-" + mes + ".csv"
}

// apiExportar devolve os lançamentos em CSV (download do navegador).
func (s *servidorWeb) apiExportar(w http.ResponseWriter, r *http.Request) {
	m, ok := s.modoEscrita(w, r)
	if !ok {
		return
	}
	mes := r.URL.Query().Get("mes")
	var buf bytes.Buffer
	s.mu.Lock()
	_, err := app.ExportarCSV(m.conn, &buf, mes)
	s.mu.Unlock()
	if err != nil {
		respondeErro(w, http.StatusBadRequest, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+nomeExportacao(mes)+`"`)
	w.Write(buf.Bytes())
}

// pastaDownloads é onde o app salva o CSV exportado: a pasta de downloads do
// usuário (xdg-user-dir no Linux; ~/Downloads nos outros), ou a pasta pessoal.
func pastaDownloads() string {
	casa, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	candidatas := []string{os.Getenv("XDG_DOWNLOAD_DIR")}
	if runtime.GOOS == "linux" { // respeita pastas traduzidas (~/Transferências)
		if out, err := exec.Command("xdg-user-dir", "DOWNLOAD").Output(); err == nil {
			candidatas = append(candidatas, strings.TrimSpace(string(out)))
		}
	}
	candidatas = append(candidatas, filepath.Join(casa, "Downloads"))
	for _, d := range candidatas {
		if d == "" {
			continue
		}
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			return d
		}
	}
	return casa
}

// apiSalvarExportacao é a exportação do app desktop: a janela (WebView) não
// baixa arquivos, então o CSV vai direto para a pasta de downloads.
func (s *servidorWeb) apiSalvarExportacao(w http.ResponseWriter, r *http.Request) {
	m, ok := s.modoEscrita(w, r)
	if !ok {
		return
	}
	mes := r.URL.Query().Get("mes")
	nome := nomeExportacao(mes)
	alvo := filepath.Join(pastaDownloads(), nome)
	if _, err := os.Stat(alvo); err == nil { // não sobrescreve um arquivo já existente
		alvo = filepath.Join(filepath.Dir(alvo), strings.TrimSuffix(nome, ".csv")+"-"+time.Now().Format("150405")+".csv")
	}
	var buf bytes.Buffer
	s.mu.Lock()
	n, err := app.ExportarCSV(m.conn, &buf, mes)
	s.mu.Unlock()
	if err == nil {
		err = os.WriteFile(alvo, buf.Bytes(), 0o644)
	}
	if err != nil {
		respondeErro(w, http.StatusInternalServerError, err)
		return
	}
	responde(w, map[string]any{"msg": fmt.Sprintf("%d lançamento(s) exportado(s) para %s", n, alvo), "arquivo": alvo})
}

// lerJSON decodifica um corpo JSON pequeno (até 64 KiB).
func lerJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return errors.New("corpo JSON inválido")
	}
	return nil
}

// apiLembretes liga ou desliga os lembretes do app (corpo: {"ligado": true}).
func (s *servidorWeb) apiLembretes(w http.ResponseWriter, r *http.Request) {
	var corpo struct {
		Ligado bool `json:"ligado"`
	}
	if err := lerJSON(w, r, &corpo); err != nil {
		respondeErro(w, http.StatusBadRequest, err)
		return
	}
	if err := s.ligaLembretes(corpo.Ligado); err != nil {
		respondeErro(w, http.StatusInternalServerError, err)
		return
	}
	msg := "Lembretes desligados."
	if corpo.Ligado {
		msg = "Lembretes ligados: o app avisa às 9h das contas atrasadas, de hoje e de amanhã."
	}
	responde(w, map[string]any{"ligado": corpo.Ligado, "msg": msg})
}

// apiAtualizar é o "atualizar agora" do app: baixa a versão nova, troca os
// binários e o app se reabre sozinho logo depois de responder.
func (s *servidorWeb) apiAtualizar(w http.ResponseWriter, r *http.Request) {
	msg, err := s.atualizar()
	if err != nil {
		respondeErro(w, http.StatusBadGateway, err)
		return
	}
	responde(w, map[string]any{"msg": msg})
}

// apiRelatorio é o fechamento do mês (?mes=AAAA-MM) em HTML para imprimir.
func (s *servidorWeb) apiRelatorio(w http.ResponseWriter, r *http.Request) {
	m, err := s.modo(r)
	if err != nil {
		respondeErro(w, http.StatusBadRequest, err)
		return
	}
	modo := r.URL.Query().Get("modo")
	link := func(mes string) string {
		u := "relatorio?mes=" + mes
		if modo != "" {
			u += "&modo=" + modo
		}
		return u
	}
	var b bytes.Buffer
	s.mu.Lock()
	err = RelatorioHTML(m.conn, r.URL.Query().Get("mes"), link, &b)
	s.mu.Unlock()
	if err != nil {
		respondeErro(w, http.StatusBadRequest, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(b.Bytes())
}
