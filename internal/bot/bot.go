package bot

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"prisma/internal/app"
	"prisma/internal/db"
	"prisma/internal/money"
)

const ajuda = `Para registrar, me mande:

[+] valor [#categoria] [descrição] [marcadores]

Sem + é gasto; com + é receita.

Marcadores (opcionais, em qualquer ordem):
#mercado — categoria (padrão: geral)
@15, @15/07, @amanha, @ontem — vencimento (padrão: hoje)
! — já está pago/recebido
3x — divide o total em 3 parcelas mensais
rep:6 — repete por 6 meses
conta:2 / cart:1 — vincula a conta ou carteira

Exemplos:
25,50 #mercado pão e leite !
+3500 #salario salário @05/07
899,70 #eletronicos fone novo 3x
1200 #moradia aluguel @05 rep:6

Para consultar:
/saldo — posição geral consolidada
/pendentes — tudo que falta pagar/receber
/mes — lançamentos do mês atual
/relatorio — gastos por categoria e mês a mês
/previsao — projeção de saldo futuro
/plano — status dos planejamentos
#mercado — sozinha, lista a categoria no mês`

// config guarda o token do bot e o chat autorizado, num JSON ao lado do banco.
type config struct {
	Token  string `json:"token"`
	ChatID int64  `json:"chat_id"`
}

func configPath() (string, error) {
	p, err := db.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "telegram.json"), nil
}

func carregaConfig() (config, error) {
	var cfg config
	p, err := configPath()
	if err != nil {
		return cfg, err
	}
	dados, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	return cfg, json.Unmarshal(dados, &cfg)
}

func salvaConfig(cfg config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	dados, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// 0600: o token dá controle total do bot, não pode vazar pra outros usuários
	return os.WriteFile(p, dados, 0o600)
}

// Run trata `prisma bot [--token X] [--chat N]`: salva o que vier por flag e
// entra no loop de long polling até ser interrompido (Ctrl+C).
func Run(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("bot", flag.ContinueOnError)
	token := fs.String("token", "", "token do bot (obtido com o @BotFather); fica salvo")
	chatID := fs.Int64("chat", 0, "chat autorizado a registrar lançamentos; fica salvo")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := carregaConfig()
	if err != nil {
		return fmt.Errorf("lendo configuração do bot: %w", err)
	}
	if *token != "" || *chatID != 0 {
		if *token != "" {
			cfg.Token = *token
		}
		if *chatID != 0 {
			cfg.ChatID = *chatID
		}
		if err := salvaConfig(cfg); err != nil {
			return fmt.Errorf("salvando configuração do bot: %w", err)
		}
	}
	if cfg.Token == "" {
		return fmt.Errorf("bot sem token: crie um bot com o @BotFather no Telegram e rode `prisma bot --token SEU_TOKEN`")
	}

	cli := novoCliente(cfg.Token)
	eu, err := cli.getMe()
	if err != nil {
		return fmt.Errorf("conectando ao Telegram: %w", err)
	}
	fmt.Printf("Bot @%s conectado.\n", eu.Username)
	if cfg.ChatID == 0 {
		fmt.Println("Nenhum chat autorizado ainda: mande qualquer mensagem ao bot para descobrir seu chat id,")
		fmt.Println("depois rode `prisma bot --chat SEU_CHAT_ID`. Até lá, nada é registrado.")
	}

	var offset int64
	for {
		ups, err := cli.getUpdates(offset)
		if err != nil {
			fmt.Fprintf(os.Stderr, "aviso: %v (tentando de novo em 5s)\n", err)
			time.Sleep(5 * time.Second)
			continue
		}
		for _, up := range ups {
			offset = up.ID + 1
			switch {
			case up.Mensagem != nil:
				trataMensagem(conn, cli, cfg, up.Mensagem)
			case up.Callback != nil:
				trataCallback(conn, cli, cfg, up.Callback)
			}
		}
	}
}

