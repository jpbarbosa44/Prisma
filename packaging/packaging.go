// Package packaging guarda o ícone do Prisma (SVG original e as versões PNG,
// ICO e ICNS geradas dele) embutido no binário, para o prisma instalar-atalho
// e o prisma-app.
package packaging

import _ "embed"

//go:embed prisma.svg
var IconeSVG []byte

//go:embed prisma.png
var IconePNG []byte

//go:embed prisma.ico
var IconeICO []byte

//go:embed prisma.icns
var IconeICNS []byte

// ícones da web instalável (PWA) e do atalho no iPhone, renderizados do SVG
//
//go:embed prisma-192.png
var Icone192 []byte

//go:embed prisma-512.png
var Icone512 []byte

//go:embed prisma-180.png
var Icone180 []byte
