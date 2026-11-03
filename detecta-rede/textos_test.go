package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Executar con `go test` despois de cambiar textos.json e antes de compilar.

func entradasTextos(t *testing.T) []entradaTexto {
	t.Helper()
	var entradas []entradaTexto
	if err := json.Unmarshal(textosJSON, &entradas); err != nil {
		t.Fatalf("textos.json no es válido: %v", err)
	}
	return entradas
}

func TestTextosValidos(t *testing.T) {
	vistos := map[string]bool{}
	for _, e := range entradasTextos(t) {
		if e.ID == "" {
			t.Errorf("hay un texto sin id: %q", e.Texto)
			continue
		}
		if vistos[e.ID] {
			t.Errorf("%s: id repetido", e.ID)
		}
		vistos[e.ID] = true
		if strings.TrimSpace(e.Texto) == "" {
			t.Errorf("%s: el texto está vacío", e.ID)
		}
		if got := comodinsDe(e.Texto); got != e.Comodines {
			t.Errorf("%s: el texto tiene los comodines %q pero debe tener %q, en el mismo orden\n    texto: %q",
				e.ID, got, e.Comodines, e.Texto)
		}
	}
}

// codigoFonte devolve o código de todos os .go do paquete agás as probas.
func codigoFonte(t *testing.T) string {
	t.Helper()
	ficheiros, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, f := range ficheiros {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		datos, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(datos)
	}
	return b.String()
}

func TestTextosUsados(t *testing.T) {
	codigo := codigoFonte(t)

	// Todo id usado con t() ou tf() ten que existir en textos.json.
	for _, m := range regexp.MustCompile(`\btf?\("([^"]+)"`).FindAllStringSubmatch(codigo, -1) {
		if _, ok := textos[m[1]]; !ok {
			t.Errorf("el código usa el texto %q, que no está en textos.json", m[1])
		}
	}

	// Todo texto de textos.json ten que aparecer no código (senón sobra).
	for _, e := range entradasTextos(t) {
		if !strings.Contains(codigo, `"`+e.ID+`"`) {
			t.Errorf("%s: está en textos.json pero el código no lo usa", e.ID)
		}
	}
}
