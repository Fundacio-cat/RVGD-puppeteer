# detecta-rede

Programa para comprobar se unha rede doméstica é válida para instalar un sensor RVGD.

A persoa que vai acoller o sensor execútao no seu ordenador (Windows ou Linux),
conectado á mesma rede onde irá o sensor. En tres minutos, unha xanela indica se a
rede é válida ou non e que hai que facer se hai algún problema.

Todo vai dentro dun único executable, incluída a configuración do túnel. Non hai que
instalar nada nin ter permisos de administrador: o programa levanta un **túnel
WireGuard real** co servidor en modo *userspace* ([wireguard-go] con netstack),
igual que fará o sensor.

[wireguard-go]: https://git.zx2c4.com/wireguard-go

## Que comproba

| Sección | Probas |
|---|---|
| Ordenador e rede local | Cable ou wifi (e SSID), VPN no ordenador, IP local, DHCP |
| Acceso a Internet | Portal cativo (`generate_204`) |
| DNS | Resolución do servidor e de Google, filtros, redirección de dominios inexistentes, DNS externo UDP/TCP 53 |
| Hora | Resposta NTP (UDP 123) |
| Webs necesarias | Google, GitHub, nodejs.org, repositorios (HTTPS e HTTP), certificados interceptados |
| Ruta de rede | Dobre router, CGNAT |
| Túnel | Handshake (porto UDP do `Endpoint` de `test.conf`), ping ao servidor, Postgres (sen credenciais), MTU, estabilidade |

Resultado: **APTA**, **APTA CON AVISOS** ou **NO APTA**, e un informe `.txt` e `.json`.

As mensaxes que ve o usuario están en castelán, como o documento de requisitos de
conexión que se entrega ás persoas acolledoras. O código, os comentarios e esta
documentación están en galego.

## Que ve a persoa acolledora

- Unha consola cos 7 pasos e, para cada un, *Correcto*, *Revisar* ou *Problema*. O
  detalle técnico só vai ao informe (ou á consola con `-detalle`).
- Se está conectada por wifi, unha xanela Si/Non para confirmar que é a wifi principal.
- Unha xanela final co veredicto (*LA RED ES VÁLIDA*, *VÁLIDA (CON AVISOS)* ou *NO ES
  VÁLIDA*) e que hai que facer, en linguaxe sinxela.
- Ao aceptar, ábrese o Explorador co informe seleccionado.

O informe gárdase ao carón do executable; se alí non se pode escribir, no Escritorio
ou no cartafol do usuario.

## Compilación

Precísase Go 1.22 ou posterior. Pódese compilar para Windows desde calquera sistema
(macOS, Linux ou Windows), sen ferramentas adicionais.

1. Copia a configuración WireGuard do cliente de probas en **`conf/test.conf`**.
   Incrústase dentro do executable e está no `.gitignore`: **non se debe subir nunca
   ao repositorio**.
2. Compila:

```bash
go mod tidy
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/detecta-rede.exe .
GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/detecta-rede-linux .
```

Se se compila sen `conf/test.conf`, o programa funciona pero a proba do túnel sae
como erro.

## Distribución

A persoa acolledora recibe **un só ficheiro**: `detecta-rede.exe`. Non hai que
instalar nada. Funciona en Windows 10 ou posterior.

Instrucións para a persoa acolledora (en castelán):

> 1. Conectad el ordenador a la wifi principal del router donde irá el sensor (o por cable).
> 2. Haced doble clic en `detecta-rede.exe`. Si Windows avisa de que es una aplicación desconocida, pulsad «Más información» → «Ejecutar de todas formas». Si pregunta por el firewall, podéis cancelar.
> 3. Esperad unos 3 minutos. Aparecerá una ventana que dice si la red es válida o no.
> 4. Al pulsar Aceptar se abrirá la carpeta con el informe: enviadnos el fichero `informe-red-…txt`.

## Opcións

| Opción | Por defecto | |
|---|---|---|
| `-conf` | a incrustada | Ficheiro WireGuard alternativo (para probas) |
| `-servidor` | `10.66.66.1` | IP do servidor dentro do túnel |
| `-bd` | porto `51432` do servidor | `host:porto` de Postgres dentro do túnel |
| `-duracion` | `60s` | Proba de estabilidade (`0` para omitila) |
| `-no-interactivo` | | Sen xanelas nin preguntas |
| `-detalle` | | Amosa o detalle técnico na consola |

## Estrutura do código

| Ficheiro | Contido |
|---|---|
| `main.go` | Fluxo principal, informe e xanela final |
| `redelocal.go` | Conexión do ordenador: cable/wifi, SSID, VPN, DHCP (Windows, Linux e macOS) |
| `probas.go` | Portal cativo, DNS, NTP, webs e ruta de rede |
| `tunel.go` | Lectura de `test.conf` e túnel WireGuard: handshake, ping, Postgres, MTU e estabilidade |
| `dialogo_windows.go`, `dialogo_outros.go` | Preguntas e resultado: xanelas nativas en Windows, consola no resto |
| `consola_windows.go` | UTF-8 e título da consola en Windows |

## Configuración necesaria no servidor

A clave do cliente de probas vai dentro do executable, que se entrega a calquera
persoa acolledora, polo que hai que considerala pública. No servidor, o cliente de
probas debe estar limitado no tornalumes a:

- ping á IP do servidor dentro do túnel;
- TCP ao porto de Postgres (só para a proba de conexión; recoméndase ademais unha
  regra `reject` para ese cliente no `pg_hba.conf`).

Nada máis: nin cara aos sensores nin cara a SSH nin cara a Internet a través do
servidor. Se dúas persoas fan a proba á vez, comparten o cliente e poden ter erros
intermitentes.