func trataMensagem(conn *sql.DB, cli *cliente, cfg config, m *mensagem) {
	if cfg.ChatID == 0 {
		fmt.Printf("Mensagem de chat %d (não autorizado).\n", m.Chat.ID)
		cli.enviar(m.Chat.ID, fmt.Sprintf(
			"Este bot ainda não está pareado. Seu chat id é %d.\n"+
				"Se este Prisma é seu, rode: prisma bot --chat %d", m.Chat.ID, m.Chat.ID))
		return
	}
	if m.Chat.ID != cfg.ChatID {
		fmt.Printf("Ignorando mensagem do chat %d (autorizado: %d).\n", m.Chat.ID, cfg.ChatID)
		return
	}
	if m.Text == "" {
		return
	}

	// espelha o comportamento da CLI: materializa recorrências pendentes antes
	if _, err := app.GerarRecorrencias(conn); err != nil {
		fmt.Fprintf(os.Stderr, "aviso: recorrências: %v\n", err)
	}

	if strings.HasPrefix(m.Text, "/") {
		trataComando(conn, cli, m)
		return
	}
	// hashtag sozinha é consulta, não lançamento: lista a categoria no mês
	if cat, ok := strings.CutPrefix(strings.TrimSpace(m.Text), "#"); ok && !strings.ContainsAny(cat, " \t") && cat != "" {
		consultar(cli, m.Chat.ID, func() error {
			return app.Lancamentos(conn, []string{"--cat", strings.ToLower(cat), "--mes", time.Now().Format("2006-01")})
		})
		return
	}

	params, err := parseMensagem(m.Text, time.Now())
	if err != nil {
		cli.enviar(m.Chat.ID, "❌ "+err.Error()+"\n\nMande /ajuda para ver o formato.")
		return
	}
	criados, categoriaNova, err := app.CriarLancamentos(conn, params)
	if err != nil {
		cli.enviar(m.Chat.ID, "❌ "+err.Error())
		return
	}
	if err := cli.enviar(m.Chat.ID, textoConfirmacao(params, criados, categoriaNova), tecladoDesfazer(criados)); err != nil {
		fmt.Fprintf(os.Stderr, "aviso: enviando confirmação: %v\n", err)
	}
}

// trataComando responde os comandos /xxx de consulta; o que não conhecer
// vira a mensagem de ajuda.
func trataComando(conn *sql.DB, cli *cliente, m *mensagem) {
	// em grupos o Telegram manda "/saldo@MeuBot"; o sufixo não interessa
	cmd, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(m.Text)), "@")
	switch cmd {
	case "/saldo":
		consultar(cli, m.Chat.ID, func() error { return app.Saldo(conn, nil) })
	case "/pendentes":
		consultar(cli, m.Chat.ID, func() error { return app.Lancamentos(conn, []string{"--pendentes"}) })
	case "/mes":
		consultar(cli, m.Chat.ID, func() error {
			return app.Lancamentos(conn, []string{"--mes", time.Now().Format("2006-01")})
		})
	case "/relatorio":
		consultar(cli, m.Chat.ID, func() error { return app.Relatorio(conn, nil) })
	case "/previsao":
		consultar(cli, m.Chat.ID, func() error { return app.Previsao(conn, nil) })
	case "/plano":
		consultar(cli, m.Chat.ID, func() error { return app.Plano(conn, []string{"status"}) })
	default:
		cli.enviar(m.Chat.ID, ajuda)
	}
}

// consultar roda um comando da CLI capturando a saída (mesma técnica da TUI)
// e a envia em blocos monoespaçados, fatiada no limite de tamanho do Telegram.
func consultar(cli *cliente, chatID int64, f func() error) {
	saida, err := captura(f)
	if err != nil {
		cli.enviar(chatID, "❌ "+err.Error())
		return
	}
	if strings.TrimSpace(saida) == "" {
		cli.enviar(chatID, "Nada encontrado.")
		return
	}
	for _, parte := range fatiar(saida, 3800) {
		if err := cli.enviarPre(chatID, parte); err != nil {
			fmt.Fprintf(os.Stderr, "aviso: enviando consulta: %v\n", err)
			return
		}
	}
}

// captura redireciona o stdout durante a execução de um comando da CLI e
// devolve o texto impresso — o bot reaproveita toda a lógica existente.
func captura(f func() error) (string, error) {
	antigo := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = w
	errF := f()
	w.Close()
	os.Stdout = antigo
	var b strings.Builder
	io.Copy(&b, r)
	r.Close()
	return b.String(), errF
}

