package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Todos os textos que ve o usuario están en textos.json, para poder cambialos
// sen tocar o código. O ficheiro incrústase no executable ao compilar.
//
// Cada texto pode levar comodíns (%s, %d…) que o programa substitúe por datos.
// O campo "comodines" indica cales debe ter o texto, e na mesma orde: se se
// cambian, os datos aparecen mal. textos_test.go compróbao.

//go:embed textos.json
var textosJSON []byte

type entradaTexto struct {
	ID        string `json:"id"`
	Texto     string `json:"texto"`
	Contexto  string `json:"contexto"`
	Comodines string `json:"comodines,omitempty"`
}

var textos = cargaTextos(textosJSON)

func cargaTextos(datos []byte) map[string]string {
	var entradas []entradaTexto
	if err := json.Unmarshal(datos, &entradas); err != nil {
		panic("textos.json no es válido: " + err.Error())
	}
	m := make(map[string]string, len(entradas))
	for _, e := range entradas {
		m[e.ID] = e.Texto
	}
	return m
}

// t devolve o texto co identificador id. Se non existe, devolve o propio id
// entre corchetes para que se note sen que o programa falle.
func t(id string) string {
	if texto, ok := textos[id]; ok {
		return texto
	}
	return "[" + id + "]"
}

// tf devolve o texto co identificador id cos comodíns substituídos por args.
func tf(id string, args ...any) string {
	return fmt.Sprintf(t(id), args...)
}

var reComodin = regexp.MustCompile(`%%|%[-+# 0]*(?:\d+|\*)?(?:\.(?:\d+|\*))?[a-zA-Z]`)

// comodinsDe devolve os comodíns dun texto, na orde en que aparecen, separados
// por espazos ("%s %d"). %% non conta: escribe o símbolo %.
func comodinsDe(texto string) string {
	var res []string
	for _, c := range reComodin.FindAllString(texto, -1) {
		if c != "%%" {
			res = append(res, c)
		}
	}
	return strings.Join(res, " ")
}
