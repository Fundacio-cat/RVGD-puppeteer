// Ferramenta para editar os textos de detecta-rede nunha folla de cálculo.
//
//	go run ./ferramentas/textos exporta textos.json textos.xlsx
//	go run ./ferramentas/textos importa textos.xlsx textos.json
//
// "exporta" crea unha folla cos textos actuais e unha columna "Texto nuevo" para
// encher. "importa" le esa columna, comproba os comodíns e actualiza
// textos.json: só cambian os textos que teñen algo en "Texto nuevo".
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/xuri/excelize/v2"
)

type entrada struct {
	ID        string `json:"id"`
	Texto     string `json:"texto"`
	Contexto  string `json:"contexto"`
	Comodines string `json:"comodines,omitempty"`
}

const (
	follaTextos     = "Textos"
	follaInstrucion = "Instrucciones"
	colTextoNovo    = "Texto nuevo"
	colID           = "ID"
)

var cabeceiras = []string{"Sección", "Dónde aparece", "Texto actual", colTextoNovo, "Comodines", colID}

// Nome lexible de cada grupo de textos, segundo o comezo do id.
var grupos = []struct{ prefixo, nome string }{
	{"xanela.", "Ventanas"},
	{"consola.", "Consola"},
	{"seccion.", "Nombres de los pasos"},
	{"wifi.", "Pregunta de la wifi"},
	{"veredicto.", "Resultado final"},
	{"informe.", "Informe"},
	{"rede.", "Paso 1 · Conexión del ordenador"},
	{"internet.", "Paso 2 · Acceso a Internet"},
	{"dns.", "Paso 3 · DNS"},
	{"ntp.", "Paso 4 · Servidor de hora"},
	{"webs.", "Paso 5 · Webs necesarias"},
	{"ruta.", "Paso 6 · Router"},
	{"conf.", "Paso 7 · Conexión con el servidor"},
	{"tunel.", "Paso 7 · Conexión con el servidor"},
}

func grupo(id string) string {
	for _, g := range grupos {
		if strings.HasPrefix(id, g.prefixo) {
			return g.nome
		}
	}
	return ""
}

var reComodin = regexp.MustCompile(`%%|%[-+# 0]*(?:\d+|\*)?(?:\.(?:\d+|\*))?[a-zA-Z]`)

// comodinsDe debe facer o mesmo que a función do mesmo nome en textos.go.
func comodinsDe(texto string) string {
	var res []string
	for _, c := range reComodin.FindAllString(texto, -1) {
		if c != "%%" {
			res = append(res, c)
		}
	}
	return strings.Join(res, " ")
}

