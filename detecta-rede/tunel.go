package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

// configWG contén os campos de test.conf necesarios para levantar o túnel.
type configWG struct {
	PrivateKey   string
	PublicKey    string
	PresharedKey string
	Endpoint     string
	Enderezos    []netip.Prefix
	AllowedIPs   []netip.Prefix
	MTU          int
	Keepalive    int
}

func cargaConfigWG(ruta string) (*configWG, error) {
	data, err := os.ReadFile(ruta)
	if err != nil {
		return nil, err
	}
	return parseConfigWG(data)
}

func parseConfigWG(data []byte) (*configWG, error) {
	cfg := &configWG{}
	seccion := ""
	for _, lina := range strings.Split(string(data), "\n") {
		lina = strings.TrimSpace(lina)
		if i := strings.IndexAny(lina, "#;"); i >= 0 {
			lina = strings.TrimSpace(lina[:i])
		}
		if lina == "" {
			continue
		}
		if strings.HasPrefix(lina, "[") {
			seccion = strings.ToLower(strings.Trim(lina, "[] "))
			continue
		}
		chave, valor, ok := strings.Cut(lina, "=")
		if !ok {
			continue
		}
		chave = strings.ToLower(strings.TrimSpace(chave))
		valor = strings.TrimSpace(valor)

		switch seccion + "." + chave {
		case "interface.privatekey":
			cfg.PrivateKey = valor
		case "interface.address":
			cfg.Enderezos = append(cfg.Enderezos, prefixos(valor)...)
		case "interface.mtu":
			cfg.MTU, _ = strconv.Atoi(valor)
		case "peer.publickey":
			cfg.PublicKey = valor
		case "peer.presharedkey":
			cfg.PresharedKey = valor
		case "peer.endpoint":
			cfg.Endpoint = valor
		case "peer.allowedips":
			cfg.AllowedIPs = append(cfg.AllowedIPs, prefixos(valor)...)
		case "peer.persistentkeepalive":
			cfg.Keepalive, _ = strconv.Atoi(valor)
		}
	}

	if cfg.PrivateKey == "" || cfg.PublicKey == "" || cfg.Endpoint == "" || len(cfg.Enderezos) == 0 {
		return nil, fmt.Errorf("faltan PrivateKey, Address, PublicKey o Endpoint")
	}
	if cfg.MTU == 0 {
		cfg.MTU = 1420
	}
	if cfg.Keepalive == 0 {
		cfg.Keepalive = 25
	}
	if len(cfg.AllowedIPs) == 0 {
		cfg.AllowedIPs = []netip.Prefix{netip.MustParsePrefix("10.66.66.0/24")}
	}
	return cfg, nil
}

func prefixos(valor string) []netip.Prefix {
	var res []netip.Prefix
	for _, p := range strings.Split(valor, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.Contains(p, "/") {
			p += "/32"
		}
		if pref, err := netip.ParsePrefix(p); err == nil {
			res = append(res, pref)
		}
	}
	return res
}

func separaEndpoint(endpoint string) (string, uint16, error) {
	host, porto, err := net.SplitHostPort(endpoint)
	if err != nil {
		return "", 0, err
	}
	p, err := strconv.ParseUint(porto, 10, 16)
	if err != nil {
		return "", 0, err
	}
	return host, uint16(p), nil
}

func chaveHex(b64 string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(b) != 32 {
		return "", fmt.Errorf("clave no válida")
	}
	return hex.EncodeToString(b), nil
}

// arrincaTunel levanta WireGuard en modo userspace (netstack): non hai que instalar
// nada nin ter permisos de administrador, e o túnel só o usa este programa.
func arrincaTunel(cfg *configWG, endpoint netip.AddrPort) (*device.Device, *netstack.Net, error) {
	var local netip.Addr
	for _, d := range cfg.Enderezos {
		if d.Addr().Is4() {
			local = d.Addr()
			break
		}
	}
	if !local.IsValid() {
		return nil, nil, fmt.Errorf("el fichero no tiene ninguna dirección IPv4 en Address")
	}

	priv, err := chaveHex(cfg.PrivateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("PrivateKey: %w", err)
	}
	pub, err := chaveHex(cfg.PublicKey)
	if err != nil {
		return nil, nil, fmt.Errorf("PublicKey: %w", err)
	}

	var ipc strings.Builder
	fmt.Fprintf(&ipc, "private_key=%s\n", priv)
	fmt.Fprintf(&ipc, "public_key=%s\n", pub)
	if cfg.PresharedKey != "" {
		psk, err := chaveHex(cfg.PresharedKey)
		if err != nil {
			return nil, nil, fmt.Errorf("PresharedKey: %w", err)
		}
		fmt.Fprintf(&ipc, "preshared_key=%s\n", psk)
	}
	fmt.Fprintf(&ipc, "endpoint=%s\n", endpoint)
	fmt.Fprintf(&ipc, "persistent_keepalive_interval=%d\n", cfg.Keepalive)
	for _, p := range cfg.AllowedIPs {
		fmt.Fprintf(&ipc, "allowed_ip=%s\n", p)
	}

	tunDev, tnet, err := netstack.CreateNetTUN([]netip.Addr{local}, nil, cfg.MTU)
	if err != nil {
		return nil, nil, err
	}
	dev := device.NewDevice(tunDev, conn.NewDefaultBind(), device.NewLogger(device.LogLevelSilent, ""))
	if err := dev.IpcSet(ipc.String()); err != nil {
		dev.Close()
		return nil, nil, err
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, nil, err
	}
	return dev, tnet, nil
}

