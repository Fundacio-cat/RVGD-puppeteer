package main

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/netip"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

func clienteHTTP(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		// Non seguimos redireccións: abonda con saber que o servidor responde.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// --- Acceso a Internet e portal cativo ---

func comprobaInternet(in *informe) {
	in.titulo("Acceso a Internet")

	resp, err := clienteHTTP(10 * time.Second).Get("http://connectivitycheck.gstatic.com/generate_204")
	if err != nil {
		in.add("Conexión a Internet", nivelErro, fmt.Sprintf("no hay acceso a Internet (%v)", err),
			"Comprobad con otro dispositivo que el router tiene Internet.")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		in.add("Conexión a Internet", nivelOK, "acceso directo, sin página de inicio de sesión", "")
		return
	}
	detalle := fmt.Sprintf("la red responde con otra página (código %d", resp.StatusCode)
	if loc := resp.Header.Get("Location"); loc != "" {
		detalle += ", redirige a " + loc
	}
	in.add("Conexión a Internet", nivelErro, detalle+"): probablemente hay un portal de inicio de sesión",
		"El sensor no puede iniciar sesión en portales: hace falta una red sin página de acceso.")
}

// --- DNS ---

func resolverPara(servidor, protocolo string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, protocolo, servidor)
		},
	}
}

func resolve4(r *net.Resolver, host string) ([]netip.Addr, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ips, err := r.LookupNetIP(ctx, "ip4", host)
	for i := range ips {
		ips[i] = ips[i].Unmap()
	}
	return ips, err
}

func textoIPs(ips []netip.Addr) string {
	s := make([]string, len(ips))
	for i, ip := range ips {
		s[i] = ip.String()
	}
	return strings.Join(s, ", ")
}

func ipsBloqueadas(ips []netip.Addr) bool {
	for _, ip := range ips {
		if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() {
			return true
		}
	}
	return false
}

func comparten(a, b []netip.Addr) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

func comprobaDNS(in *informe, host string) {
	in.titulo("DNS")

	ipsServidor, err := resolve4(net.DefaultResolver, host)
	if err != nil {
		in.add("Servidor de puntoGAL", nivelErro, fmt.Sprintf("no se puede resolver %s con el DNS del router", host),
			"Revisad que no haya filtros de DNS en el router o del operador.")
	} else {
		in.add("Servidor de puntoGAL", nivelOK, fmt.Sprintf("%s → %s", host, textoIPs(ipsServidor)), "")
	}

	if ips, err := resolve4(net.DefaultResolver, "www.google.com"); err != nil {
		in.add("Google", nivelErro, "no se puede resolver www.google.com con el DNS del router",
			"Revisad que no haya filtros de DNS en el router o del operador.")
	} else if ipsBloqueadas(ips) {
		in.add("Google", nivelErro, fmt.Sprintf("www.google.com se resuelve a una dirección bloqueada (%s): hay un filtro de contenido", textoIPs(ips)),
			"Desactivad los filtros de contenido o el control parental del router o del operador.")
	} else {
		in.add("Google", nivelOK, "www.google.com se resuelve correctamente", "")
	}

	inexistente := fmt.Sprintf("rvgd-%08x.example.com", rand.Uint32())
	if ips, err := resolve4(net.DefaultResolver, inexistente); err == nil && len(ips) > 0 {
		in.add("Dominios inexistentes", nivelAviso, fmt.Sprintf("el DNS responde a dominios que no existen (%s): el operador redirige las consultas", textoIPs(ips)),
			"Suele ser un servicio del operador («protección», «sugerencias»…); conviene desactivarlo.")
	} else {
		in.add("Dominios inexistentes", nivelOK, "se tratan correctamente", "")
	}

	// O sensor usa o DNS que lle dá o router, pero o documento de requisitos
	// pide que tamén se poidan facer consultas DNS externas.
	for _, proto := range []string{"udp", "tcp"} {
		proba := "DNS externo " + strings.ToUpper(proto) + " 53"
		ips, err := resolve4(resolverPara("8.8.8.8:53", proto), host)
		switch {
		case err != nil:
			in.add(proba, nivelAviso, fmt.Sprintf("el router no deja consultar servidores DNS externos por %s 53", strings.ToUpper(proto)),
				"No es imprescindible (el sensor usa el DNS del router), pero el documento de requisitos pide permitirlo.")
		case proto == "udp" && len(ipsServidor) > 0 && !comparten(ips, ipsServidor):
			in.add(proba, nivelAviso, fmt.Sprintf("el DNS del router da otra dirección para %s (%s) que un DNS público (%s)", host, textoIPs(ipsServidor), textoIPs(ips)),
				"Puede haber un filtro o una redirección de DNS en el router o del operador.")
		default:
			in.add(proba, nivelOK, "funciona", "")
		}
	}
}

