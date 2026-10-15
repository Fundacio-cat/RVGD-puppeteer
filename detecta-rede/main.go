// detecta-rede: comprobación de rede para os sensores RVGD.
//
// Execútase no ordenador da persoa que acolle o sensor, conectado á rede onde
// irá o sensor, e comproba os requisitos de conexión: DHCP, portal cativo, DNS,
// NTP, webs necesarias e, sobre todo, un túnel WireGuard real co cliente de
// probas. A configuración do túnel (conf/test.conf) incrústase no executable ao
// compilalo, de xeito que a persoa acolledora só recibe un ficheiro.
//
// As mensaxes que ve o usuario están en castelán, como o documento de requisitos.
// Na consola só se amosa o progreso; o detalle técnico vai ao informe.
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const version = "0.2.0"

// Endpoint por defecto, para a proba de DNS se non se pode ler a configuración.
const endpointPorDefecto = "profesor.galego.lacuinadel.cat"

const tituloXanela = "Comprobación de red - Sensor RVGD"

// conf/ contén un LEME.md (no repositorio) e test.conf (só en local, no .gitignore).
//
//go:embed conf
var confIncrustada embed.FS

type nivel string

const (
	nivelOK    nivel = "OK"
	nivelInfo  nivel = "INFO"
	nivelAviso nivel = "AVISO"
	nivelErro  nivel = "ERROR"
)

var gravidade = map[nivel]int{nivelOK: 0, nivelInfo: 0, nivelAviso: 1, nivelErro: 2}

type resultado struct {
	Seccion string `json:"seccion"`
	Proba   string `json:"prueba"`
	Nivel   nivel  `json:"nivel"`
	Detalle string `json:"detalle"`
	Accion  string `json:"accion,omitempty"`
}

type informe struct {
	Version    string      `json:"version"`
	Fecha      time.Time   `json:"fecha"`
	Sistema    string      `json:"sistema"`
	Veredicto  string      `json:"veredicto"`
	Rede       *infoRede   `json:"red,omitempty"`
	Resultados []resultado `json:"resultados"`

	out     io.Writer // detalle técnico (só no informe)
	consola io.Writer // progreso para a persoa acolledora

	seccion       string
	numSeccion    int
	peorSeccion   nivel
	seccionAberta bool
}

// Nomes das seccións tal como os ve a persoa acolledora na consola.
var seccionsConsola = []struct{ interna, visible string }{
	{"Equipo y red local", "Conexión del ordenador"},
	{"Acceso a Internet", "Acceso a Internet"},
	{"DNS", "Nombres de dominio (DNS)"},
	{"Hora (NTP)", "Servidor de hora"},
	{"Webs necesarias", "Webs necesarias"},
	{"Ruta de red", "Router"},
	{"Túnel con el servidor", "Conexión con puntoGAL (1-2 min)"},
}

func (in *informe) titulo(s string) {
	in.pechaSeccion()
	in.seccion = s
	in.numSeccion++
	in.peorSeccion = nivelOK
	in.seccionAberta = true
	fmt.Fprintf(in.out, "\n== %s ==\n", s)

	visible := s
	for _, sc := range seccionsConsola {
		if sc.interna == s {
			visible = sc.visible
		}
	}
	fmt.Fprintf(in.consola, "  %d/%d  %-40s", in.numSeccion, len(seccionsConsola), visible+"...")
}

func (in *informe) pechaSeccion() {
	if !in.seccionAberta {
		return
	}
	in.seccionAberta = false
	estado := "Correcto"
	switch in.peorSeccion {
	case nivelAviso:
		estado = "Revisar"
	case nivelErro:
		estado = "Problema"
	}
	fmt.Fprintln(in.consola, estado)
}

func (in *informe) add(proba string, n nivel, detalle, accion string) {
	in.Resultados = append(in.Resultados, resultado{
		Seccion: in.seccion,
		Proba:   proba,
		Nivel:   n,
		Detalle: detalle,
		Accion:  accion,
	})
	if gravidade[n] > gravidade[in.peorSeccion] {
		in.peorSeccion = n
	}
	fmt.Fprintf(in.out, "  [%-5s] %s: %s\n", n, proba, detalle)
	if accion != "" {
		fmt.Fprintf(in.out, "          -> %s\n", accion)
	}
}

