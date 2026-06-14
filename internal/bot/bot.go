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
grupo:1 — divide a despesa entre o grupo (veja /grupos)

Exemplos:
25,50 #mercado pão e leite !
+3500 #salario salário @05/07
899,70 #eletronicos fone novo 3x
300 #mercado feira grupo:1

Outras ações:
quitar 142 — marca como pago/recebido
corrigir 27,90 — conserta o último lançamento
  (aceita valor, #categoria, @data, ! e nova descrição)
transferir 200 conta:1 cart:2 — move entre conta/carteira
foto com legenda — registra e guarda o comprovante
foto sem legenda — anexa ao último lançamento
/comprovante 142 — reenvia o comprovante

Para consultar:
/saldo — posição geral consolidada
/pendentes — tudo que falta pagar/receber
/mes — lançamentos do mês atual
/relatorio — gastos por categoria e mês a mês
/previsao — projeção de saldo futuro
/simular 4000 12x — e se eu comprar isto? (aceita 2% de juros, entrada:500)
/plano — status dos planejamentos
/grupos — seus grupos e os ids para usar em grupo:N
#mercado — a categoria no mês atual
#mercado maio · 3m · 2026-05 · tudo — outros períodos

Aviso de vencimentos às 9h e resumo do dia às 20h, automáticos.`

const (
	horaLembrete = 9  // a partir desta hora, avisa os vencimentos do dia
	horaResumo   = 20 // a partir desta hora, manda o resumo do dia
)

// config guarda o token do bot e o chat autorizado, num JSON ao lado do banco.
type config struct {
	Token  string `json:"token"`
	ChatID int64  `json:"chat_id"`
	// datas (AAAA-MM-DD) do último aviso enviado, para não repetir no mesmo dia
	UltimoLembrete string `json:"ultimo_lembrete,omitempty"`
	UltimoResumo   string `json:"ultimo_resumo,omitempty"`
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

// sessao é o estado de uma execução do bot.
type sessao struct {
	conn     *sql.DB
	cli      *cliente
	cfg      *config
	ultimoID int64 // último lançamento criado pelo bot — alvo de "corrigir" e da foto sem legenda
}

// Run trata `prisma bot [--token X] [--chat N]`: salva o que vier por flag e
// entra no loop de long polling até ser interrompido (Ctrl+C).
func Run(conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("bot", flag.ContinueOnError)
	token := fs.String("token", "", "token do bot (obtido com o @BotFather); fica salvo")
	chatID := fs.Int64("chat", 0, "chat autorizado a registrar lançamentos; fica salvo")
	instalar := fs.Bool("instalar-servico", false, "mantém o bot sempre rodando via systemd (acessível de qualquer rede)")
	remover := fs.Bool("remover-servico", false, "remove o serviço systemd do bot")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *remover {
		return removerServico()
	}

	cfg, err := carregaConfig()
	if err != nil {
		return fmt.Errorf("lendo configuração do bot: %w", err)
	}
	mudouConfig := *token != "" || *chatID != 0
	if mudouConfig {
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
	if *instalar {
		if err := instalarServico(); err != nil {
			return err
		}
		if cfg.ChatID == 0 {
			fmt.Println("\nFalta parear o chat: mande uma mensagem ao bot e rode `prisma bot --chat SEU_ID`.")
		}
		return nil
	}
	// souServico: esta execução É o próprio serviço systemd (o unit define
	// PRISMA_BOT_SERVICE=1). Sem isso, o processo do serviço se veria como "já
	// ativo" e se recusaria a rodar — o guard abaixo é só para o terminal.
	souServico := os.Getenv("PRISMA_BOT_SERVICE") == "1"

	// Se o serviço já está no ar, não dá para abrir um segundo poller (o
	// Telegram só permite um getUpdates por bot). Quando o usuário só quer
	// salvar token/chat, gravamos e reiniciamos o serviço — sem loop aqui.
	if !souServico && servicoAtivo() {
		if mudouConfig {
			if err := reiniciarServico(); err != nil {
				return fmt.Errorf("reiniciando o serviço do bot: %w", err)
			}
			fmt.Println("Configuração salva e serviço prisma-bot reiniciado.")
			if cfg.ChatID == 0 {
				fmt.Println("Falta parear o chat: mande uma mensagem ao bot e rode `prisma bot --chat SEU_ID`.")
			}
			return nil
		}
		return fmt.Errorf("o serviço prisma-bot já está rodando (um bot só admite um poller).\n" +
			"veja com `systemctl --user status prisma-bot`; pare com `prisma bot --remover-servico` se quiser rodar no terminal")
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

	s := &sessao{conn: conn, cli: cli, cfg: &cfg}
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
				s.trataMensagem(up.Mensagem)
			case up.Callback != nil:
				s.trataCallback(up.Callback)
			}
		}
		s.verificaAgenda(time.Now())
	}
}

func (s *sessao) trataMensagem(m *mensagem) {
	if s.cfg.ChatID == 0 {
		fmt.Printf("Mensagem de chat %d (não autorizado).\n", m.Chat.ID)
		s.cli.enviar(m.Chat.ID, fmt.Sprintf(
			"Este bot ainda não está pareado. Seu chat id é %d.\n"+
				"Se este Prisma é seu, rode: prisma bot --chat %d", m.Chat.ID, m.Chat.ID))
		return
	}
	if m.Chat.ID != s.cfg.ChatID {
		fmt.Printf("Ignorando mensagem do chat %d (autorizado: %d).\n", m.Chat.ID, s.cfg.ChatID)
		return
	}
	texto := m.Text
	if texto == "" {
		texto = m.Legenda
	}
	if texto == "" && len(m.Fotos) == 0 {
		return
	}

	// espelha o comportamento da CLI: materializa recorrências pendentes antes
	if _, err := app.GerarRecorrencias(s.conn); err != nil {
		fmt.Fprintf(os.Stderr, "aviso: recorrências: %v\n", err)
	}

	if len(m.Fotos) > 0 {
		s.trataFoto(m, texto)
		return
	}
	if strings.HasPrefix(texto, "/") {
		s.trataComando(m, texto)
		return
	}
	if cat, per, ok := consultaCategoria(texto); ok {
		s.consultaPorCategoria(m.Chat.ID, cat, per)
		return
	}

	palavra, resto, _ := strings.Cut(texto, " ")
	switch strings.ToLower(palavra) {
	case "quitar":
		s.quitar(m.Chat.ID, strings.TrimSpace(resto))
	case "corrigir":
		s.corrigir(m.Chat.ID, resto)
	case "transferir":
		s.transferir(m.Chat.ID, resto)
	default:
		s.registrar(m.Chat.ID, texto, "")
	}
}

// registrar cria o lançamento a partir do texto; fileID, se presente, vira
// comprovante anexado ao primeiro lançamento criado.
func (s *sessao) registrar(chatID int64, texto, fileID string) {
	params, err := parseMensagem(texto, time.Now())
	if err != nil {
		s.cli.enviar(chatID, "❌ "+err.Error()+"\n\nMande /ajuda para ver o formato.")
		return
	}
	criados, categoriaNova, err := app.CriarLancamentos(s.conn, params)
	if err != nil {
		s.cli.enviar(chatID, "❌ "+err.Error())
		return
	}
	s.ultimoID = criados[len(criados)-1].ID

	txt := textoConfirmacao(params, criados, categoriaNova)
	txt += s.notaGrupo(params, criados)
	if fileID != "" {
		if err := s.anexaComprovante(criados[0].ID, fileID); err != nil {
			txt += "\n⚠️ Não consegui guardar o comprovante: " + err.Error()
		} else {
			txt += fmt.Sprintf("\n📎 Comprovante anexado ao #%d.", criados[0].ID)
		}
	}
	txt += s.avisoPlano(params)
	if err := s.cli.enviar(chatID, txt, tecladoDesfazer(criados)); err != nil {
		fmt.Fprintf(os.Stderr, "aviso: enviando confirmação: %v\n", err)
	}
}

// notaGrupo, quando a despesa foi vinculada a um grupo, mostra o grupo, o
// divisor e a parte que de fato cabe a você (o resto é das outras pessoas).
func (s *sessao) notaGrupo(p app.LancamentoParams, criados []app.LancamentoCriado) string {
	if p.GrupoID == 0 {
		return ""
	}
	var nome string
	var pessoas int
	err := s.conn.QueryRow(`
		SELECT g.nome, COUNT(gp.id) FROM grupos g
		LEFT JOIN grupo_pessoas gp ON gp.grupo_id = g.id
		WHERE g.id = ? GROUP BY g.id`, p.GrupoID).Scan(&nome, &pessoas)
	if err != nil || pessoas < 1 {
		return ""
	}
	var total int64
	for _, c := range criados {
		total += c.Valor
	}
	return fmt.Sprintf("\n👥 %s ÷%d — sua parte: %s", nome, pessoas, money.Format(total/int64(pessoas)))
}

// avisoPlano avisa quando um gasto novo encosta (80%) ou estoura o limite do
// plano da categoria.
func (s *sessao) avisoPlano(p app.LancamentoParams) string {
	if p.Tipo != "pagar" {
		return ""
	}
	usos, err := app.PlanosDaCategoria(s.conn, catOuGeral(p.Cat), p.Venc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aviso: planos: %v\n", err)
		return ""
	}
	rotulos := map[string]string{"mes": "do mês", "semana": "da semana"}
	var b strings.Builder
	for _, u := range usos {
		if u.Limite <= 0 {
			continue
		}
		pct := u.Gasto * 100 / u.Limite
		switch {
		case u.Gasto > u.Limite:
			fmt.Fprintf(&b, "\n🚨 Plano de #%s %s estourado: %s de %s.",
				catOuGeral(p.Cat), rotulos[u.Periodo], money.Format(u.Gasto), money.Format(u.Limite))
		case pct >= 80:
			fmt.Fprintf(&b, "\n⚠️ Plano de #%s %s em %d%%: %s de %s.",
				catOuGeral(p.Cat), rotulos[u.Periodo], pct, money.Format(u.Gasto), money.Format(u.Limite))
		}
	}
	return b.String()
}

// quitar trata `quitar <id>`, reaproveitando o comando da CLI.
func (s *sessao) quitar(chatID int64, resto string) {
	id := strings.TrimPrefix(strings.TrimSpace(resto), "#")
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		s.cli.enviar(chatID, "❌ uso: quitar <id> (veja os ids em /pendentes)")
		return
	}
	saida, err := captura(func() error { return app.Quitar(s.conn, []string{id}) })
	if err != nil {
		s.cli.enviar(chatID, "❌ "+err.Error())
		return
	}
	s.cli.enviar(chatID, "✅ "+strings.TrimSpace(saida))
}

// corrigir altera campos do último lançamento criado pelo bot.
func (s *sessao) corrigir(chatID int64, resto string) {
	alvo, err := s.alvoUltimo()
	if err != nil {
		s.cli.enviar(chatID, "❌ "+err.Error())
		return
	}
	c, err := parseCorrecao(resto, time.Now())
	if err != nil {
		s.cli.enviar(chatID, "❌ "+err.Error())
		return
	}

	var sets []string
	var args []any
	if c.Valor != nil {
		if *c.Valor <= 0 {
			s.cli.enviar(chatID, "❌ o valor deve ser positivo")
			return
		}
		sets, args = append(sets, "valor = ?"), append(args, *c.Valor)
	}
	if c.Cat != nil {
		sets, args = append(sets, "categoria = ?"), append(args, *c.Cat)
	}
	if c.Venc != nil {
		sets, args = append(sets, "vencimento = ?"), append(args, *c.Venc)
	}
	if c.Desc != nil {
		sets, args = append(sets, "descricao = ?"), append(args, *c.Desc)
	}
	if c.Quitado {
		sets, args = append(sets, "status = 'quitado', quitado_em = ?"), append(args, time.Now().Format("2006-01-02"))
	}
	args = append(args, alvo)
	if _, err := s.conn.Exec(`UPDATE lancamentos SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		s.cli.enviar(chatID, "❌ "+err.Error())
		return
	}

	var desc, cat, venc, status string
	var valor int64
	err = s.conn.QueryRow(
		`SELECT descricao, categoria, vencimento, status, valor FROM lancamentos WHERE id = ?`, alvo,
	).Scan(&desc, &cat, &venc, &status, &valor)
	if err != nil {
		s.cli.enviar(chatID, "❌ "+err.Error())
		return
	}
	s.cli.enviar(chatID, fmt.Sprintf("✏️ Lançamento #%d corrigido\n%s — %s\n#%s · venc. %s · %s",
		alvo, money.Format(valor), desc, cat, dataBR(venc), status))
}