// --- NTP ---

func sntp(servidor string) (time.Duration, error) {
	c, err := net.DialTimeout("udp", net.JoinHostPort(servidor, "123"), 5*time.Second)
	if err != nil {
		return 0, err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))

	peticion := make([]byte, 48)
	peticion[0] = 0x1b // LI=0, versión 3, modo cliente
	t1 := time.Now()
	if _, err := c.Write(peticion); err != nil {
		return 0, err
	}
	resp := make([]byte, 48)
	n, err := c.Read(resp)
	t4 := time.Now()
	if err != nil {
		return 0, err
	}
	if n < 48 {
		return 0, fmt.Errorf("respuesta NTP incompleta")
	}

	const epocaNTP = 2208988800 // segundos entre 1900 e 1970
	seg := int64(binary.BigEndian.Uint32(resp[40:44])) - epocaNTP
	frac := int64((uint64(binary.BigEndian.Uint32(resp[44:48])) * 1e9) >> 32)
	horaServidor := time.Unix(seg, frac)
	return horaServidor.Sub(t1.Add(t4.Sub(t1) / 2)), nil
}

func comprobaNTP(in *informe) {
	in.titulo("Hora (NTP)")

	var ultimo error
	for _, s := range []string{"pool.ntp.org", "debian.pool.ntp.org", "time.google.com"} {
		desfase, err := sntp(s)
		if err != nil {
			ultimo = err
			continue
		}
		in.add("Servidor de hora", nivelOK, fmt.Sprintf("respuesta de %s (el reloj de este equipo difiere %s)", s, desfase.Round(time.Millisecond)), "")
		return
	}
	in.add("Servidor de hora", nivelErro, fmt.Sprintf("no responde ningún servidor de hora por UDP 123 (%v)", ultimo),
		"Permitid la salida UDP 123 en el router. Sin hora, el sensor no puede conectarse.")
}

// --- Webs necesarias ---

type web struct {
	url     string
	critica bool
	uso     string
}

var webs = []web{
	{"https://www.google.com/", true, "búsquedas"},
	{"https://github.com/", true, "actualizaciones del sensor"},
	{"https://raw.githubusercontent.com/", false, "instalación"},
	{"https://nodejs.org/", false, "instalación"},
	{"https://archive.raspberrypi.com/", false, "actualizaciones del sistema"},
	{"https://deb.debian.org/", false, "actualizaciones del sistema"},
	{"https://repo.zabbix.com/", false, "monitorización"},
	{"https://deb.nodesource.com/", false, "instalación"},
	{"http://deb.debian.org/", false, "actualizaciones del sistema por HTTP"},
	{"http://archive.raspberrypi.com/", false, "actualizaciones del sistema por HTTP"},
}

func comprobaWebs(in *informe) {
	in.titulo("Webs necesarias")

	cliente := clienteHTTP(15 * time.Second)
	errs := make([]error, len(webs))
	var wg sync.WaitGroup
	for i, w := range webs {
		wg.Add(1)
		go func(i int, url string) {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodGet, url, nil)
			req.Header.Set("User-Agent", "detecta-rede/"+version)
			resp, err := cliente.Do(req)
			if err == nil {
				resp.Body.Close()
			}
			errs[i] = err
		}(i, w.url)
	}
	wg.Wait()

	for i, w := range webs {
		proba := strings.TrimSuffix(w.url, "/")
		if errs[i] == nil {
			in.add(proba, nivelOK, "accesible ("+w.uso+")", "")
			continue
		}
		n := nivelAviso
		if w.critica {
			n = nivelErro
		}
		var errCert *tls.CertificateVerificationError
		if errors.As(errs[i], &errCert) {
			in.add(proba, n, "certificado no válido: algún filtro (del router, del operador o un antivirus de red) está interceptando las conexiones seguras",
				"Desactivad los filtros de contenido o de seguridad del router o del operador.")
			continue
		}
		porto := "443"
		if strings.HasPrefix(w.url, "http://") {
			porto = "80"
		}
		in.add(proba, n, fmt.Sprintf("no accesible (%v)", errs[i]),
			fmt.Sprintf("Permitid el acceso a esta web desde el router (TCP %s). Se usa para: %s.", porto, w.uso))
	}
}