func (in *informe) conta(n nivel) int {
	total := 0
	for _, r := range in.Resultados {
		if r.Nivel == n {
			total++
		}
	}
	return total
}

func main() {
	rutaConf := flag.String("conf", "", "fichero de configuración de WireGuard (por defecto, el incluido en el programa)")
	servidor := flag.String("servidor", "10.66.66.1", "IP del servidor dentro del túnel")
	bd := flag.String("bd", "", "host:puerto de Postgres dentro del túnel (por defecto se prueban los puertos 51432 y 5432 del servidor)")
	duracion := flag.Duration("duracion", 60*time.Second, "duración de la prueba de estabilidad (0 para omitirla)")
	nonInteractivo := flag.Bool("no-interactivo", false, "no mostrar ventanas ni preguntas")
	detalle := flag.Bool("detalle", false, "mostrar el detalle técnico en la consola")
	flag.Parse()

	dir := directorioPrograma()

	var copia strings.Builder
	in := &informe{
		Version: version,
		Fecha:   time.Now(),
		Sistema: runtime.GOOS + "/" + runtime.GOARCH,
		out:     &copia,
		consola: os.Stdout,
	}
	if *detalle {
		in.out = io.MultiWriter(os.Stdout, &copia)
		in.consola = io.Discard
	}

	fmt.Fprintf(&copia, "Comprobación de red para sensores RVGD (v%s)\nFecha: %s\n", version, in.Fecha.Format("2006-01-02 15:04:05"))
	fmt.Println()
	fmt.Println("  COMPROBACIÓN DE RED PARA EL SENSOR")
	fmt.Println("  ==================================")
	fmt.Println()
	fmt.Println("  Se está comprobando si esta red es válida para el sensor.")
	fmt.Println("  Tarda unos 3 minutos. No cerréis esta ventana.")
	fmt.Println()

	rede := comprobaRedeLocal(in)
	if rede != nil && rede.WiFi && !*nonInteractivo {
		pregunta := "¿Estáis conectados a la wifi principal de vuestro router?\n\n(Si es la wifi de invitados, responded «No».)"
		if rede.SSID != "" {
			pregunta = "Este ordenador está conectado a la wifi «" + rede.SSID + "».\n\n" + pregunta
		}
		if preguntaSiNon(tituloXanela, pregunta) {
			in.add("Wifi principal", nivelOK, "confirmado por el usuario", "")
		} else {
			in.add("Wifi principal", nivelErro, "el ordenador no está conectado a la wifi principal del router",
				"Conectad el ordenador a la wifi principal del router (o por cable) y volved a abrir este programa.")
		}
	}

	comprobaInternet(in)

	cfg, orixeConf, errConf := cargaConfiguracion(*rutaConf, dir)
	endpointHost := endpointPorDefecto
	if cfg != nil {
		// Se o Endpoint é unha IP, a proba de DNS faise co nome do servidor por defecto.
		if host, _, err := separaEndpoint(cfg.Endpoint); err == nil && net.ParseIP(host) == nil {
			endpointHost = host
		}
	}

	comprobaDNS(in, endpointHost)
	comprobaNTP(in)
	comprobaWebs(in)
	comprobaRuta(in)
	comprobaTunel(in, cfg, errConf, orixeConf, *servidor, *bd, *duracion, rede != nil && rede.WiFi)
	in.pechaSeccion()

	erros, avisos := in.conta(nivelErro), in.conta(nivelAviso)
	titular, global := "LA RED ES VÁLIDA", nivelOK
	switch {
	case erros > 0:
		titular, global = "LA RED NO ES VÁLIDA", nivelErro
		in.Veredicto = "NO APTA: hay problemas que impedirán que el sensor se conecte."
	case avisos > 0:
		titular, global = "LA RED ES VÁLIDA (CON AVISOS)", nivelAviso
		in.Veredicto = "APTA CON AVISOS: el sensor debería poder conectarse, pero hay puntos a revisar."
	default:
		in.Veredicto = "APTA: la red cumple los requisitos para el sensor."
	}
	fmt.Fprintf(&copia, "\n== Resultado ==\n  %s\n  (%d errores, %d avisos)\n", in.Veredicto, erros, avisos)

	fmt.Printf("\n  RESULTADO: %s\n", titular)
	rutaInforme := gardaInforme(in, copia.String(), dir)

	if !*nonInteractivo {
		amosaResultado(tituloXanela, textoResultado(in, titular, global, rutaInforme), global)
		if rutaInforme != "" {
			abreCartafol(rutaInforme)
		}
	}
	if erros > 0 {
		os.Exit(2)
	}
}