// transferir trata `transferir <valor> <origem> <destino> [descrição]`.
func (s *sessao) transferir(chatID int64, resto string) {
	t, err := parseTransferencia(resto)
	if err != nil {
		s.cli.enviar(chatID, "❌ "+err.Error())
		return
	}
	args := []string{"--de", t.De, "--para", t.Para, "--valor", money.Format(t.Valor)}
	if t.Desc != "" {
		args = append(args, "--desc", t.Desc)
	}
	saida, err := captura(func() error { return app.Transferir(s.conn, args) })
	if err != nil {
		s.cli.enviar(chatID, "❌ "+err.Error())
		return
	}
	s.cli.enviar(chatID, "🔄 "+strings.TrimSpace(saida))
}

// simular trata `/simular <valor> [Nx] [J%] [entrada:V]`, projetando o impacto
// de uma compra parcelada — reaproveita o comando da CLI.
func (s *sessao) simular(chatID int64, resto string) {
	args, err := parseSimulacao(resto)
	if err != nil {
		s.cli.enviar(chatID, "❌ "+err.Error())
		return
	}
	s.consultar(chatID, func() error { return app.Simular(s.conn, args) })
}

// trataFoto guarda o comprovante: com legenda, registra o lançamento junto;
// sem legenda, anexa ao último lançamento.
func (s *sessao) trataFoto(m *mensagem, legenda string) {
	fileID := m.Fotos[len(m.Fotos)-1].FileID // o último tamanho é o maior
	if legenda != "" {
		s.registrar(m.Chat.ID, legenda, fileID)
		return
	}
	alvo, err := s.alvoUltimo()
	if err != nil {
		s.cli.enviar(m.Chat.ID, "❌ "+err.Error()+"\nMande a foto com o lançamento na legenda.")
		return
	}
	if err := s.anexaComprovante(alvo, fileID); err != nil {
		s.cli.enviar(m.Chat.ID, "❌ "+err.Error())
		return
	}
	var desc string
	s.conn.QueryRow(`SELECT descricao FROM lancamentos WHERE id = ?`, alvo).Scan(&desc)
	s.cli.enviar(m.Chat.ID, fmt.Sprintf("📎 Comprovante anexado ao lançamento #%d (%s).", alvo, desc))
}

