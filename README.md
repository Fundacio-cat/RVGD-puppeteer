# Rastreador de contidos de buscadores web

Este proxecto, contén un rastrexador escrito en Node.js que utiliza Puppeteer para automatizar procuras en Google, obter os principais resultados e gardalos nunha base de datos Postgres. Ademais, integra diferentes módulos para escoller navegadores, motores de busca e dimensións de xanela, todo adaptado ao contorno da Fundació puntCAT.

## Funcionalidade principal

- **Execución dunha procura**: emprega un navegador Chromium controlado con Puppeteer para procurar unha consulta concreta en Google, aceptando o banner de cookies se é preciso e simulando interaccións humanas (pausas, desprazamentos, etc.).
- **Extracción de resultados**: colle o título, o URL e a descrición (snippet) dos resultados orgánicos visibles, evitando duplicados e descartando módulos específicos como "Máis preguntas".
- **Persistencia**: garda cada resultado nunha táboa `resultats` de Postgres, asociándoo ao sensor, navegador, motor de busca, identificador de procura e tamaño de xanela.
- **Orquestración**: selecciona a seguinte procura pendente para o sensor, obtén a configuración do navegador/motor/tamaño e espera o tempo necesario entre execucións.

## Arquitectura básica

- `src/crawler.js`: punto de entrada. Le a configuración (`config.json`, variables de contorno e argumentos de liña de ordes), crea o navegador, conéctase coa base de datos e coordina todo o fluxo (`executaProcuraGoogle`, insercións, etc.).
- `src/cercadors/google.js`: lóxica específica de Google (xestión do banner de cookies, execución da procura, extracción de resultados).
- `src/database.js`: utilidades para conectarse a Postgres, seleccionar tamaños/navegadores/motores e realizar insercións.
- `src/cercadors/@cercador.js`, `src/navegador.js`, etc.: abstraen a selección de compoñentes funcionais (motor activo, navegador, etc.).
- `config.json`: ficheiro de configuración por defecto (URL da base de datos, consultas, parámetros de procura, etc.).

## Requisitos

1. **Node.js**
2. **PostgreSQL** operativo e accesible mediante unha URL de conexión (`postgres://user:password@host:port/database`)
3. **Chromium/Puppeteer**: instálase automaticamente ao executar `npm install`, pero cómpre garantir que a máquina poida descargar o navegador (requírese conexión a Internet ou, se non, configurar Puppeteer para reutilizar un existente).
4. **Dependencias do sistema** (macOS/Linux): pode ser necesario ter instaladas bibliotecas gráficas e fontes para executar Chromium. En macOS adoitan vir preinstaladas.

## Instalación

1. Clona o repositorio.
```bash
git clone https://gitlab.com/pau_fundacio/gal_puppeteer
cd "puppeteer"
```

2. Instala as dependencias.
```bash
# Actualización de paquetes
sudo apt update

# Dependencias do sistema e de Chromium
sudo apt install -y chromium-browser chromium-codecs-ffmpeg fonts-liberation libatk-bridge2.0-0 libatk1.0-0 libatspi2.0-0 libcups2 libdrm2 libgbm1 libgtk-3-0 libnss3 libxcomposite1 libxdamage1 libxfixes3 libxrandr2 libxshmfence1

# Instala Node.js e npm desde o repositorio de NodeSource 
curl -sL https://deb.nodesource.com/setup_10.x | sudo bash -
sudo apt install nodejs
node --version

# Instala Node.js e npm usando NVM 
curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.35.3/install.sh | bash
nvm --version
nvm install node

# Dependencias de Node
npm install
```

3. Define a configuración:
- Copia `config.example.json` a `config.json` e cobre `database.url` coas credenciais que correspondan ao sensor. 

4. O rastrexador tenta detectar automaticamente o executable de Chromium. Se non o atopa, podes indicar a ruta manualmente:
- Ficheiro de configuración (`config.json`):
```json
{
  "browser": {
    "executablePath": "/usr/bin/chromium-browser",
    "headless": true
  }
}
```

5. Para evitar a descarga automática de Chromium en instalacións futuras:
```bash
PUPPETEER_SKIP_CHROMIUM_DOWNLOAD=true npm install
```

## Execución

### Desde a liña de ordes

```bash
npm run crawl
```

Parámetros opcionais:
- `--max-results=10` para limitar os resultados por procura.
- `--chromium-path=/usr/bin/chromium` para indicar manualmente o executable do navegador (por defecto, en Linux próbase `/usr/bin/chromium-browser`, `/usr/bin/chromium`, etc.).
- `--headless` para executar sen interface gráfica.

```bash
npm run crawl -- --headless --max-results=5 --chromium-path="$(which chromium-browser)"
```

### Automatización con cron

O script `executa_crawler.bash` crea rexistros en `logs/monitor.log` e inicia automaticamente un `DISPLAY` virtual con `xvfb` cando é preciso. Antes de empregalo desde `cron`, asegúrate de que é executable:

```bash
chmod +x /home/catalanet/puppeteer/executa_crawler.bash
```

- **Crontab -e**: para executalo cada día ás 04:35, engade:
```cron
35 4 * * * CRON_WAIT=1 /home/catalanet/puppeteer/executa_crawler.bash
```
A variable `CRON_WAIT=1` obriga o script a esperar a que remate o proceso e así recolle todos os rexistros antes de saír.