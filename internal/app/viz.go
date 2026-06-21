package app

import (
	"strconv"
	"strings"

	"github.com/guptarohit/asciigraph"
)

// Este arquivo concentra o toolkit de visualização em texto reaproveitado pelos
// gráficos (`prisma graficos`) e pelo módulo Analytics. Tudo aqui devolve string
// pura (sem ANSI): a coloração fica a cargo da TUI, que captura o stdout e pinta
// por regex — assim o mesmo desenho serve para o terminal cru e para a TUI. Os
// gráficos de linha usam github.com/guptarohit/asciigraph; as barras, sparklines
// e mapas de calor são desenhados com blocos Unicode de resolução sub-célula.

const (
	// blocosOitavos vai de vazio (0/8) a cheio (8/8): permite barras com
	// resolução de 1/8 de caractere, bem mais suaves que repetir "█".
	blocosOitavos = " ▏▎▍▌▋▊▉█"
	// sparkRunes são as oito alturas de uma sparkline.
	sparkRunes = "▁▂▃▄▅▆▇█"
	// sombras vão do claro ao denso, para mapas de calor.
	sombras = " ·░▒▓█"
)

// Códigos ANSI de cor usados nas barras. O módulo emite o ANSI direto (como o
// asciigraph faz nos gráficos de linha): isto só alcança Graficos e Analytics,
// nunca o bot do Telegram, que captura apenas Relatorio/Plano (texto puro).
const (
	cReset  = "\x1b[0m"
	cVerde  = "\x1b[32m"
	cVermel = "\x1b[31m"
	cAmar   = "\x1b[33m"
	cCiano  = "\x1b[36m"
	cAzul   = "\x1b[34m"
	cMagen  = "\x1b[35m"
	cCinza  = "\x1b[90m"
)

// paletaSeg é a sequência de cores das fatias da barra de composição, alinhada
// aos glifos de barra100.
var paletaSeg = []string{cCiano, cVerde, cAmar, cMagen, cAzul, cVermel}

// pintar envolve s no código de cor (e zera depois). String vazia passa direto.
func pintar(cor, s string) string {
	if s == "" {
		return s
	}
	return cor + s + cReset
}

// corZona devolve a cor de um score 0–100: verde (bom), amarelo (atenção),
// vermelho (crítico) — o mesmo critério de rotuloSaude.
func corZona(score int) string {
	switch {
	case score >= 60:
		return cVerde
	case score >= 40:
		return cAmar
	default:
		return cVermel
	}
}

// corCalor mapeia uma intensidade 0..1 num gradiente verde→amarelo→vermelho,
// para o mapa de calor da sazonalidade.
func corCalor(frac float64) string {
	switch {
	case frac >= 0.80:
		return cVermel
	case frac >= 0.55:
		return cAmar
	case frac > 0:
		return cVerde
	default:
		return cCinza
	}
}

// reais converte centavos em reais (float) para alimentar os gráficos de linha,
// cujo eixo Y é numérico.
func reais(c int64) float64 { return float64(c) / 100 }

// reaisSerie converte uma série de centavos em reais.
func reaisSerie(vs []int64) []float64 {
	out := make([]float64, len(vs))
	for i, v := range vs {
		out[i] = reais(v)
	}
	return out
}

// barraFina desenha uma barra-medidor proporcional a valor/maior, com trilho
// (░) preenchendo o restante e resolução de 1/8 de célula na ponta — o que dá um
// acabamento bem mais fino que blocos inteiros. Sempre devolve `largura` células.
func barraFina(valor, maior int64, largura int) string {
	if largura <= 0 {
		return ""
	}
	if maior <= 0 {
		return strings.Repeat("░", largura)
	}
	frac := float64(valor) / float64(maior)
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	total := frac * float64(largura) // em células
	cheios := int(total)
	oit := int((total-float64(cheios))*8 + 0.5)
	if oit == 8 { // arredondou para uma célula cheia
		cheios++
		oit = 0
	}
	if cheios > largura {
		cheios, oit = largura, 0
	}
	var b strings.Builder
	b.WriteString(strings.Repeat("█", cheios))
	trilho := largura - cheios
	if oit > 0 && trilho > 0 {
		b.WriteString(string([]rune(blocosOitavos)[oit]))
		trilho--
	}
	if trilho > 0 {
		b.WriteString(strings.Repeat("░", trilho))
	}
	return b.String()
}

// barraFinaCor é barraFina com a parte preenchida em `cor` e o trilho (░) em
// cinza. A largura visível continua sendo `largura` (o ANSI tem largura zero na
// tela), então só serve em saídas que NÃO passam por tabwriter — ou na última
// coluna dele.
func barraFinaCor(valor, maior int64, largura int, cor string) string {
	runes := []rune(barraFina(valor, maior, largura))
	i := 0
	for i < len(runes) && runes[i] != '░' {
		i++
	}
	return pintar(cor, string(runes[:i])) + pintar(cCinza, string(runes[i:]))
}