func (s *sessao) anexaComprovante(lancamentoID int64, fileID string) error {
	_, err := s.conn.Exec(
		`INSERT INTO comprovantes (lancamento_id, file_id) VALUES (?, ?)`, lancamentoID, fileID)
	return err
}

// alvoUltimo devolve o lançamento mais recente: o último criado pelo bot
// nesta execução ou, após reiniciar, o de maior id no banco.
func (s *sessao) alvoUltimo() (int64, error) {
	if s.ultimoID != 0 {
		return s.ultimoID, nil
	}
	var id sql.NullInt64
	if err := s.conn.QueryRow(`SELECT MAX(id) FROM lancamentos`).Scan(&id); err != nil {
		return 0, err
	}
	if !id.Valid {
		return 0, fmt.Errorf("nenhum lançamento registrado ainda")
	}
	return id.Int64, nil
}

// consultaCategoria reconhece mensagens "#categoria [período]" (sem valor),
// que são consulta e não lançamento.
func consultaCategoria(texto string) (cat, per string, ok bool) {
	tokens := strings.Fields(texto)
	if len(tokens) == 0 || len(tokens) > 2 || !strings.HasPrefix(tokens[0], "#") || len(tokens[0]) == 1 {
		return "", "", false
	}
	if len(tokens) == 2 {
		if pareceValor(tokens[1]) { // "#mercado 25,50" é lançamento
			return "", "", false
		}
		per = tokens[1]
	}
	return strings.ToLower(strings.TrimPrefix(tokens[0], "#")), per, true
}

