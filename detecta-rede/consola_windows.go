//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// A consola clásica de Windows non usa UTF-8 por defecto e os acentos do
// castelán veríanse mal. Tamén lle poñemos un título claro á xanela.
func init() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	kernel32.NewProc("SetConsoleOutputCP").Call(65001)
	kernel32.NewProc("SetConsoleCP").Call(65001)
	if titulo, err := syscall.UTF16PtrFromString(tituloXanela); err == nil {
		kernel32.NewProc("SetConsoleTitleW").Call(uintptr(unsafe.Pointer(titulo)))
	}
}
