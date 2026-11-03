//go:build !windows && !darwin

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Fóra de Windows e macOS (é dicir, en Linux), as preguntas e o resultado amósanse na consola.

var entrada = bufio.NewReader(os.Stdin)

func preguntaSiNon(_, texto string) bool {
	fmt.Printf("\n%s %s: ", texto, t("consola.si_non"))
	resp, _ := entrada.ReadString('\n')
	return !strings.HasPrefix(strings.ToLower(strings.TrimSpace(resp)), strings.ToLower(t("consola.resposta_non")))
}

func amosaResultado(_, texto string, _ nivel) {
	fmt.Printf("\n%s\n\n%s", texto, t("consola.pulsar_intro"))
	entrada.ReadString('\n')
}

func abreCartafol(string) {}
