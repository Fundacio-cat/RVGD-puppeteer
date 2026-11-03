package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

const textosProba = `[
  {"id": "consola.estado_ok", "texto": "Correcto", "contexto": "estado"},
  {"id": "informe.resumo", "texto": "(%d errores, %d avisos)", "contexto": "resumo", "comodines": "%d %d"}
]
`

// preparaFolla exporta textosProba e escribe na columna "Texto nuevo" os textos
// indicados por número de fila (2 = primeiro texto).
func preparaFolla(t *testing.T, novos map[int]string) (rutaJSON, rutaXLSX string) {
	t.Helper()
	dir := t.TempDir()
	rutaJSON = filepath.Join(dir, "textos.json")
	rutaXLSX = filepath.Join(dir, "textos.xlsx")
	if err := os.WriteFile(rutaJSON, []byte(textosProba), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := exporta(rutaJSON, rutaXLSX); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenFile(rutaXLSX)
	if err != nil {
		t.Fatal(err)
	}
	for fila, texto := range novos {
		f.SetCellStr(follaTextos, "D"+string(rune('0'+fila)), texto)
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return rutaJSON, rutaXLSX
}

func TestImportaAplicaCambios(t *testing.T) {
	rutaJSON, rutaXLSX := preparaFolla(t, map[int]string{2: "Todo ben", 3: "(%d erros e %d avisos)"})
	if err := importa(rutaXLSX, rutaJSON); err != nil {
		t.Fatal(err)
	}
	entradas, err := leTextos(rutaJSON)
	if err != nil {
		t.Fatal(err)
	}
	if entradas[0].Texto != "Todo ben" || entradas[1].Texto != "(%d erros e %d avisos)" {
		t.Errorf("los textos no se han actualizado: %+v", entradas)
	}
}

func TestImportaRexeitaComodinsMal(t *testing.T) {
	rutaJSON, rutaXLSX := preparaFolla(t, map[int]string{2: "Todo ben", 3: "(con erros e avisos)"})
	err := importa(rutaXLSX, rutaJSON)
	if err == nil || !strings.Contains(err.Error(), "informe.resumo") {
		t.Fatalf("se esperaba un error por los comodines de informe.resumo, y ha llegado: %v", err)
	}
	datos, _ := os.ReadFile(rutaJSON)
	if string(datos) != textosProba {
		t.Errorf("con errores no se debe cambiar nada, pero textos.json ha cambiado:\n%s", datos)
	}
}
