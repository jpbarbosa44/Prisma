package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/term"

	"prisma/internal/seguranca"
)

// `prisma pin definir|remover|status|tempo N|rede on|off`: o PIN que tranca a
// interface web e o app desktop (e libera o acesso pelo celular).
func cmdPIN(args []string, in io.Reader, out io.Writer) error {
	if len(args) == 0 {
		args = []string{"status"}
	}
	c, err := seguranca.Carrega()
	if err != nil {
		return err
	}
	ler := leitorPIN(in, out)
	switch args[0] {
	case "status":
		if !c.TemPIN() {
			fmt.Fprintln(out, "Sem PIN: a web e o app abrem direto (só neste computador).")
			fmt.Fprintln(out, "Defina com: prisma pin definir")
			return nil
		}
		fmt.Fprintf(out, "PIN definido. Tranca depois de %d min parado.\n", int(c.Ociosidade().Minutes()))
		if c.Rede {
			fmt.Fprintf(out, "Acesso pelo celular: ligado (porta %d, mesma rede).\n", seguranca.PortaRede)
		} else {
			fmt.Fprintln(out, "Acesso pelo celular: desligado (prisma pin rede on).")
		}
		return nil
	case "definir", "trocar", "set":
		if c.TemPIN() {
			atual, err := ler("PIN atual: ")
			if err != nil {
				return err
			}
			if !c.Confere(atual) {
				return errors.New("o PIN atual não confere")
			}
		}
		novo, err := ler("PIN novo (4 a 32 caracteres): ")
		if err != nil {
			return err
		}
		if err := seguranca.ValidaPIN(novo); err != nil {
			return err
		}
		de, err := ler("Repita o PIN novo: ")
		if err != nil {
			return err
		}
		if de != novo {
			return errors.New("os dois PINs não batem")
		}
		if err := seguranca.DefinirPIN(novo); err != nil {
			return err
		}
		fmt.Fprintln(out, "PIN definido. A web e o app passam a pedir o PIN ao abrir.")
		return nil
	case "remover", "rm":
		if !c.TemPIN() {
			fmt.Fprintln(out, "Não há PIN definido.")
			return nil
		}
		atual, err := ler("PIN atual: ")
		if err != nil {
			return err
		}
		if !c.Confere(atual) {
			return errors.New("o PIN atual não confere")
		}
		if err := seguranca.RemoverPIN(); err != nil {
			return err
		}
		fmt.Fprintln(out, "PIN removido (e o acesso pelo celular, desligado).")
		return nil
	case "tempo":
		if len(args) < 2 {
			return errors.New("uso: prisma pin tempo <minutos>")
		}
		n, err := strconv.Atoi(args[1])
		if err != nil || n < 1 || n > 24*60 {
			return fmt.Errorf("minutos inválidos: %q (de 1 a 1440)", args[1])
		}
		c.Minutos = n
		if err := seguranca.Salva(c); err != nil {
			return err
		}
		fmt.Fprintf(out, "O Prisma tranca depois de %d min parado.\n", n)
		return nil
	case "rede":
		if len(args) < 2 || (args[1] != "on" && args[1] != "off") {
			return errors.New("uso: prisma pin rede on|off")
		}
		if args[1] == "on" && !c.TemPIN() {
			return errors.New("defina um PIN antes: prisma pin definir")
		}
		c.Rede = args[1] == "on"
		if err := seguranca.Salva(c); err != nil {
			return err
		}
		if c.Rede {
			fmt.Fprintf(out, "Acesso pelo celular ligado: vale na próxima vez que o app ou o prisma web abrir (porta %d).\n", seguranca.PortaRede)
		} else {
			fmt.Fprintln(out, "Acesso pelo celular desligado (na próxima abertura do app ou da web).")
		}
		return nil
	}
	return fmt.Errorf("subcomando inválido %q (use: status, definir, remover, tempo N, rede on|off)", args[0])
}

// leitorPIN lê o PIN sem eco no terminal; numa entrada redirecionada, lê
// linha a linha (scripts e testes).
func leitorPIN(in io.Reader, out io.Writer) func(prompt string) (string, error) {
	if f, ok := in.(*os.File); ok && term.IsTerminal(f.Fd()) {
		return func(prompt string) (string, error) {
			fmt.Fprint(out, prompt)
			b, err := term.ReadPassword(f.Fd())
			fmt.Fprintln(out)
			return string(b), err
		}
	}
	sc := bufio.NewScanner(in)
	return func(prompt string) (string, error) {
		fmt.Fprint(out, prompt)
		if !sc.Scan() {
			return "", errors.New("PIN não informado")
		}
		fmt.Fprintln(out)
		return strings.TrimRight(sc.Text(), "\r"), nil
	}
}
