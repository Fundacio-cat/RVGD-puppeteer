//go:build !windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Fóra de Windows, as preguntas e o resultado amósanse na consola.

var entrada = bufio.NewReader(os.Stdin)

func preguntaSiNon(_, texto string) bool {
	fmt.Printf("\n%s [s/n]: ", texto)
	resp, _ := entrada.ReadString('\n')
	return !strings.HasPrefix(strings.ToLower(strings.TrimSpace(resp)), "n")
}

func amosaResultado(_, texto string, _ nivel) {
	fmt.Printf("\n%s\n\nPulsad Intro para salir...", texto)
	entrada.ReadString('\n')
}

func abreCartafol(string) {}