func (s *sessao) consultaPorCategoria(chatID int64, cat, per string) {
	filtros, err := parsePeriodoConsulta(per, time.Now())
	if err != nil {
		s.cli.enviar(chatID, "❌ "+err.Error())
		return
	}
	args := append([]string{"--cat", cat}, filtros...)
	s.consultar(chatID, func() error { return app.Lancamentos(s.conn, args) })
}

// trataComando responde os comandos /xxx; o que não conhecer vira a ajuda.
func (s *sessao) trataComando(m *mensagem, texto string) {
	campos := strings.Fields(strings.TrimSpace(texto))
	// em grupos o Telegram manda "/saldo@MeuBot"; o sufixo não interessa
	cmd, _, _ := strings.Cut(strings.ToLower(campos[0]), "@")
	switch cmd {
	case "/saldo":
		s.consultar(m.Chat.ID, func() error { return app.Saldo(s.conn, nil) })
	case "/pendentes":
		s.consultar(m.Chat.ID, func() error { return app.Lancamentos(s.conn, []string{"--pendentes"}) })
	case "/mes":
		s.consultar(m.Chat.ID, func() error {
			return app.Lancamentos(s.conn, []string{"--mes", time.Now().Format("2006-01")})
		})
	case "/relatorio":
		s.consultar(m.Chat.ID, func() error { return app.Relatorio(s.conn, nil) })
	case "/previsao":
		s.consultar(m.Chat.ID, func() error { return app.Previsao(s.conn, nil) })
	case "/simular", "/simulacao":
		_, resto, _ := strings.Cut(strings.TrimSpace(texto), " ")
		s.simular(m.Chat.ID, resto)
	case "/plano":
		s.consultar(m.Chat.ID, func() error { return app.Plano(s.conn, []string{"status"}) })
	case "/grupos", "/grupo":
		s.consultar(m.Chat.ID, func() error { return app.Grupo(s.conn, []string{"listar"}) })
	case "/comprovante":
		if len(campos) < 2 {
			s.cli.enviar(m.Chat.ID, "❌ uso: /comprovante <id>")
			return
		}
		s.enviaComprovantes(m.Chat.ID, strings.TrimPrefix(campos[1], "#"))
	default:
		s.cli.enviar(m.Chat.ID, ajuda)
	}
}