func main() {
	if len(os.Args) != 4 || (os.Args[1] != "exporta" && os.Args[1] != "importa") {
		fmt.Fprintln(os.Stderr, "Uso:\n  go run ./ferramentas/textos exporta textos.json textos.xlsx\n  go run ./ferramentas/textos importa textos.xlsx textos.json")
		os.Exit(2)
	}
	var err error
	if os.Args[1] == "exporta" {
		err = exporta(os.Args[2], os.Args[3])
	} else {
		err = importa(os.Args[2], os.Args[3])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func leTextos(ruta string) ([]entrada, error) {
	datos, err := os.ReadFile(ruta)
	if err != nil {
		return nil, err
	}
	var entradas []entrada
	if err := json.Unmarshal(datos, &entradas); err != nil {
		return nil, fmt.Errorf("%s no es válido: %w", ruta, err)
	}
	return entradas, nil
}

// --- exporta ---

func exporta(rutaJSON, rutaXLSX string) error {
	entradas, err := leTextos(rutaJSON)
	if err != nil {
		return err
	}

	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetSheetName("Sheet1", follaTextos); err != nil {
		return err
	}
	if _, err := f.NewSheet(follaInstrucion); err != nil {
		return err
	}

	fonte := &excelize.Font{Family: "Arial", Size: 10}
	borde := []excelize.Border{
		{Type: "left", Color: "D0D0D0", Style: 1}, {Type: "right", Color: "D0D0D0", Style: 1},
		{Type: "top", Color: "D0D0D0", Style: 1}, {Type: "bottom", Color: "D0D0D0", Style: 1},
	}
	aliñado := &excelize.Alignment{WrapText: true, Vertical: "top"}
	estCabeceira, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "Arial", Size: 10, Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"2F5597"}},
		Alignment: &excelize.Alignment{WrapText: true, Vertical: "center"},
		Border:    borde,
	})
	estNormal, _ := f.NewStyle(&excelize.Style{Font: fonte, Alignment: aliñado, Border: borde})
	estGris, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Family: "Arial", Size: 9, Color: "808080"}, Alignment: aliñado, Border: borde})
	estEditable, _ := f.NewStyle(&excelize.Style{
		Font:      fonte,
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"FFF2CC"}},
		Alignment: aliñado,
		Border:    borde,
	})
	estComodin, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "Arial", Size: 10, Bold: true, Color: "C00000"},
		Alignment: aliñado,
		Border:    borde,
	})

	// Folla de textos.
	for i, c := range cabeceiras {
		cela, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellStr(follaTextos, cela, c)
	}
	f.SetCellStyle(follaTextos, "A1", "F1", estCabeceira)
	f.SetRowHeight(follaTextos, 1, 22)
	for i, e := range entradas {
		fila := i + 2
		valores := []string{grupo(e.ID), e.Contexto, e.Texto, "", e.Comodines, e.ID}
		for c, v := range valores {
			cela, _ := excelize.CoordinatesToCellName(c+1, fila)
			f.SetCellStr(follaTextos, cela, v)
		}
		f.SetCellStyle(follaTextos, fmt.Sprintf("A%d", fila), fmt.Sprintf("C%d", fila), estNormal)
		f.SetCellStyle(follaTextos, fmt.Sprintf("D%d", fila), fmt.Sprintf("D%d", fila), estEditable)
		f.SetCellStyle(follaTextos, fmt.Sprintf("E%d", fila), fmt.Sprintf("E%d", fila), estComodin)
		f.SetCellStyle(follaTextos, fmt.Sprintf("F%d", fila), fmt.Sprintf("F%d", fila), estGris)
	}
	for col, ancho := range map[string]float64{"A": 22, "B": 38, "C": 55, "D": 55, "E": 12, "F": 28} {
		f.SetColWidth(follaTextos, col, col, ancho)
	}
	f.SetPanes(follaTextos, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	f.AutoFilter(follaTextos, fmt.Sprintf("A1:F%d", len(entradas)+1), nil)

	// Folla de instrucións, con exemplos.
	estTitulo, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Family: "Arial", Size: 14, Bold: true}})
	estTexto, _ := f.NewStyle(&excelize.Style{Font: fonte, Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"}})
	estSubtitulo, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Family: "Arial", Size: 11, Bold: true}})
	liñas := []string{
		"1. En la hoja «Textos» hay una fila por cada texto que muestra el programa: dónde aparece y el texto actual.",
		"2. Para cambiar un texto, escribe el nuevo en la columna amarilla «Texto nuevo». Si no quieres cambiarlo, déjala vacía.",
		"3. No cambies las columnas «Comodines» ni «ID»: el programa las necesita para saber qué texto es cada uno.",
		"4. Si la columna «Comodines» (en rojo) tiene algo, como %s o %d, el texto nuevo tiene que llevar exactamente esos comodines y en el mismo orden. El programa los sustituye por los datos (un nombre, un número…). %% escribe el símbolo %.",
		"5. Para partir un texto en varias líneas dentro de la celda: Alt+Intro en Windows, o Ctrl+Opción+Intro en Mac.",
		"6. Los textos cortos (como «Correcto» o los nombres de los pasos) aparecen en una línea de la consola: mejor que no sean mucho más largos.",
		"7. Cuando termines, guarda el fichero y envíalo a puntoGAL.",
	}
	f.SetCellStr(follaInstrucion, "A1", "Cómo cambiar los textos del programa detecta-rede")
	f.SetCellStyle(follaInstrucion, "A1", "A1", estTitulo)
	for i, l := range liñas {
		cela := fmt.Sprintf("A%d", i+3)
		f.SetCellStr(follaInstrucion, cela, l)
		f.SetCellStyle(follaInstrucion, cela, cela, estTexto)
	}
	inicioEx := len(liñas) + 4
	f.SetCellStr(follaInstrucion, fmt.Sprintf("A%d", inicioEx), "Ejemplos")
	f.SetCellStyle(follaInstrucion, fmt.Sprintf("A%d", inicioEx), fmt.Sprintf("A%d", inicioEx), estSubtitulo)
	exemplos := [][]string{
		{"Texto actual", "Texto nuevo", "Comodines", "¿Correcto?"},
		{"Correcto", "Todo ben", "", "Sí"},
		{"RESULTADO: %s", "VEREDICTO: %s", "%s", "Sí"},
		{"(%d errores, %d avisos)", "(%d erros e %d avisos)", "%d %d", "Sí"},
		{"(%d errores, %d avisos)", "(con erros e avisos)", "%d %d", "No: faltan los dos %d"},
	}
	for i, ex := range exemplos {
		for c, v := range ex {
			cela, _ := excelize.CoordinatesToCellName(c+1, inicioEx+1+i)
			f.SetCellStr(follaInstrucion, cela, v)
			estilo := estNormal
			switch {
			case i == 0:
				estilo = estCabeceira
			case c == 1:
				estilo = estEditable
			case c == 2:
				estilo = estComodin
			}
			f.SetCellStyle(follaInstrucion, cela, cela, estilo)
		}
	}
	f.SetColWidth(follaInstrucion, "A", "A", 60)
	f.SetColWidth(follaInstrucion, "B", "B", 32)
	f.SetColWidth(follaInstrucion, "C", "C", 12)
	f.SetColWidth(follaInstrucion, "D", "D", 26)

	if indice, err := f.GetSheetIndex(follaInstrucion); err == nil {
		f.SetActiveSheet(indice)
	}
	f.MoveSheet(follaInstrucion, follaTextos)

	if err := f.SaveAs(rutaXLSX); err != nil {
		return err
	}
	fmt.Printf("Exportados %d textos a %s\n", len(entradas), rutaXLSX)
	return nil
}