func handshakeFeito(dev *device.Device) bool {
	estado, err := dev.IpcGet()
	if err != nil {
		return false
	}
	for _, lina := range strings.Split(estado, "\n") {
		if v, ok := strings.CutPrefix(lina, "last_handshake_time_sec="); ok && v != "0" {
			return true
		}
	}
	return false
}

// pingTunel envía un ping ICMP polo túnel con `tamano` bytes de datos.
func pingTunel(tnet *netstack.Net, destino netip.Addr, tamano int, timeout time.Duration) (time.Duration, error) {
	sock, err := tnet.Dial("ping4", destino.String())
	if err != nil {
		return 0, err
	}
	defer sock.Close()

	datos := make([]byte, tamano)
	for i := range datos {
		datos[i] = byte(i)
	}
	eco := &icmp.Echo{ID: rand.Intn(1 << 16), Seq: rand.Intn(1 << 16), Data: datos}
	paquete, err := (&icmp.Message{Type: ipv4.ICMPTypeEcho, Body: eco}).Marshal(nil)
	if err != nil {
		return 0, err
	}

	sock.SetReadDeadline(time.Now().Add(timeout))
	inicio := time.Now()
	if _, err := sock.Write(paquete); err != nil {
		return 0, err
	}
	buf := make([]byte, tamano+128)
	for {
		n, err := sock.Read(buf)
		if err != nil {
			return 0, err
		}
		resp, err := icmp.ParseMessage(1, buf[:n])
		if err != nil {
			continue
		}
		if r, ok := resp.Body.(*icmp.Echo); ok && r.Seq == eco.Seq && bytes.Equal(r.Data, datos) {
			return time.Since(inicio), nil
		}
	}
}

// sondaPostgres comproba que hai un Postgres escoitando, sen credenciais:
// envía un SSLRequest e Postgres sempre responde cun só byte, 'S' ou 'N'.
func sondaPostgres(tnet *netstack.Net, enderezo string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := tnet.DialContext(ctx, "tcp", enderezo)
	if err != nil {
		return false, err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte{0, 0, 0, 8, 0x04, 0xd2, 0x16, 0x2f}); err != nil {
		return false, err
	}
	resp := make([]byte, 1)
	if _, err := io.ReadFull(c, resp); err != nil {
		return false, err
	}
	return resp[0] == 'S' || resp[0] == 'N', nil
}