func (s *sessao) enviaComprovantes(chatID int64, id string) {
	rows, err := s.conn.Query(`
		SELECT c.file_id, l.descricao FROM comprovantes c
		JOIN lancamentos l ON l.id = c.lancamento_id
		WHERE c.lancamento_id = ? ORDER BY c.id`, id)
	if err != nil {
		s.cli.enviar(chatID, "❌ "+err.Error())
		return
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var fileID, desc string
		if err := rows.Scan(&fileID, &desc); err != nil {
			break
		}
		s.cli.enviarFoto(chatID, fileID, fmt.Sprintf("#%s — %s", id, desc))
		n++
	}
	if n == 0 {
		s.cli.enviar(chatID, fmt.Sprintf("Nenhum comprovante para o lançamento #%s.", id))
	}
}

// consultar roda um comando da CLI capturando a saída (mesma técnica da TUI)
// e a envia em blocos monoespaçados, fatiada no limite de tamanho do Telegram.
func (s *sessao) consultar(chatID int64, f func() error) {
	saida, err := captura(f)
	if err != nil {
		s.cli.enviar(chatID, "❌ "+err.Error())
		return
	}
	if strings.TrimSpace(saida) == "" {
		s.cli.enviar(chatID, "Nada encontrado.")
		return
	}
	for _, parte := range fatiar(saida, 3800) {
		if err := s.cli.enviarPre(chatID, parte); err != nil {
			fmt.Fprintf(os.Stderr, "aviso: enviando consulta: %v\n", err)
			return
		}
	}
}

