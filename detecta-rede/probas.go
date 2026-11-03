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
	in.titulo("internet")

	resp, err := clienteHTTP(10 * time.Second).Get("http://connectivitycheck.gstatic.com/generate_204")
	if err != nil {
		in.add(t("internet.proba"), nivelErro, tf("internet.sen_internet_detalle", err), t("internet.sen_internet_accion"))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		in.add(t("internet.proba"), nivelOK, t("internet.ok"), "")
		return
	}
	detalle := tf("internet.portal_detalle", resp.StatusCode)
	if loc := resp.Header.Get("Location"); loc != "" {
		detalle = tf("internet.portal_detalle_redirixe", resp.StatusCode, loc)
	}
	in.add(t("internet.proba"), nivelErro, detalle, t("internet.portal_accion"))
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
	in.titulo("dns")

	ipsServidor, err := resolve4(net.DefaultResolver, host)
	if err != nil {
		in.add(t("dns.proba_servidor"), nivelErro, tf("dns.servidor_erro", host), t("dns.revisar_filtros"))
	} else {
		in.add(t("dns.proba_servidor"), nivelOK, fmt.Sprintf("%s → %s", host, textoIPs(ipsServidor)), "")
	}

	if ips, err := resolve4(net.DefaultResolver, "www.google.com"); err != nil {
		in.add(t("dns.proba_google"), nivelErro, t("dns.google_erro"), t("dns.revisar_filtros"))
	} else if ipsBloqueadas(ips) {
		in.add(t("dns.proba_google"), nivelErro, tf("dns.google_bloqueado_detalle", textoIPs(ips)), t("dns.google_bloqueado_accion"))
	} else {
		in.add(t("dns.proba_google"), nivelOK, t("dns.google_ok"), "")
	}

	inexistente := fmt.Sprintf("rvgd-%08x.example.com", rand.Uint32())
	if ips, err := resolve4(net.DefaultResolver, inexistente); err == nil && len(ips) > 0 {
		in.add(t("dns.proba_inexistentes"), nivelAviso, tf("dns.inexistentes_detalle", textoIPs(ips)), t("dns.inexistentes_accion"))
	} else {
		in.add(t("dns.proba_inexistentes"), nivelOK, t("dns.inexistentes_ok"), "")
	}

	// O sensor usa o DNS que lle dá o router, pero o documento de requisitos
	// pide que tamén se poidan facer consultas DNS externas.
	for _, proto := range []string{"udp", "tcp"} {
		proba := tf("dns.proba_externo", strings.ToUpper(proto))
		ips, err := resolve4(resolverPara("8.8.8.8:53", proto), host)
		switch {
		case err != nil:
			in.add(proba, nivelAviso, tf("dns.externo_erro_detalle", strings.ToUpper(proto)), t("dns.externo_erro_accion"))
		case proto == "udp" && len(ipsServidor) > 0 && !comparten(ips, ipsServidor):
			in.add(proba, nivelAviso, tf("dns.externo_distinto_detalle", host, textoIPs(ipsServidor), textoIPs(ips)), t("dns.externo_distinto_accion"))
		default:
			in.add(proba, nivelOK, t("dns.externo_ok"), "")
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
		return 0, errors.New(t("ntp.resposta_incompleta"))
	}

	const epocaNTP = 2208988800 // segundos entre 1900 e 1970
	seg := int64(binary.BigEndian.Uint32(resp[40:44])) - epocaNTP
	frac := int64((uint64(binary.BigEndian.Uint32(resp[44:48])) * 1e9) >> 32)
	horaServidor := time.Unix(seg, frac)
	return horaServidor.Sub(t1.Add(t4.Sub(t1) / 2)), nil
}

func comprobaNTP(in *informe) {
	in.titulo("ntp")

	var ultimo error
	for _, s := range []string{"pool.ntp.org", "debian.pool.ntp.org", "time.google.com"} {
		desfase, err := sntp(s)
		if err != nil {
			ultimo = err
			continue
		}
		in.add(t("ntp.proba"), nivelOK, tf("ntp.ok", s, desfase.Round(time.Millisecond)), "")
		return
	}
	in.add(t("ntp.proba"), nivelErro, tf("ntp.erro_detalle", ultimo), t("ntp.erro_accion"))
}

// --- Webs necesarias ---

type web struct {
	url     string
	critica bool
	uso     string // id do texto que explica para que se usa
}

var webs = []web{
	{"https://www.google.com/", true, "webs.uso_procuras"},
	{"https://github.com/", true, "webs.uso_actualizacions_sensor"},
	{"https://raw.githubusercontent.com/", false, "webs.uso_instalacion"},
	{"https://nodejs.org/", false, "webs.uso_instalacion"},
	{"https://archive.raspberrypi.com/", false, "webs.uso_actualizacions_sistema"},
	{"https://deb.debian.org/", false, "webs.uso_actualizacions_sistema"},
	{"https://repo.zabbix.com/", false, "webs.uso_monitorizacion"},
	{"https://deb.nodesource.com/", false, "webs.uso_instalacion"},
	{"http://deb.debian.org/", false, "webs.uso_actualizacions_http"},
	{"http://archive.raspberrypi.com/", false, "webs.uso_actualizacions_http"},
}

func comprobaWebs(in *informe) {
	in.titulo("webs")

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
			in.add(proba, nivelOK, tf("webs.ok", t(w.uso)), "")
			continue
		}
		n := nivelAviso
		if w.critica {
			n = nivelErro
		}
		var errCert *tls.CertificateVerificationError
		if errors.As(errs[i], &errCert) {
			in.add(proba, n, t("webs.certificado_detalle"), t("webs.certificado_accion"))
			continue
		}
		porto := "443"
		if strings.HasPrefix(w.url, "http://") {
			porto = "80"
		}
		in.add(proba, n, tf("webs.erro_detalle", errs[i]), tf("webs.erro_accion", porto, t(w.uso)))
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
		return nil, errors.New(tf("ruta.sen_ferramenta", nome))
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
	in.titulo("ruta")

	saltos, err := traceroute()
	if err != nil || len(saltos) == 0 {
		motivo := t("ruta.sen_resposta")
		if err != nil {
			motivo = err.Error()
		}
		in.add(t("ruta.proba"), nivelInfo, tf("ruta.non_comprobada", motivo), "")
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
			in.add(t("ruta.proba_cgnat"), nivelInfo, t("ruta.cgnat_detalle"), "")
			break
		}
	}

	// Dous routers domésticos encadeados (p. ex. un router ou mesh propio detrás do
	// do operador): o sensor podería acabar conectado a un aparello distinto do do ordenador.
	domestica := netip.MustParsePrefix("192.168.0.0/16")
	s1, ok1 := saltos[1]
	s2, ok2 := saltos[2]
	if ok1 && ok2 && s1.IsPrivate() && domestica.Contains(s2) {
		in.add(t("ruta.proba"), nivelAviso, tf("ruta.dobre_detalle", ruta), t("ruta.dobre_accion"))
		return
	}
	in.add(t("ruta.proba"), nivelOK, tf("ruta.ok", ruta), "")
}
