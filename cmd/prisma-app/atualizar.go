package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"prisma/internal/update"
)

// atualizarEReabrir é o "atualizar agora" da página: instala a versão nova
// (o prisma-app e o prisma ao lado) e, depois de a página receber a resposta,
// abre a cópia nova e fecha esta janela.
func atualizarEReabrir() (string, error) {
	var log bytes.Buffer
	tag, atualizou, err := update.AtualizarDoApp(&log)
	if err != nil {
		return "", err
	}
	if !atualizou {
		return fmt.Sprintf("Você já está na versão mais recente (%s).", update.Versao), nil
	}
	go func() {
		time.Sleep(700 * time.Millisecond) // deixa a resposta chegar à página
		reabrir()
	}()
	return fmt.Sprintf("Atualizado para %s. Reabrindo o Prisma…", tag), nil
}

// reabrir solta a trava, inicia o prisma-app novo (no mesmo caminho; no
// Windows o antigo foi movido para .old) e fecha a janela atual.
func reabrir() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	if lock, err := trava(); err == nil {
		os.Remove(lock) // senão a cópia nova só focaria esta, que vai fechar
	}
	if err := exec.Command(exe).Start(); err != nil {
		return
	}
	janela.Lock()
	defer janela.Unlock()
	if w := janela.w; w != nil {
		w.Dispatch(w.Terminate)
	}
}

// limpaAntigo apaga o .old que a atualização deixa no Windows (o .exe em uso
// não pode ser sobrescrito, só movido); nos outros sistemas não existe.
func limpaAntigo() {
	if exe, err := os.Executable(); err == nil {
		os.Remove(exe + ".old")
	}
}
