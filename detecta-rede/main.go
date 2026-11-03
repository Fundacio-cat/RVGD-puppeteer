// detecta-rede: comprobación de rede para os sensores RVGD.
//
// Execútase no ordenador da persoa que acolle o sensor, conectado á rede onde
// irá o sensor, e comproba os requisitos de conexión: DHCP, portal cativo, DNS,
// NTP, webs necesarias e, sobre todo, un túnel WireGuard real co cliente de
// probas. A configuración do túnel (conf/test.conf) incrústase no executable ao
// compilalo, de xeito que a persoa acolledora só recibe un ficheiro.
//
// Os textos que ve o usuario están en textos.json (ver textos.go). Na consola só
// se amosa o progreso; o detalle técnico vai ao informe.
package main

import (
	"embed"
	"encoding/json"
	"errors"
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

// Seccións da proba, na orde en que se fan: o seu título no informe e o nome
// que ve a persoa acolledora na consola.
var seccions = map[string]struct{ informe, consola string }{
	"rede_local": {"seccion.rede_local.informe", "seccion.rede_local.consola"},
	"internet":   {"seccion.internet.informe", "seccion.internet.consola"},
	"dns":        {"seccion.dns.informe", "seccion.dns.consola"},
	"ntp":        {"seccion.ntp.informe", "seccion.ntp.consola"},
	"webs":       {"seccion.webs.informe", "seccion.webs.consola"},
	"ruta":       {"seccion.ruta.informe", "seccion.ruta.consola"},
	"tunel":      {"seccion.tunel.informe", "seccion.tunel.consola"},
}

func (in *informe) titulo(id string) {
	in.pechaSeccion()
	s := seccions[id]
	in.seccion = t(s.informe)
	in.numSeccion++
	in.peorSeccion = nivelOK
	in.seccionAberta = true
	fmt.Fprintf(in.out, "\n== %s ==\n", in.seccion)
	fmt.Fprintf(in.consola, "  %d/%d  %-40s", in.numSeccion, len(seccions), t(s.consola)+"...")
}

func (in *informe) pechaSeccion() {
	if !in.seccionAberta {
		return
	}
	in.seccionAberta = false
	estado := t("consola.estado_ok")
	switch in.peorSeccion {
	case nivelAviso:
		estado = t("consola.estado_aviso")
	case nivelErro:
		estado = t("consola.estado_erro")
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
	bd := flag.String("bd", "", "host:puerto de Postgres dentro del túnel (por defecto, el puerto 51432 del servidor)")
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

	fmt.Fprintln(&copia, tf("informe.cabeceira", version))
	fmt.Fprintln(&copia, tf("informe.data", in.Fecha.Format("2006-01-02 15:04:05")))
	cabeceira := t("consola.cabeceira")
	fmt.Println()
	fmt.Println("  " + cabeceira)
	fmt.Println("  " + strings.Repeat("=", len([]rune(cabeceira))))
	fmt.Println()
	fmt.Println("  " + t("consola.intro1"))
	fmt.Println("  " + t("consola.intro2"))
	fmt.Println()

	rede := comprobaRedeLocal(in)
	if rede != nil && rede.WiFi && !*nonInteractivo {
		pregunta := t("wifi.pregunta")
		if rede.SSID != "" {
			pregunta = tf("wifi.pregunta_ssid", rede.SSID) + "\n\n" + pregunta
		}
		if preguntaSiNon(t("xanela.titulo"), pregunta) {
			in.add(t("wifi.proba"), nivelOK, t("wifi.ok"), "")
		} else {
			in.add(t("wifi.proba"), nivelErro, t("wifi.erro_detalle"), t("wifi.erro_accion"))
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
	titular, global := t("veredicto.titular_ok"), nivelOK
	switch {
	case erros > 0:
		titular, global = t("veredicto.titular_erro"), nivelErro
		in.Veredicto = t("veredicto.informe_erro")
	case avisos > 0:
		titular, global = t("veredicto.titular_aviso"), nivelAviso
		in.Veredicto = t("veredicto.informe_aviso")
	default:
		in.Veredicto = t("veredicto.informe_ok")
	}
	fmt.Fprintf(&copia, "\n== %s ==\n  %s\n  %s\n", t("informe.resultado"), in.Veredicto, tf("informe.resumo", erros, avisos))

	fmt.Printf("\n  %s\n", tf("consola.resultado", titular))
	rutaInforme := gardaInforme(in, copia.String(), dir)

	if !*nonInteractivo {
		amosaResultado(t("xanela.titulo"), textoResultado(in, titular, global, rutaInforme), global)
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
		return cfg, t("conf.incluida"), err
	}
	junto := filepath.Join(dir, "test.conf")
	if _, err := os.Stat(junto); err == nil {
		cfg, err := cargaConfigWG(junto)
		return cfg, junto, err
	}
	return nil, t("conf.incluida"), errors.New(t("conf.sen_ficheiro"))
}

// textoResultado prepara a mensaxe da xanela final en linguaxe sinxela:
// o veredicto, que hai que facer e onde está o informe.
func textoResultado(in *informe, titular string, global nivel, rutaInforme string) string {
	var b strings.Builder
	b.WriteString(titular + "\n\n")

	switch global {
	case nivelOK:
		b.WriteString(t("veredicto.xanela_ok"))
	case nivelAviso:
		b.WriteString(t("veredicto.xanela_aviso"))
	case nivelErro:
		b.WriteString(t("veredicto.xanela_erro"))
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
		b.WriteString("\n\n" + tf("xanela.mais_puntos", amosados-maximo))
	}

	if rutaInforme != "" {
		b.WriteString("\n\n" + tf("xanela.informe_gardado", filepath.Base(rutaInforme)))
		if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
			b.WriteString("\n\n" + t("xanela.abrir_cartafol"))
		} else {
			b.WriteString("\n\n" + rutaInforme)
		}
	} else {
		b.WriteString("\n\n" + t("xanela.sen_informe"))
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
	base := t("informe.nome_ficheiro") + "-" + in.Fecha.Format("20060102-150405")
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
		fmt.Printf("\n  %s\n", tf("consola.informe_gardado", txt))
		return txt
	}
	return ""
}