// verificaAgenda dispara os avisos do dia: vencimentos a partir das 9h e
// resumo a partir das 20h, no máximo uma vez por dia cada.
func (s *sessao) verificaAgenda(agora time.Time) {
	if s.cfg.ChatID == 0 {
		return
	}
	hoje := agora.Format("2006-01-02")
	if agora.Hour() >= horaLembrete && s.cfg.UltimoLembrete != hoje {
		// sessões longas do bot não reabrem o banco; o backup diário sai daqui
		if err := db.Backup(); err != nil {
			fmt.Fprintf(os.Stderr, "aviso: backup diário falhou: %v\n", err)
		}
		s.enviaLembretes(hoje)
		s.cfg.UltimoLembrete = hoje
		if err := salvaConfig(*s.cfg); err != nil {
			fmt.Fprintf(os.Stderr, "aviso: salvando config: %v\n", err)
		}
	}
	if agora.Hour() >= horaResumo && s.cfg.UltimoResumo != hoje {
		s.enviaResumo(hoje)
		s.cfg.UltimoResumo = hoje
		if err := salvaConfig(*s.cfg); err != nil {
			fmt.Fprintf(os.Stderr, "aviso: salvando config: %v\n", err)
		}
	}
}

// enviaLembretes avisa os pendentes atrasados, de hoje e de amanhã, com
// botão de quitar para cada um.
func (s *sessao) enviaLembretes(hoje string) {
	if _, err := app.GerarRecorrencias(s.conn); err != nil {
		fmt.Fprintf(os.Stderr, "aviso: recorrências: %v\n", err)
	}
	amanha := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	rows, err := s.conn.Query(`
		SELECT id, tipo, descricao, valor, vencimento FROM lancamentos
		WHERE status = 'pendente' AND vencimento <= ?
		ORDER BY vencimento, id LIMIT 15`, amanha)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aviso: lembretes: %v\n", err)
		return
	}
	defer rows.Close()

	var b strings.Builder
	b.WriteString("🔔 Vencimentos\n")
	var teclado tecladoInline
	n := 0
	for rows.Next() {
		var id, valor int64
		var tipo, desc, venc string
		if err := rows.Scan(&id, &tipo, &desc, &valor, &venc); err != nil {
			fmt.Fprintf(os.Stderr, "aviso: lembretes: %v\n", err)
			return
		}
		emoji := "🔴"
		if tipo == "receber" {
			emoji = "🟢"
		}
		quando := "vence hoje"
		switch {
		case venc < hoje:
			quando = "ATRASADO desde " + dataBR(venc)
		case venc > hoje:
			quando = "vence amanhã"
		}
		fmt.Fprintf(&b, "\n%s #%d %s — %s — %s", emoji, id, desc, money.Format(valor), quando)
		teclado.Linhas = append(teclado.Linhas, []botaoInline{
			{Texto: fmt.Sprintf("✅ Quitar #%d", id), Dados: fmt.Sprintf("quitar:%d", id)},
		})
		n++
	}
	if n == 0 {
		return // nada vencendo, nada de spam
	}
	if err := s.cli.enviar(s.cfg.ChatID, b.String(), &teclado); err != nil {
		fmt.Fprintf(os.Stderr, "aviso: enviando lembretes: %v\n", err)
	}
}

