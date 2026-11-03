# detecta-rede

Programa para comprobar se unha rede doméstica é válida para instalar un sensor RVGD.

A persoa que vai acoller o sensor execútao no seu ordenador (Windows, macOS ou Linux),
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

As mensaxes que ve o usuario están en [`textos.json`](textos.json) e pódense cambiar
sen tocar o código (ver [Textos](#textos)). Agora están en castelán, como o documento de
requisitos de conexión que se entrega ás persoas acolledoras. O código, os comentarios e
esta documentación están en galego.

## Que ve a persoa acolledora

- Unha consola cos 7 pasos e, para cada un, *Correcto*, *Revisar* ou *Problema*. O
  detalle técnico só vai ao informe (ou á consola con `-detalle`).
- Se está conectada por wifi, unha xanela Si/Non para confirmar que é a wifi principal.
- Unha xanela final co veredicto (*LA RED ES VÁLIDA*, *VÁLIDA (CON AVISOS)* ou *NO ES
  VÁLIDA*) e que hai que facer, en linguaxe sinxela.
- Ao aceptar, ábrese o Explorador (Windows) ou o Finder (macOS) co informe seleccionado.

O informe gárdase ao carón do executable; se alí non se pode escribir, no Escritorio
ou no cartafol do usuario.

## Compilación

Precísase Go 1.22 ou posterior. Pódese compilar para Windows e Linux desde calquera
sistema, sen ferramentas adicionais. A versión universal de macOS (Intel e Apple
Silicon) precisa `lipo` e `codesign`, que só están en macOS.

1. Copia a configuración WireGuard do cliente de probas en **`conf/test.conf`**.
   Incrústase dentro do executable e está no `.gitignore`: **non se debe subir nunca
   ao repositorio**.
2. Compila:

```bash
go mod tidy
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/detecta-rede.exe .
GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/detecta-rede-linux .
```

Versión de macOS (desde un Mac):

```bash
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o dist/detecta-rede-arm64 .
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/detecta-rede-amd64 .
lipo -create -output dist/detecta-rede dist/detecta-rede-arm64 dist/detecta-rede-amd64
codesign -s - -f dist/detecta-rede
ditto -c -k dist/detecta-rede dist/detecta-rede-macos.zip
```

Se se compila sen `conf/test.conf`, o programa funciona pero a proba do túnel sae
como erro.

## Distribución

A persoa acolledora recibe **un só ficheiro** e non ten que instalar nada:

- **Windows 10 ou posterior:** `detecta-rede.exe`.
- **macOS 12 ou posterior (Intel e Apple Silicon):** `detecta-rede-macos.zip`. Envíase
  comprimido para conservar o permiso de execución.

Instrucións para a persoa acolledora en Windows (en castelán):

> 1. Conectad el ordenador a la wifi principal del router donde irá el sensor (o por cable).
> 2. Haced doble clic en `detecta-rede.exe`. Si Windows avisa de que es una aplicación desconocida, pulsad «Más información» → «Ejecutar de todas formas». Si pregunta por el firewall, podéis cancelar.
> 3. Esperad unos 3 minutos. Aparecerá una ventana que dice si la red es válida o no.
> 4. Al pulsar Aceptar se abrirá la carpeta con el informe: enviadnos el fichero `informe-red-…txt`.

Instrucións para a persoa acolledora en macOS (en castelán):

> 1. Conectad el Mac a la wifi principal del router donde irá el sensor (o por cable).
> 2. Abrid `detecta-rede-macos.zip` (se descomprime solo) y haced doble clic en `detecta-rede`.
> 3. La primera vez, macOS dirá que no puede verificar el programa: pulsad «OK» (o «Hecho»). Id a **Ajustes del Sistema → Privacidad y seguridad**, bajad hasta abajo y pulsad **«Abrir igualmente»**. Volved a hacer doble clic en `detecta-rede` y confirmad «Abrir igualmente».
> 4. Se abrirá una ventana de Terminal con la prueba. Esperad unos 3 minutos. Si macOS pregunta si el Terminal puede acceder a la carpeta Descargas, pulsad «Permitir».
> 5. Aparecerá una ventana que dice si la red es válida o no. Al pulsar Aceptar se abrirá la carpeta con el informe: enviadnos el fichero `informe-red-…txt`.

O executable de macOS só ten unha sinatura *ad hoc*: sen unha conta de Apple
Developer (sinatura *Developer ID* e notarización), macOS bloquéao a primeira vez e
hai que permitilo a man (paso 3).

## Textos

Todos os textos que ve o usuario (consola, xanelas e informe) están en
[`textos.json`](textos.json), que se incrusta no executable ao compilar. Cada texto ten:

- `id`: identificador que usa o código. Non se cambia.
- `texto`: o texto que se amosa.
- `contexto`: onde aparece, para quen o edite (en castelán, como a folla de cálculo).
- `comodines`: os comodíns (`%s`, `%d`…) que debe levar o texto, na mesma orde. O
  programa substitúeos polos datos. `%%` escribe o símbolo `%`.

Para que outras persoas os editen nunha folla de cálculo:

```bash
go run ./ferramentas/textos exporta textos.json dist/textos-detecta-rede.xlsx
# ...enchen a columna amarela «Texto nuevo» e devolven o ficheiro...
go run ./ferramentas/textos importa textos-detecta-rede.xlsx textos.json
go test ./...
```

`importa` só cambia os textos que teñen algo en «Texto nuevo» e, se algún ten os comodíns
mal, non cambia nada e di cal é. `go test` comproba que non falte nin sobre ningún texto e
que os comodíns sexan os correctos; hai que executalo antes de compilar.

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
| `dialogo_windows.go`, `dialogo_macos.go`, `dialogo_outros.go` | Preguntas e resultado: xanelas nativas en Windows e macOS, consola en Linux |
| `consola_windows.go` | UTF-8 e título da consola en Windows |
| `textos.json`, `textos.go` | Textos que ve o usuario e funcións `t()` / `tf()` para lelos |
| `textos_test.go` | Comprobación dos textos (`go test`) |
| `ferramentas/textos/` | Exportación e importación dos textos a unha folla de cálculo |

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