// sparkline condensa uma série inteira numa única linha de mini-barras, ótima
// para insinuar tendência ao lado de um número. A escala é local (min..max).
func sparkline(vals []int64) string {
	if len(vals) == 0 {
		return ""
	}
	min, max := vals[0], vals[0]
	for _, v := range vals {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	rs := []rune(sparkRunes)
	rng := max - min
	var b strings.Builder
	for _, v := range vals {
		idx := 0
		if rng > 0 {
			idx = int(float64(v-min)/float64(rng)*float64(len(rs)-1) + 0.5)
		}
		b.WriteRune(rs[idx])
	}
	return b.String()
}

// shade devolve o caractere de sombreado correspondente a frac (0..1), para
// mapas de calor (sazonalidade, intensidade mês a mês).
func shade(frac float64) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	rs := []rune(sombras)
	return string(rs[int(frac*float64(len(rs)-1)+0.5)])
}

// graficoLinha desenha uma ou mais séries (já em reais) como gráfico de linha
// ASCII. `cores` colore cada série; `nomes` rende a legenda colorida das séries;
// `legenda` vira a caption embaixo do gráfico.
func graficoLinha(series [][]float64, altura int, cores []asciigraph.AnsiColor, nomes []string, legenda string) string {
	largura := larguraSerie(series)
	opts := []asciigraph.Option{
		asciigraph.Height(altura),
		asciigraph.Width(largura),
		asciigraph.Precision(0),
		asciigraph.Offset(3),
	}
	if len(cores) > 0 {
		opts = append(opts, asciigraph.SeriesColors(cores...))
	}
	if len(nomes) > 0 {
		opts = append(opts, asciigraph.SeriesLegends(nomes...))
	}
	if legenda != "" {
		opts = append(opts, asciigraph.Caption(legenda))
	}
	return asciigraph.PlotMany(series, opts...)
}

// larguraSerie escolhe uma largura agradável: ~6 colunas por ponto, presa entre
// 36 e 60, para o asciigraph interpolar séries curtas sem ficarem espremidas.
func larguraSerie(series [][]float64) int {
	n := 0
	for _, s := range series {
		if len(s) > n {
			n = len(s)
		}
	}
	w := n * 6
	if w < 36 {
		w = 36
	}
	if w > 60 {
		w = 60
	}
	return w
}

// barra100 desenha uma barra horizontal 100% empilhada a partir de partes já
// ordenadas (maior primeiro): cada fatia recebe um glifo distinto e devolve-se
// também a legenda glifo→rótulo com o respectivo percentual. Fatias além do
// número de glifos disponíveis são somadas em "outros".
func barra100(partes []ParRotulo, largura int) (string, []string) {
	glifos := []string{"█", "▓", "▒", "░", "▚", "▞"}
	var total int64
	for _, p := range partes {
		total += p.Valor
	}
	if total <= 0 || largura <= 0 {
		return strings.Repeat("░", maxInt(largura, 0)), nil
	}
	// agrupa o excedente em "outros" para não estourar a paleta de glifos
	mostra := partes
	var outros int64
	if len(partes) > len(glifos) {
		mostra = partes[:len(glifos)-1]
		for _, p := range partes[len(glifos)-1:] {
			outros += p.Valor
		}
	}

	cor := func(i int) string { return paletaSeg[i%len(paletaSeg)] }
	var bar strings.Builder
	var leg []string
	usados := 0
	emit := func(idx int, rotulo string, val int64) {
		g, c := glifos[idx], cor(idx)
		cels := int(float64(val) / float64(total) * float64(largura))
		if cels < 1 && val > 0 {
			cels = 1
		}
		if usados+cels > largura {
			cels = largura - usados
		}
		if cels <= 0 {
			return
		}
		bar.WriteString(pintar(c, strings.Repeat(g, cels)))
		usados += cels
		leg = append(leg, pintar(c, g)+" "+rotulo+" ("+pctStr(val, total)+")")
	}
	for i := range mostra {
		emit(i, mostra[i].Rotulo, mostra[i].Valor)
	}
	if outros > 0 {
		emit(len(glifos)-1, "outros", outros)
	}
	if usados < largura {
		bar.WriteString(strings.Repeat(" ", largura-usados))
	}
	return bar.String(), leg
}

// medidor desenha um gauge com escala 0–100 e um cursor (▲) sob o valor, usado
// no Health Score e nos componentes. Devolve duas linhas: barra e régua.
func medidor(score, largura int) (string, string) {
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	barra := "│" + barraFinaCor(int64(score), 100, largura, corZona(score)) + "│"
	pos := score * (largura - 1) / 100
	regua := " " + strings.Repeat(" ", pos) + pintar(corZona(score), "▲")
	return barra, regua
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// pctStr formata parte/total como percentual inteiro ("37%").
func pctStr(parte, total int64) string {
	if total == 0 {
		return "0%"
	}
	p := int(float64(parte)/float64(total)*100 + 0.5)
	return strconv.Itoa(p) + "%"
}