// cargaConfiguracion le a configuración do túnel: a do parámetro -conf se se
// indica; se non, a incrustada no executable e, se non a hai, un test.conf ao
// carón do programa (útil para probas).
func cargaConfiguracion(ruta, dir string) (*configWG, string, error) {
	if ruta != "" {
		cfg, err := cargaConfigWG(ruta)
		return cfg, ruta, err
	}
	if data, err := confIncrustada.ReadFile("conf/test.conf"); err == nil {
		cfg, err := parseConfigWG(data)
		return cfg, "configuración incluida en el programa", err
	}
	junto := filepath.Join(dir, "test.conf")
	if _, err := os.Stat(junto); err == nil {
		cfg, err := cargaConfigWG(junto)
		return cfg, junto, err
	}
	return nil, "configuración incluida en el programa",
		fmt.Errorf("el programa se ha generado sin el fichero conf/test.conf")
}

// textoResultado prepara a mensaxe da xanela final en linguaxe sinxela:
// o veredicto, que hai que facer e onde está o informe.
func textoResultado(in *informe, titular string, global nivel, rutaInforme string) string {
	var b strings.Builder
	b.WriteString(titular + "\n\n")

	switch global {
	case nivelOK:
		b.WriteString("Esta red cumple todos los requisitos. Podéis conectar el sensor al router.")
	case nivelAviso:
		b.WriteString("El sensor debería funcionar con esta red, pero conviene revisar:")
	case nivelErro:
		b.WriteString("El sensor no podrá conectarse con esta red. Hay que resolver:")
	}

	const maximo = 6
	amosados := 0
	for _, n := range []nivel{nivelErro, nivelAviso} {
		for _, r := range in.Resultados {
			if r.Nivel != n {
				continue
			}
			amosados++
			if amosados > maximo {
				continue
			}
			texto := r.Accion
			if texto == "" {
				texto = r.Detalle
			}
			fmt.Fprintf(&b, "\n\n• %s:\n   %s", r.Proba, texto)
		}
	}
	if amosados > maximo {
		fmt.Fprintf(&b, "\n\n(y %d puntos más en el informe)", amosados-maximo)
	}

	if rutaInforme != "" {
		fmt.Fprintf(&b, "\n\nSe ha guardado un informe en el fichero «%s». Enviadlo a puntoGAL.", filepath.Base(rutaInforme))
		if runtime.GOOS == "windows" {
			b.WriteString("\n\nAl pulsar Aceptar se abrirá la carpeta donde está.")
		} else {
			b.WriteString("\n\n" + rutaInforme)
		}
	} else {
		b.WriteString("\n\nNo se ha podido guardar el informe: haced una foto o captura de esta ventana y enviadla a puntoGAL.")
	}
	return b.String()
}

func directorioPrograma() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

// gardaInforme garda o informe en texto e JSON e devolve a ruta do .txt ("" se non foi posible).
func gardaInforme(in *informe, texto, dir string) string {
	base := "informe-red-" + in.Fecha.Format("20060102-150405")
	jsonData, _ := json.MarshalIndent(in, "", "  ")

	directorios := []string{dir, "."}
	if home, err := os.UserHomeDir(); err == nil {
		directorios = append(directorios, filepath.Join(home, "Desktop"), home)
	}
	for _, d := range directorios {
		txt := filepath.Join(d, base+".txt")
		if err := os.WriteFile(txt, []byte(texto), 0o644); err != nil {
			continue
		}
		os.WriteFile(filepath.Join(d, base+".json"), jsonData, 0o644)
		if abs, err := filepath.Abs(txt); err == nil {
			txt = abs
		}
		fmt.Printf("\n  Informe guardado en: %s\n", txt)
		return txt
	}
	return ""
}
