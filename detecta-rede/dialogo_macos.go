//go:build darwin

package main

import (
	"os/exec"
	"strings"
)

// Xanelas nativas de macOS mediante osascript (vén co sistema). Os textos
// pásanse como argumentos para non ter que escapalos dentro do AppleScript.

func preguntaSiNon(titulo, texto string) bool {
	si, non := t("xanela.boton_si"), t("xanela.boton_non")
	out, err := exec.Command("osascript",
		"-e", "on run argv",
		"-e", `display dialog (item 1 of argv) with title (item 2 of argv) buttons {item 4 of argv, item 3 of argv} default button (item 3 of argv) with icon note`,
		"-e", "end run",
		texto, titulo, si, non).Output()
	if err != nil {
		// O botón negativo non é o de cancelar, polo que un erro só pode vir de
		// que osascript non estea dispoñible: nese caso non bloqueamos a proba.
		return true
	}
	return strings.TrimSpace(string(out)) == "button returned:"+si
}

func amosaResultado(titulo, texto string, n nivel) {
	icono := "note"
	switch n {
	case nivelErro:
		icono = "stop"
	case nivelAviso:
		icono = "caution"
	}
	exec.Command("osascript",
		"-e", "on run argv",
		"-e", `display dialog (item 1 of argv) with title (item 2 of argv) buttons {item 3 of argv} default button (item 3 of argv) with icon `+icono,
		"-e", "end run",
		texto, titulo, t("xanela.boton_aceptar")).Run()
}

// abreCartafol abre o Finder co ficheiro seleccionado.
func abreCartafol(ruta string) {
	exec.Command("open", "-R", ruta).Start()
}
