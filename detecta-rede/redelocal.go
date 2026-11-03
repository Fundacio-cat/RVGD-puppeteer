package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// infoRede describe a conexión do ordenador pola que sae o tráfico cara a Internet.
type infoRede struct {
	Interface   string `json:"interfaz"`
	Descricion  string `json:"descripcion,omitempty"`
	IPLocal     string `json:"ip_local"`
	Mascara     int    `json:"mascara"`
	PortaEnlace string `json:"puerta_enlace,omitempty"`
	WiFi        bool   `json:"wifi"`
	SSID        string `json:"ssid,omitempty"`
	DHCP        string `json:"dhcp"` // "si", "no" ou "desconocido"
	VPN         bool   `json:"vpn"`
}

func comprobaRedeLocal(in *informe) *infoRede {
	in.titulo("rede_local")

	ip, err := ipLocal()
	if err != nil {
		in.add(t("rede.proba_conexion"), nivelErro, t("rede.sen_rede_detalle"), t("rede.sen_rede_accion"))
		return nil
	}

	rede := &infoRede{IPLocal: ip.String(), DHCP: "desconocido"}
	if nome, bits, ok := interfaceDeIP(ip); ok {
		rede.Interface = nome
		rede.Mascara = bits
	}
	switch runtime.GOOS {
	case "windows":
		detallesWindows(rede)
	case "linux":
		detallesLinux(rede)
	case "darwin":
		detallesDarwin(rede)
	}
	rede.VPN = pareceVPN(rede.Interface, rede.Descricion)
	in.Rede = rede

	tipo := t("rede.tipo_cable")
	if rede.WiFi {
		tipo = t("rede.tipo_wifi")
		if rede.SSID != "" {
			tipo = tf("rede.tipo_wifi_ssid", rede.SSID)
		}
	}
	nome := rede.Interface
	if rede.Descricion != "" {
		nome += " (" + rede.Descricion + ")"
	}
	in.add(t("rede.proba_tipo"), nivelInfo, tf("rede.tipo_detalle", tipo, nome), "")

	if rede.VPN {
		in.add(t("rede.proba_vpn"), nivelErro, tf("rede.vpn_detalle", nome), t("rede.vpn_accion"))
	}

	if ip.IsPrivate() {
		detalle := ip.String()
		if rede.Mascara > 0 {
			detalle = tf("rede.ip_detalle", ip.String(), rede.Mascara, (1<<(32-rede.Mascara))-2)
		}
		in.add(t("rede.proba_ip"), nivelOK, detalle, "")
	} else {
		in.add(t("rede.proba_ip"), nivelAviso, tf("rede.ip_publica", ip.String()), "")
	}

	if rede.PortaEnlace != "" {
		in.add(t("rede.proba_router"), nivelInfo, tf("rede.router_detalle", rede.PortaEnlace), "")
	}

	switch rede.DHCP {
	case "si":
		in.add(t("rede.proba_dhcp"), nivelOK, t("rede.dhcp_ok"), "")
	case "no":
		in.add(t("rede.proba_dhcp"), nivelAviso, t("rede.dhcp_manual_detalle"), t("rede.dhcp_manual_accion"))
	default:
		in.add(t("rede.proba_dhcp"), nivelInfo, t("rede.dhcp_sen_datos"), "")
	}
	return rede
}

// ipLocal devolve a IP local pola que o ordenador sae a Internet. Non envía ningún paquete.
func ipLocal() (netip.Addr, error) {
	c, err := net.Dial("udp4", "8.8.8.8:53")
	if err != nil {
		return netip.Addr{}, err
	}
	defer c.Close()
	addr, ok := netip.AddrFromSlice(c.LocalAddr().(*net.UDPAddr).IP.To4())
	if !ok {
		return netip.Addr{}, fmt.Errorf("dirección local no válida")
	}
	return addr, nil
}

func interfaceDeIP(ip netip.Addr) (string, int, bool) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", 0, false
	}
	for _, ifc := range ifaces {
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if candidata, ok := netip.AddrFromSlice(ipnet.IP.To4()); ok && candidata == ip {
				bits, _ := ipnet.Mask.Size()
				return ifc.Name, bits, true
			}
		}
	}
	return "", 0, false
}

func executa(timeout time.Duration, nome string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, nome, args...).Output()
	return string(out), err
}

