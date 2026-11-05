package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"

	"prisma/internal/db"
	"prisma/internal/desktop"
)

// estadoApp é o que o app lembra entre aberturas, em app.json ao lado do banco.
type estadoApp struct {
	Atalho bool `json:"atalho"` // o atalho no menu já foi criado
	// SemLembretes desliga os lembretes de contas (o padrão é ligado)
	SemLembretes bool   `json:"semLembretes,omitempty"`
	UltimoAviso  string `json:"ultimoAviso,omitempty"` // AAAA-MM-DD do último lembrete
}

func lembretesLigados() bool {
	muEstado.Lock()
	defer muEstado.Unlock()
	return !carregaEstado().SemLembretes
}

func ligaLembretes(ligado bool) error {
	muEstado.Lock()
	defer muEstado.Unlock()
	e := carregaEstado()
	e.SemLembretes = !ligado
	return salvaEstado(e)
}

var muEstado sync.Mutex

func caminhoEstado() (string, error) {
	p, err := db.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "app.json"), nil
}

// carregaEstado lê o app.json; ausente ou ilegível vale o estado zero.
func carregaEstado() estadoApp {
	var e estadoApp
	if p, err := caminhoEstado(); err == nil {
		if b, err := os.ReadFile(p); err == nil {
			json.Unmarshal(b, &e)
		}
	}
	return e
}

func salvaEstado(e estadoApp) error {
	p, err := caminhoEstado()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// garanteAtalho põe o Prisma no menu de aplicativos na primeira abertura:
// quem só usa o app nunca roda `prisma instalar-atalho`. Falhou, tenta de
// novo na próxima abertura.
func garanteAtalho() {
	muEstado.Lock()
	defer muEstado.Unlock()
	e := carregaEstado()
	if e.Atalho {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	if err := desktop.InstalarAtalhoDoApp(exe, io.Discard); err != nil {
		return
	}
	e.Atalho = true
	salvaEstado(e)
}
