package tui

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"prisma/internal/app"
)

// Os comprovantes (anexos) pela web: enviar foto/PDF de um lançamento (no
// celular, direto da câmera), listar, abrir e remover.

func (s *servidorWeb) apiAnexos(w http.ResponseWriter, r *http.Request) {
	m, err := s.modo(r)
	if err != nil {
		respondeErro(w, http.StatusBadRequest, err)
		return
	}
	id, _ := strconv.ParseInt(r.URL.Query().Get("lancamento"), 10, 64)
	if id <= 0 {
		respondeErro(w, http.StatusBadRequest, errors.New("informe o lançamento"))
		return
	}
	s.mu.Lock()
	as, err := app.AnexosDe(m.conn, id)
	s.mu.Unlock()
	if err != nil {
		respondeErro(w, http.StatusInternalServerError, err)
		return
	}
	responde(w, as)
}

func (s *servidorWeb) apiEnviarAnexo(w http.ResponseWriter, r *http.Request) {
	m, ok := s.modoEscrita(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, app.MaxAnexo+64<<10)
	if err := r.ParseMultipartForm(app.MaxAnexo + 64<<10); err != nil {
		respondeErro(w, http.StatusBadRequest, fmt.Errorf("arquivo grande demais (máximo %d MB) ou envio inválido", app.MaxAnexo>>20))
		return
	}
	id, _ := strconv.ParseInt(r.FormValue("lancamento"), 10, 64)
	f, cab, err := r.FormFile("arquivo")
	if err != nil || id <= 0 {
		respondeErro(w, http.StatusBadRequest, errors.New("escolha a foto ou o PDF do comprovante"))
		return
	}
	var buf bytes.Buffer
	_, err = io.Copy(&buf, io.LimitReader(f, app.MaxAnexo+1))
	f.Close()
	if err != nil {
		respondeErro(w, http.StatusBadRequest, err)
		return
	}
	s.mu.Lock()
	a, err := app.Anexar(m.conn, id, cab.Filename, buf.Bytes())
	s.mu.Unlock()
	if err != nil {
		respondeErro(w, http.StatusBadRequest, err)
		return
	}
	responde(w, a)
}

// apiAnexo serve o arquivo — inline, com o tipo detectado na gravação e sem
// deixar o navegador adivinhar outro (nosniff).
func (s *servidorWeb) apiAnexo(w http.ResponseWriter, r *http.Request) {
	m, err := s.modo(r)
	if err != nil {
		respondeErro(w, http.StatusBadRequest, err)
		return
	}
	id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	s.mu.Lock()
	a, caminho, err := app.AbrirAnexo(m.conn, id)
	s.mu.Unlock()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	dados, err := os.ReadFile(caminho)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", a.Tipo)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("Content-Disposition", "inline; filename*=UTF-8''"+url.PathEscape(a.Nome))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Write(dados)
}

func (s *servidorWeb) apiRemoverAnexo(w http.ResponseWriter, r *http.Request) {
	m, ok := s.modoEscrita(w, r)
	if !ok {
		return
	}
	var p struct {
		ID int64 `json:"id"`
	}
	if err := lerJSON(w, r, &p); err != nil {
		respondeErro(w, http.StatusBadRequest, err)
		return
	}
	s.mu.Lock()
	err := app.RemoverAnexo(m.conn, p.ID)
	s.mu.Unlock()
	if err != nil {
		respondeErro(w, http.StatusBadRequest, err)
		return
	}
	responde(w, map[string]string{"msg": fmt.Sprintf("anexo #%d removido", p.ID)})
}