// --- importa ---

func importa(rutaXLSX, rutaJSON string) error {
	entradas, err := leTextos(rutaJSON)
	if err != nil {
		return err
	}
	porID := map[string]int{}
	for i, e := range entradas {
		porID[e.ID] = i
	}

	f, err := excelize.OpenFile(rutaXLSX)
	if err != nil {
		return err
	}
	defer f.Close()
	filas, err := f.GetRows(follaTextos)
	if err != nil {
		return fmt.Errorf("no se encuentra la hoja «%s»: %w", follaTextos, err)
	}
	if len(filas) == 0 {
		return fmt.Errorf("la hoja «%s» está vacía", follaTextos)
	}
	colNovo, colIdent := -1, -1
	for i, c := range filas[0] {
		switch strings.TrimSpace(c) {
		case colTextoNovo:
			colNovo = i
		case colID:
			colIdent = i
		}
	}
	if colNovo < 0 || colIdent < 0 {
		return fmt.Errorf("faltan las columnas «%s» o «%s» en la hoja «%s»", colTextoNovo, colID, follaTextos)
	}

	var problemas []string
	cambios := 0
	for n, fila := range filas[1:] {
		if colNovo >= len(fila) || colIdent >= len(fila) {
			continue
		}
		novo := strings.ReplaceAll(fila[colNovo], "\r\n", "\n")
		if strings.TrimSpace(novo) == "" {
			continue
		}
		id := strings.TrimSpace(fila[colIdent])
		i, ok := porID[id]
		if !ok {
			problemas = append(problemas, fmt.Sprintf("fila %d: el ID %q no existe en %s", n+2, id, rutaJSON))
			continue
		}
		if got := comodinsDe(novo); got != entradas[i].Comodines {
			problemas = append(problemas, fmt.Sprintf("fila %d (%s): el texto nuevo tiene los comodines %q pero debe tener %q, en el mismo orden\n    texto nuevo: %q",
				n+2, id, got, entradas[i].Comodines, novo))
			continue
		}
		if novo != entradas[i].Texto {
			entradas[i].Texto = novo
			cambios++
		}
	}
	if len(problemas) > 0 {
		return fmt.Errorf("no se ha cambiado nada porque hay %d problemas:\n  %s", len(problemas), strings.Join(problemas, "\n  "))
	}

	if err := os.WriteFile(rutaJSON, serializa(entradas), 0o644); err != nil {
		return err
	}
	fmt.Printf("Actualizados %d textos en %s. Ejecuta `go test ./...` antes de compilar.\n", cambios, rutaJSON)
	return nil
}

// serializa escribe un texto por liña, cunha liña en branco entre grupos, igual
// que textos.json, para que os cambios se vexan ben nun diff.
func serializa(entradas []entrada) []byte {
	var b bytes.Buffer
	b.WriteString("[\n")
	anterior := ""
	for i, e := range entradas {
		prefixo, _, _ := strings.Cut(e.ID, ".")
		if i > 0 {
			b.WriteString(",\n")
			if prefixo != anterior {
				b.WriteString("\n")
			}
		}
		anterior = prefixo
		fmt.Fprintf(&b, `  {"id": %s, "texto": %s, "contexto": %s`, cadea(e.ID), cadea(e.Texto), cadea(e.Contexto))
		if e.Comodines != "" {
			fmt.Fprintf(&b, `, "comodines": %s`, cadea(e.Comodines))
		}
		b.WriteString("}")
	}
	b.WriteString("\n]\n")
	return b.Bytes()
}

func cadea(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}
