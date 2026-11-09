// Package seguranca guarda o PIN que tranca a interface web e o app desktop, e
// controla as sessões abertas com ele.
//
// O PIN é por computador, não por banco: fica em seguranca.json, ao lado do
// banco pessoal, e vale para os três modos (pessoal, empresa e Analytics) e
// para o acesso pelo celular. Só o hash vai para o disco (PBKDF2-SHA256 com
// sal aleatório).
package seguranca

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"prisma/internal/db"
)

const (
	iteracoes     = 600_000
	MinutosPadrao = 15 // sessão ociosa por mais que isso tranca de novo
	PortaRede     = 8765
)

// Config é o conteúdo de seguranca.json.
type Config struct {
	Sal     string `json:"sal,omitempty"`
	Hash    string `json:"hash,omitempty"`
	Iter    int    `json:"iter,omitempty"`
	Minutos int    `json:"minutos,omitempty"` // 0 = MinutosPadrao
	// Rede deixa o celular (e outros aparelhos da rede de casa) abrir a web;
	// só vale com PIN definido
	Rede bool `json:"rede,omitempty"`
}

// TemPIN diz se há um PIN definido.
func (c Config) TemPIN() bool { return c.Hash != "" }

// Ociosidade é quanto tempo uma sessão parada continua aberta.
func (c Config) Ociosidade() time.Duration {
	if c.Minutos <= 0 {
		return MinutosPadrao * time.Minute
	}
	return time.Duration(c.Minutos) * time.Minute
}

// Confere diz se pin é o PIN definido (tempo constante).
func (c Config) Confere(pin string) bool {
	if !c.TemPIN() {
		return false
	}
	sal, err1 := hex.DecodeString(c.Sal)
	alvo, err2 := hex.DecodeString(c.Hash)
	if err1 != nil || err2 != nil {
		return false
	}
	h, err := pbkdf2.Key(sha256.New, pin, sal, c.Iter, len(alvo))
	return err == nil && subtle.ConstantTimeCompare(h, alvo) == 1
}

// Caminho é o arquivo seguranca.json (ao lado do banco pessoal).
func Caminho() (string, error) {
	p, err := db.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "seguranca.json"), nil
}

// Carrega lê a configuração; sem arquivo, devolve a vazia (sem PIN).
func Carrega() (Config, error) {
	var c Config
	p, err := Caminho()
	if err != nil {
		return c, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("%s inválido: %w", p, err)
	}
	return c, nil
}

// Salva grava a configuração só para o dono do arquivo (0600).
func Salva(c Config) error {
	p, err := Caminho()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// ValidaPIN confere o formato: de 4 a 32 caracteres, sem espaços nas pontas.
func ValidaPIN(pin string) error {
	if strings.TrimSpace(pin) != pin {
		return errors.New("o PIN não pode começar nem terminar com espaço")
	}
	if n := utf8.RuneCountInString(pin); n < 4 || n > 32 {
		return errors.New("o PIN precisa ter de 4 a 32 caracteres")
	}
	return nil
}

// DefinirPIN troca (ou cria) o PIN, mantendo o resto da configuração.
func DefinirPIN(pin string) error {
	if err := ValidaPIN(pin); err != nil {
		return err
	}
	c, err := Carrega()
	if err != nil {
		return err
	}
	sal := make([]byte, 16)
	rand.Read(sal)
	h, err := pbkdf2.Key(sha256.New, pin, sal, iteracoes, 32)
	if err != nil {
		return err
	}
	c.Sal, c.Hash, c.Iter = hex.EncodeToString(sal), hex.EncodeToString(h), iteracoes
	return Salva(c)
}

// RemoverPIN tira o PIN e, junto, o acesso pela rede (que exige PIN).
func RemoverPIN() error {
	c, err := Carrega()
	if err != nil {
		return err
	}
	c.Sal, c.Hash, c.Iter, c.Rede = "", "", 0, false
	return Salva(c)
}

// Sessoes são os logins abertos com o PIN, em memória: fechar o servidor
// tranca tudo. Cada uso renova o prazo de ociosidade.
type Sessoes struct {
	mu     sync.Mutex
	ultimo map[string]time.Time
	agora  func() time.Time
}

// NovasSessoes cria o controle de sessões.
func NovasSessoes() *Sessoes {
	return &Sessoes{ultimo: map[string]time.Time{}, agora: time.Now}
}

// Nova abre uma sessão e devolve o token (32 bytes aleatórios, em hex).
func (s *Sessoes) Nova() string {
	b := make([]byte, 32)
	rand.Read(b)
	t := hex.EncodeToString(b)
	s.mu.Lock()
	s.ultimo[t] = s.agora()
	s.mu.Unlock()
	return t
}

// Valida diz se o token é de uma sessão viva e, se for, renova o prazo.
func (s *Sessoes) Valida(token string, ociosidade time.Duration) bool {
	if token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	agora := s.agora()
	for t, u := range s.ultimo { // poda as vencidas
		if agora.Sub(u) > ociosidade {
			delete(s.ultimo, t)
		}
	}
	if _, ok := s.ultimo[token]; !ok {
		return false
	}
	s.ultimo[token] = agora
	return true
}

// Encerra fecha uma sessão (botão trancar).
func (s *Sessoes) Encerra(token string) {
	s.mu.Lock()
	delete(s.ultimo, token)
	s.mu.Unlock()
}

// EncerraTodas fecha todas (o PIN mudou).
func (s *Sessoes) EncerraTodas() {
	s.mu.Lock()
	clear(s.ultimo)
	s.mu.Unlock()
}

// Tentativas barra quem erra o PIN seguido: a partir da 5ª falha de um mesmo
// endereço, espera 30 s, e a espera dobra a cada nova falha (até 1 h).
type Tentativas struct {
	mu    sync.Mutex
	por   map[string]*tentativa
	agora func() time.Time
}

type tentativa struct {
	falhas int
	ate    time.Time
}

// NovasTentativas cria o controle de tentativas.
func NovasTentativas() *Tentativas {
	return &Tentativas{por: map[string]*tentativa{}, agora: time.Now}
}

// Espera diz quanto falta para o endereço poder tentar de novo (0 = já pode).
func (t *Tentativas) Espera(ip string) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	if x, ok := t.por[ip]; ok {
		if d := x.ate.Sub(t.agora()); d > 0 {
			return d
		}
	}
	return 0
}

// Falhou registra um erro e devolve a espera imposta a partir de agora.
func (t *Tentativas) Falhou(ip string) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	x := t.por[ip]
	if x == nil {
		x = &tentativa{}
		t.por[ip] = x
	}
	x.falhas++
	if x.falhas < 5 {
		return 0
	}
	d := 30 * time.Second << min(x.falhas-5, 7) // 30 s, 1 min, 2 min ... ~1 h
	x.ate = t.agora().Add(d)
	return d
}

// Acertou zera o histórico do endereço.
func (t *Tentativas) Acertou(ip string) {
	t.mu.Lock()
	delete(t.por, ip)
	t.mu.Unlock()
}
