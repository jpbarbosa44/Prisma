// Package bot implementa o bot de Telegram do Prisma: um loop de long
// polling que registra lançamentos a partir de mensagens de texto.
package bot

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// cliente fala com a API HTTP do Telegram usando apenas a biblioteca padrão.
type cliente struct {
	token string
	http  *http.Client
}

func novoCliente(token string) *cliente {
	// o timeout precisa ser maior que o long polling de getUpdates (50s)
	return &cliente{token: token, http: &http.Client{Timeout: 70 * time.Second}}
}

type usuario struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Nome     string `json:"first_name"`
}

type chat struct {
	ID int64 `json:"id"`
}

type foto struct {
	FileID string `json:"file_id"`
}

type mensagem struct {
	ID      int64    `json:"message_id"`
	De      *usuario `json:"from"`
	Chat    chat     `json:"chat"`
	Text    string   `json:"text"`
	Legenda string   `json:"caption"`
	Fotos   []foto   `json:"photo"` // tamanhos crescentes; o último é o maior
}

type callback struct {
	ID       string    `json:"id"`
	De       usuario   `json:"from"`
	Mensagem *mensagem `json:"message"`
	Dados    string    `json:"data"`
}

type update struct {
	ID       int64     `json:"update_id"`
	Mensagem *mensagem `json:"message"`
	Callback *callback `json:"callback_query"`
}

type botaoInline struct {
	Texto string `json:"text"`
	Dados string `json:"callback_data"`
}

type tecladoInline struct {
	Linhas [][]botaoInline `json:"inline_keyboard"`
}

// chamar faz um POST em /bot<token>/<metodo> e decodifica `result` em out (se não nil).
func (c *cliente) chamar(metodo string, params url.Values, out any) error {
	resp, err := c.http.PostForm("https://api.telegram.org/bot"+c.token+"/"+metodo, params)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var envelope struct {
		Ok        bool            `json:"ok"`
		Descricao string          `json:"description"`
		Resultado json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("%s: resposta inválida: %w", metodo, err)
	}
	if !envelope.Ok {
		return fmt.Errorf("%s: %s", metodo, envelope.Descricao)
	}
	if out != nil {
		return json.Unmarshal(envelope.Resultado, out)
	}
	return nil
}

func (c *cliente) getMe() (usuario, error) {
	var u usuario
	err := c.chamar("getMe", url.Values{}, &u)
	return u, err
}

// getUpdates espera até 50s por novidades (long polling).
func (c *cliente) getUpdates(offset int64) ([]update, error) {
	params := url.Values{
		"offset":          {strconv.FormatInt(offset, 10)},
		"timeout":         {"50"},
		"allowed_updates": {`["message","callback_query"]`},
	}
	var ups []update
	err := c.chamar("getUpdates", params, &ups)
	return ups, err
}

// enviar manda uma mensagem; o teclado inline é opcional.
func (c *cliente) enviar(chatID int64, texto string, teclado ...*tecladoInline) error {
	params := url.Values{
		"chat_id": {strconv.FormatInt(chatID, 10)},
		"text":    {texto},
	}
	if len(teclado) > 0 && teclado[0] != nil {
		b, err := json.Marshal(teclado[0])
		if err != nil {
			return err
		}
		params.Set("reply_markup", string(b))
	}
	return c.chamar("sendMessage", params, nil)
}

// enviarFoto reenvia uma foto já armazenada no Telegram pelo file_id.
func (c *cliente) enviarFoto(chatID int64, fileID, legenda string) error {
	params := url.Values{
		"chat_id": {strconv.FormatInt(chatID, 10)},
		"photo":   {fileID},
	}
	if legenda != "" {
		params.Set("caption", legenda)
	}
	return c.chamar("sendPhoto", params, nil)
}

// enviarPre manda texto em bloco monoespaçado (<pre>), preservando o
// alinhamento em colunas das tabelas da CLI.
func (c *cliente) enviarPre(chatID int64, texto string) error {
	params := url.Values{
		"chat_id":    {strconv.FormatInt(chatID, 10)},
		"text":       {"<pre>" + html.EscapeString(texto) + "</pre>"},
		"parse_mode": {"HTML"},
	}
	return c.chamar("sendMessage", params, nil)
}

// editar troca o texto de uma mensagem já enviada e remove o teclado inline.
func (c *cliente) editar(chatID, msgID int64, texto string) error {
	params := url.Values{
		"chat_id":    {strconv.FormatInt(chatID, 10)},
		"message_id": {strconv.FormatInt(msgID, 10)},
		"text":       {texto},
	}
	return c.chamar("editMessageText", params, nil)
}

// responderCallback fecha o "relógio" que o Telegram mostra ao tocar num botão.
func (c *cliente) responderCallback(id, texto string) error {
	params := url.Values{"callback_query_id": {id}}
	if texto != "" {
		params.Set("text", texto)
	}
	return c.chamar("answerCallbackQuery", params, nil)
}