// fatiar divide o texto em pedaços de até max bytes, quebrando em fim de
// linha (mensagens do Telegram têm limite de 4096 caracteres).
func fatiar(texto string, max int) []string {
	texto = strings.TrimRight(texto, "\n")
	var partes []string
	for len(texto) > max {
		corte := strings.LastIndexByte(texto[:max], '\n')
		if corte <= 0 {
			corte = max
		}
		partes = append(partes, strings.TrimRight(texto[:corte], "\n"))
		texto = strings.TrimLeft(texto[corte:], "\n")
	}
	if texto != "" {
		partes = append(partes, texto)
	}
	return partes
}

func textoConfirmacao(p app.LancamentoParams, criados []app.LancamentoCriado, categoriaNova bool) string {
	rotulo, emoji := "Gasto", "🔴"
	if p.Tipo == "receber" {
		rotulo, emoji = "Receita", "🟢"
	}
	status := "pendente"
	if p.Quitado {
		status = "quitado"
	}

	var b strings.Builder
	if len(criados) == 1 {
		c := criados[0]
		fmt.Fprintf(&b, "%s %s #%d criado\n", emoji, rotulo, c.ID)
		fmt.Fprintf(&b, "%s — %s\n", money.Format(c.Valor), c.Desc)
		fmt.Fprintf(&b, "#%s · venc. %s · %s", catOuGeral(p.Cat), dataBR(c.Venc), status)
	} else {
		fmt.Fprintf(&b, "%s %s %q criado em %d lançamentos\n", emoji, rotulo, p.Desc, len(criados))
		fmt.Fprintf(&b, "#%s · %s\n", catOuGeral(p.Cat), status)
		for _, c := range criados {
			fmt.Fprintf(&b, "#%d · %s · %s\n", c.ID, dataBR(c.Venc), money.Format(c.Valor))
		}
	}
	if categoriaNova {
		fmt.Fprintf(&b, "\n⚠️ Primeira vez usando #%s — confira se não é erro de digitação.", catOuGeral(p.Cat))
	}
	return b.String()
}

// tecladoDesfazer monta o botão de desfazer; o callback_data do Telegram tem
// limite de 64 bytes, então com muitos ids o botão é omitido.
func tecladoDesfazer(criados []app.LancamentoCriado) *tecladoInline {
	ids := make([]string, len(criados))
	for i, c := range criados {
		ids[i] = strconv.FormatInt(c.ID, 10)
	}
	dados := "undo:" + strings.Join(ids, ",")
	if len(dados) > 64 {
		return nil
	}
	return &tecladoInline{Linhas: [][]botaoInline{{{Texto: "🗑 Desfazer", Dados: dados}}}}
}

func trataCallback(conn *sql.DB, cli *cliente, cfg config, cb *callback) {
	defer cli.responderCallback(cb.ID, "")
	if cfg.ChatID == 0 || cb.De.ID != cfg.ChatID || cb.Mensagem == nil {
		return
	}
	dados, ok := strings.CutPrefix(cb.Dados, "undo:")
	if !ok {
		return
	}
	var apagados int64
	for _, s := range strings.Split(dados, ",") {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			continue
		}
		res, err := conn.Exec(`DELETE FROM lancamentos WHERE id = ?`, id)
		if err != nil {
			cli.enviar(cb.Mensagem.Chat.ID, "❌ erro ao desfazer: "+err.Error())
			return
		}
		n, _ := res.RowsAffected()
		apagados += n
	}
	texto := cb.Mensagem.Text + "\n\n↩️ Desfeito."
	if apagados == 0 {
		texto = cb.Mensagem.Text + "\n\n↩️ Nada a desfazer (já tinha sido removido)."
	}
	if err := cli.editar(cb.Mensagem.Chat.ID, cb.Mensagem.ID, texto); err != nil {
		fmt.Fprintf(os.Stderr, "aviso: editando mensagem: %v\n", err)
	}
}

func catOuGeral(cat string) string {
	if cat == "" {
		return "geral"
	}
	return cat
}

// dataBR converte AAAA-MM-DD em DD/MM/AAAA para exibição.
func dataBR(s string) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return t.Format("02/01/2006")
}