func comprobaTunel(in *informe, cfg *configWG, errConf error, orixe, servidor, bd string, duracion time.Duration, wifi bool) {
	in.titulo("tunel")

	if errConf != nil {
		in.add(t("tunel.proba_configuracion"), nivelErro, tf("tunel.configuracion_erro_detalle", orixe, errConf), t("tunel.configuracion_erro_accion"))
		return
	}

	host, porto, err := separaEndpoint(cfg.Endpoint)
	if err != nil {
		in.add(t("tunel.proba_configuracion"), nivelErro, tf("tunel.endpoint_non_valido", cfg.Endpoint), "")
		return
	}
	ips, err := resolve4(net.DefaultResolver, host)
	if err != nil || len(ips) == 0 {
		in.add(t("tunel.proba_conexion"), nivelErro, tf("tunel.non_resolve", host), t("dns.revisar_filtros"))
		return
	}
	srv, err := netip.ParseAddr(servidor)
	if err != nil {
		in.add(t("tunel.proba_configuracion"), nivelErro, tf("tunel.ip_non_valida", servidor), "")
		return
	}

	dev, tnet, err := arrincaTunel(cfg, netip.AddrPortFrom(ips[0], porto))
	if err != nil {
		in.add(t("tunel.proba_conexion"), nivelErro, tf("tunel.non_arrinca", err), "")
		return
	}
	defer dev.Close()

	// Os pings provocan o handshake; WireGuard reinténtao cada 5 s.
	limite := time.Now().Add(20 * time.Second)
	for !handshakeFeito(dev) && time.Now().Before(limite) {
		pingTunel(tnet, srv, 8, 2*time.Second)
	}
	if !handshakeFeito(dev) {
		in.add(t("tunel.proba_conexion"), nivelErro, tf("tunel.sen_resposta_detalle", porto), tf("tunel.sen_resposta_accion", porto))
		return
	}
	in.add(t("tunel.proba_conexion"), nivelOK, tf("tunel.ok", host, porto), "")

	latencia, err := pingTunel(tnet, srv, 56, 3*time.Second)
	if err != nil {
		in.add(t("tunel.proba_resposta"), nivelAviso, tf("tunel.ping_erro_detalle", srv, err), t("tunel.revisar_servidor"))
	} else {
		in.add(t("tunel.proba_resposta"), nivelOK, tf("tunel.ping_ok", srv, latencia.Round(time.Millisecond)), "")
	}

	comprobaBD(in, tnet, srv, bd)

	if err == nil {
		comprobaMTU(in, tnet, srv, cfg.MTU)
		comprobaEstabilidade(in, tnet, srv, duracion, wifi)
	}
}

// portoBD é o porto de Postgres no servidor, dentro do túnel.
const portoBD = 51432

func comprobaBD(in *informe, tnet *netstack.Net, srv netip.Addr, bd string) {
	if bd == "" {
		bd = netip.AddrPortFrom(srv, portoBD).String()
	}
	if ePostgres, err := sondaPostgres(tnet, bd); err == nil && ePostgres {
		in.add(t("tunel.proba_bd"), nivelOK, tf("tunel.bd_ok", bd), "")
		return
	}
	in.add(t("tunel.proba_bd"), nivelAviso, tf("tunel.bd_erro_detalle", bd), t("tunel.bd_erro_accion"))
}

func comprobaMTU(in *informe, tnet *netstack.Net, srv netip.Addr, mtu int) {
	tamano := mtu - 28 // cabeceiras IPv4 (20) + ICMP (8)
	for intento := 0; intento < 2; intento++ {
		if _, err := pingTunel(tnet, srv, tamano, 3*time.Second); err == nil {
			in.add(t("tunel.proba_mtu"), nivelOK, tf("tunel.mtu_ok", mtu), "")
			return
		}
	}
	in.add(t("tunel.proba_mtu"), nivelAviso, tf("tunel.mtu_erro_detalle", mtu), t("tunel.mtu_erro_accion"))
}

func comprobaEstabilidade(in *informe, tnet *netstack.Net, srv netip.Addr, duracion time.Duration, wifi bool) {
	if duracion <= 0 {
		return
	}
	fmt.Fprintf(in.out, "  %s\n", tf("tunel.medindo_estabilidade", duracion))

	var enviados, perdidos int
	var total, maxima time.Duration
	fin := time.Now().Add(duracion)
	for time.Now().Before(fin) {
		inicio := time.Now()
		enviados++
		if lat, err := pingTunel(tnet, srv, 56, 2*time.Second); err != nil {
			perdidos++
		} else {
			total += lat
			if lat > maxima {
				maxima = lat
			}
		}
		if espera := time.Second - time.Since(inicio); espera > 0 {
			time.Sleep(espera)
		}
	}

	recibidos := enviados - perdidos
	perdida := 100 * float64(perdidos) / float64(enviados)
	detalle := tf("tunel.estabilidade_detalle", perdidos, enviados, perdida)
	if recibidos > 0 {
		detalle += tf("tunel.estabilidade_latencia",
			(total / time.Duration(recibidos)).Round(time.Millisecond), maxima.Round(time.Millisecond))
	}

	// A wifi engade perdas propias; o sensor irá por cable.
	limiar := 2.0
	if wifi {
		limiar = 5.0
	}
	accion := ""
	if wifi {
		accion = t("tunel.estabilidade_wifi")
	}
	switch {
	case perdida > 20:
		in.add(t("tunel.proba_estabilidade"), nivelErro, detalle, strings.TrimSpace(t("tunel.estabilidade_inestable")+" "+accion))
	case perdida > limiar:
		in.add(t("tunel.proba_estabilidade"), nivelAviso, detalle, accion)
	default:
		in.add(t("tunel.proba_estabilidade"), nivelOK, detalle, "")
	}
}