// detallesWindows usa PowerShell porque os valores (Enabled, Native 802.11…) non
// dependen do idioma do sistema, a diferenza de ipconfig ou netsh.
func detallesWindows(rede *infoRede) {
	script := fmt.Sprintf(`[Console]::OutputEncoding=[Text.Encoding]::UTF8
$ErrorActionPreference='SilentlyContinue'
$ix=(Get-NetIPAddress -IPAddress '%s').InterfaceIndex
$r=Get-NetRoute -InterfaceIndex $ix -DestinationPrefix '0.0.0.0/0' | Sort-Object RouteMetric | Select-Object -First 1
$a=Get-NetAdapter -InterfaceIndex $ix
$i=Get-NetIPInterface -InterfaceIndex $ix -AddressFamily IPv4
$p=Get-NetConnectionProfile -InterfaceIndex $ix
[pscustomobject]@{Alias=[string]$a.Name;Desc=[string]$a.InterfaceDescription;Media=[string]$a.PhysicalMediaType;Gateway=[string]$r.NextHop;Dhcp=[string]$i.Dhcp;Profile=[string]$p.Name} | ConvertTo-Json -Compress`, rede.IPLocal)

	out, err := executa(20*time.Second, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	if err != nil {
		return
	}
	var d struct{ Alias, Desc, Media, Gateway, Dhcp, Profile string }
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &d) != nil {
		return
	}
	if d.Alias != "" {
		rede.Interface = d.Alias
	}
	rede.Descricion = d.Desc
	rede.PortaEnlace = d.Gateway
	desc := strings.ToLower(d.Desc)
	rede.WiFi = strings.Contains(d.Media, "802.11") || strings.Contains(desc, "wi-fi") ||
		strings.Contains(desc, "wireless") || strings.Contains(desc, "wlan")
	if rede.WiFi {
		rede.SSID = d.Profile
	}
	switch d.Dhcp {
	case "Enabled":
		rede.DHCP = "si"
	case "Disabled":
		rede.DHCP = "no"
	}
}

func detallesLinux(rede *infoRede) {
	if rede.Interface == "" {
		return
	}
	if data, err := os.ReadFile("/proc/net/route"); err == nil {
		for _, lina := range strings.Split(string(data), "\n")[1:] {
			campos := strings.Fields(lina)
			if len(campos) < 3 || campos[0] != rede.Interface || campos[1] != "00000000" {
				continue
			}
			if b, err := hex.DecodeString(campos[2]); err == nil && len(b) == 4 {
				rede.PortaEnlace = netip.AddrFrom4([4]byte{b[3], b[2], b[1], b[0]}).String()
			}
			break
		}
	}

	for _, p := range []string{"wireless", "phy80211"} {
		if _, err := os.Stat("/sys/class/net/" + rede.Interface + "/" + p); err == nil {
			rede.WiFi = true
		}
	}
	if rede.WiFi {
		if out, err := executa(5*time.Second, "iwgetid", "-r"); err == nil {
			rede.SSID = strings.TrimSpace(out)
		} else if out, err := executa(5*time.Second, "nmcli", "-t", "-g", "GENERAL.CONNECTION", "device", "show", rede.Interface); err == nil {
			rede.SSID = strings.TrimSpace(out)
		}
	}

	// Os enderezos obtidos por DHCP márcanse como "dynamic". Se non aparece, non
	// quere dicir necesariamente que sexa manual (algúns clientes DHCP non o marcan).
	if out, err := executa(5*time.Second, "ip", "-4", "addr", "show", "dev", rede.Interface); err == nil && strings.Contains(out, " dynamic") {
		rede.DHCP = "si"
	}
}

// detallesDarwin obtén os detalles da conexión en macOS.
func detallesDarwin(rede *infoRede) {
	if out, err := executa(5*time.Second, "route", "-n", "get", "default"); err == nil {
		for _, lina := range strings.Split(out, "\n") {
			if k, v, ok := strings.Cut(strings.TrimSpace(lina), ":"); ok && k == "gateway" {
				rede.PortaEnlace = strings.TrimSpace(v)
			}
		}
	}
	if rede.Interface == "" {
		return
	}
	if out, err := executa(5*time.Second, "ipconfig", "getpacket", rede.Interface); err == nil && strings.TrimSpace(out) != "" {
		rede.DHCP = "si"
	}
	// Desde macOS 14.4, networksetup di que non hai wifi aínda que a haxa, e o SSID
	// aparece como "<redacted>" sen permiso de localización. ipconfig getsummary
	// segue indicando o tipo de interface; o SSID só se garda se é visible.
	if out, err := executa(5*time.Second, "ipconfig", "getsummary", rede.Interface); err == nil {
		for _, lina := range strings.Split(out, "\n") {
			k, v, ok := strings.Cut(strings.TrimSpace(lina), " : ")
			if !ok {
				continue
			}
			v = strings.TrimSpace(v)
			switch k {
			case "InterfaceType":
				rede.WiFi = strings.EqualFold(v, "WiFi")
			case "SSID":
				if v != "" && !strings.Contains(v, "redacted") {
					rede.SSID = v
				}
			}
		}
	}
}

func pareceVPN(nome, descripcion string) bool {
	n := strings.ToLower(nome)
	for _, prefixo := range []string{"tun", "tap", "wg", "ppp", "tailscale", "zt", "utun", "ipsec"} {
		if strings.HasPrefix(n, prefixo) {
			return true
		}
	}
	texto := n + " " + strings.ToLower(descripcion)
	for _, marca := range []string{"vpn", "wireguard", "openvpn", "anyconnect", "fortinet", "forticlient",
		"globalprotect", "pangp", "zscaler", "nordlynx", "tailscale", "zerotier", "wintun", "tap-windows", "sonicwall", "juniper"} {
		if strings.Contains(texto, marca) {
			return true
		}
	}
	return false
}
