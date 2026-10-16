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