// enviaResumo fecha o dia: o que foi registrado e quitado, e o status dos
// planos de gasto.
func (s *sessao) enviaResumo(hoje string) {
	type total struct {
		n    int
		soma int64
	}
	soma := func(query string) map[string]total {
		m := map[string]total{}
		rows, err := s.conn.Query(query, hoje)
		if err != nil {
			return m
		}
		defer rows.Close()
		for rows.Next() {
			var tipo string
			var t total
			if rows.Scan(&tipo, &t.n, &t.soma) == nil {
				m[tipo] = t
			}
		}
		return m
	}
	criados := soma(`SELECT tipo, COUNT(*), COALESCE(SUM(valor),0) FROM lancamentos WHERE criado_em = ? GROUP BY tipo`)
	quitados := soma(`SELECT tipo, COUNT(*), COALESCE(SUM(valor),0) FROM lancamentos WHERE quitado_em = ? GROUP BY tipo`)
	if len(criados) == 0 && len(quitados) == 0 {
		return // dia sem movimento, sem resumo
	}

	var b strings.Builder
	fmt.Fprintf(&b, "🌙 Resumo de %s\n", dataBR(hoje))
	if t, ok := criados["pagar"]; ok {
		fmt.Fprintf(&b, "\nRegistrado: %d gasto(s), %s", t.n, money.Format(t.soma))
	}
	if t, ok := criados["receber"]; ok {
		fmt.Fprintf(&b, "\nRegistrado: %d receita(s), %s", t.n, money.Format(t.soma))
	}
	if t, ok := quitados["pagar"]; ok {
		fmt.Fprintf(&b, "\nPago hoje: %s (%d lançamento(s))", money.Format(t.soma), t.n)
	}
	if t, ok := quitados["receber"]; ok {
		fmt.Fprintf(&b, "\nRecebido hoje: %s (%d lançamento(s))", money.Format(t.soma), t.n)
	}
	if err := s.cli.enviar(s.cfg.ChatID, b.String()); err != nil {
		fmt.Fprintf(os.Stderr, "aviso: enviando resumo: %v\n", err)
		return
	}

	// anexa o status dos planos, se houver algum no período
	saida, err := captura(func() error { return app.Plano(s.conn, []string{"status"}) })
	if err == nil && !strings.HasPrefix(saida, "Nenhum plano") {
		for _, parte := range fatiar(saida, 3800) {
			s.cli.enviarPre(s.cfg.ChatID, parte)
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

func (s *sessao) trataCallback(cb *callback) {
	defer s.cli.responderCallback(cb.ID, "")
	if s.cfg.ChatID == 0 || cb.De.ID != s.cfg.ChatID || cb.Mensagem == nil {
		return
	}
	if dados, ok := strings.CutPrefix(cb.Dados, "undo:"); ok {
		s.desfazer(cb, dados)
		return
	}
	if id, ok := strings.CutPrefix(cb.Dados, "quitar:"); ok {
		s.quitar(cb.Mensagem.Chat.ID, id)
		return
	}
}

func (s *sessao) desfazer(cb *callback, dados string) {
	var apagados int64
	for _, str := range strings.Split(dados, ",") {
		id, err := strconv.ParseInt(str, 10, 64)
		if err != nil {
			continue
		}
		res, err := s.conn.Exec(`DELETE FROM lancamentos WHERE id = ?`, id)
		if err != nil {
			s.cli.enviar(cb.Mensagem.Chat.ID, "❌ erro ao desfazer: "+err.Error())
			return
		}
		n, _ := res.RowsAffected()
		apagados += n
	}
	texto := cb.Mensagem.Text + "\n\n↩️ Desfeito."
	if apagados == 0 {
		texto = cb.Mensagem.Text + "\n\n↩️ Nada a desfazer (já tinha sido removido)."
	}
	if err := s.cli.editar(cb.Mensagem.Chat.ID, cb.Mensagem.ID, texto); err != nil {
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
