//go:build windows

package main

import (
	"os/exec"
	"syscall"
	"unsafe"
)

// Xanelas nativas de Windows (MessageBoxW): non precisan ningunha biblioteca nin instalación.

const (
	mbYesNo         = 0x00000004
	mbIconError     = 0x00000010
	mbIconQuestion  = 0x00000020
	mbIconWarning   = 0x00000030
	mbIconInfo      = 0x00000040
	mbSetForeground = 0x00010000
	mbTopmost       = 0x00040000
	idYes           = 6
)

var messageBoxW = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")

func messageBox(titulo, texto string, estilo uintptr) uintptr {
	t, _ := syscall.UTF16PtrFromString(titulo)
	m, _ := syscall.UTF16PtrFromString(texto)
	ret, _, _ := messageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), estilo|mbSetForeground|mbTopmost)
	return ret
}

func preguntaSiNon(titulo, texto string) bool {
	return messageBox(titulo, texto, mbYesNo|mbIconQuestion) == idYes
}

func amosaResultado(titulo, texto string, n nivel) {
	icono := uintptr(mbIconInfo)
	switch n {
	case nivelErro:
		icono = mbIconError
	case nivelAviso:
		icono = mbIconWarning
	}
	messageBox(titulo, texto, icono)
}

// abreCartafol abre o Explorador co ficheiro seleccionado. A liña de ordes pásase
// tal cal porque explorer precisa /select,"ruta" sen máis comiñas.
func abreCartafol(ruta string) {
	cmd := exec.Command("explorer")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer /select,"` + ruta + `"`}
	cmd.Start()
}