// --- Ruta de rede (dobre router, CGNAT) ---

var (
	reSalto = regexp.MustCompile(`^\s*(\d+)\??:?\s+(.*)$`)
	reIPv4  = regexp.MustCompile(`\b(\d{1,3}(?:\.\d{1,3}){3})\b`)
)

func traceroute() (map[int]netip.Addr, error) {
	var nome string
	var args []string
	switch runtime.GOOS {
	case "windows":
		nome, args = "tracert", []string{"-d", "-h", "3", "-w", "1000", "8.8.8.8"}
	case "linux":
		if _, err := exec.LookPath("tracepath"); err == nil {
			nome, args = "tracepath", []string{"-n", "-m", "3", "8.8.8.8"}
		} else {
			nome, args = "traceroute", []string{"-n", "-m", "3", "-w", "1", "8.8.8.8"}
		}
	default:
		nome, args = "traceroute", []string{"-n", "-m", "3", "-w", "1", "8.8.8.8"}
	}
	if _, err := exec.LookPath(nome); err != nil {
		return nil, fmt.Errorf("%s no está disponible", nome)
	}

	out, _ := executa(60*time.Second, nome, args...)
	saltos := map[int]netip.Addr{}
	for _, lina := range strings.Split(out, "\n") {
		m := reSalto.FindStringSubmatch(lina)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		if _, xa := saltos[n]; xa {
			continue
		}
		if ip := reIPv4.FindString(m[2]); ip != "" {
			if addr, err := netip.ParseAddr(ip); err == nil {
				saltos[n] = addr
			}
		}
	}
	return saltos, nil
}

func comprobaRuta(in *informe) {
	in.titulo("Ruta de red")

	saltos, err := traceroute()
	if err != nil || len(saltos) == 0 {
		motivo := "sin respuesta"
		if err != nil {
			motivo = err.Error()
		}
		in.add("Routers intermedios", nivelInfo, "no se ha podido comprobar la ruta ("+motivo+")", "")
		return
	}

	numeros := make([]int, 0, len(saltos))
	for n := range saltos {
		numeros = append(numeros, n)
	}
	sort.Ints(numeros)
	partes := make([]string, len(numeros))
	for i, n := range numeros {
		partes[i] = fmt.Sprintf("%d: %s", n, saltos[n])
	}
	ruta := strings.Join(partes, ", ")

	cgnat := netip.MustParsePrefix("100.64.0.0/10")
	for _, ip := range saltos {
		if cgnat.Contains(ip) {
			in.add("CGNAT", nivelInfo, "el operador usa CGNAT (dirección pública compartida); el túnel funciona igualmente", "")
			break
		}
	}

	// Dous routers domésticos encadeados (p. ex. un router ou mesh propio detrás do
	// do operador): o sensor podería acabar conectado a un aparello distinto do do ordenador.
	domestica := netip.MustParsePrefix("192.168.0.0/16")
	s1, ok1 := saltos[1]
	s2, ok2 := saltos[2]
	if ok1 && ok2 && s1.IsPrivate() && domestica.Contains(s2) {
		in.add("Routers intermedios", nivelAviso, "parece que hay dos routers encadenados ("+ruta+")",
			"Conectad el sensor al mismo aparato que da la red a este equipo, o repetid la prueba con el equipo conectado por cable donde irá el sensor.")
		return
	}
	in.add("Routers intermedios", nivelOK, "ruta directa ("+ruta+")", "")
}
