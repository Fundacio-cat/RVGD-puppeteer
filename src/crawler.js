import { readFile } from 'node:fs/promises';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { createStealthPage, launchBrowser, waitMs } from './browser.js';
import { ensureDatabaseConnection, seguentCerca, seleccionaMides, guardaDb } from './database.js';
import { seleccionaCercador } from './cercadors/@cercador.js';
import { seleccionaNavegador } from './navegador.js';
import { executaProcuraGoogle, GoogleBlockedError } from './cercadors/google.js';
import { nomSensor } from './utils.js';

/**
 * Punto de entrada do crawler. Le a configuración, orquestra o navegador,
 * obtén os resultados e opcionalmente gárdaos en disco e en Postgres.
 */

function formatTimestamp(date = new Date()) {
  const pad = (value) => String(value).padStart(2, '0');
  const year = date.getFullYear();
  const month = pad(date.getMonth() + 1);
  const day = pad(date.getDate());
  const hours = pad(date.getHours());
  const minutes = pad(date.getMinutes());
  const seconds = pad(date.getSeconds());

  return `[${year}-${month}-${day} ${hours}:${minutes}:${seconds}]`;
}

function patchConsoleWithTimestamp() {
  if (console.__timestampPatched) {
    return;
  }

  const originalConsole = {
    log: console.log.bind(console),
    info: console.info.bind(console),
    warn: console.warn.bind(console),
    error: console.error.bind(console),
    debug: console.debug ? console.debug.bind(console) : undefined,
  };

  const wrap = (method) => (...args) => {
    const prefix = formatTimestamp();
    if (args.length === 0) {
      originalConsole[method](prefix);
      return;
    }

    if (typeof args[0] === 'string') {
      originalConsole[method](`${prefix} ${args[0]}`, ...args.slice(1));
    } else {
      originalConsole[method](prefix, ...args);
    }
  };

  console.log = wrap('log');
  console.info = wrap('info');
  console.warn = wrap('warn');
  console.error = wrap('error');
  if (originalConsole.debug) {
    console.debug = wrap('debug');
  }

  Object.defineProperty(console, '__timestampPatched', {
    value: true,
    configurable: false,
    enumerable: false,
    writable: false,
  });
}

patchConsoleWithTimestamp();

const DEFAULT_CONFIG_PATH = path.resolve(process.cwd(), 'config.json');

/**
 * Carga un ficheiro JSON coa configuración básica do crawler.
 * Devolve un obxecto baleiro se o ficheiro non existe ou hai algún erro de lectura.
 */
async function loadConfig(configPath = DEFAULT_CONFIG_PATH) {
  try {
    const content = await readFile(configPath, 'utf8');
    return JSON.parse(content);
  } catch (error) {
    if (error?.code === 'ENOENT') {
      return {};
    }

    console.warn(`Non se puido cargar o ficheiro de configuración "${configPath}":`, error);
    return {};
  }
}

async function closeBrowserSafely(browser, { waitForExitMs = 2_000, killSignal = 'SIGKILL' } = {}) {
  if (!browser) {
    return;
  }

  const browserProcess = typeof browser.process === 'function' ? browser.process() : null;

  try {
    await browser.close();
  } catch (error) {
    console.error('Erro pechando o navegador:', error);
  }

  if (!browserProcess) {
    return;
  }

  const exited = await new Promise((resolve) => {
    if (browserProcess.exitCode !== null) {
      resolve(true);
      return;
    }

    let finished = false;
    let timeoutId;

    const cleanup = () => {
      if (finished) {
        return;
      }
      finished = true;
      clearTimeout(timeoutId);
      if (typeof browserProcess.off === 'function') {
        browserProcess.off('exit', onExit);
      } else {
        browserProcess.removeListener('exit', onExit);
      }
    };

    const onExit = () => {
      cleanup();
      resolve(true);
    };

    timeoutId = setTimeout(() => {
      cleanup();
      resolve(false);
    }, waitForExitMs);

    browserProcess.once('exit', onExit);
  });

  if (!exited && browserProcess.exitCode === null && !browserProcess.killed) {
    try {
      const killed = browserProcess.kill(killSignal);
      if (!killed) {
        console.error(`O proceso de Chromium segue activo; non se puido enviar ${killSignal}.`);
      }
    } catch (error) {
      console.error(`Non se puido matar o proceso de Chromium con ${killSignal}:`, error);
    }
  }
}

/**
 * Executa a procura en Google e xestiona a persistencia segundo as opcións recibidas.
 */
async function runCrawler(query, { databaseUrl = null, searchOptions = {}, browserOptions = {} } = {}) {
  console.log(`Iniciando o crawler...`);

  // Comproba que a cadea de conexión á base de datos estea especificada
  if (!databaseUrl) {
    throw new Error('Cómpre especificar unha cadea de conexión á base de datos (databaseUrl).');
  }

  // Inicia o navegador
  const browser = await launchBrowser(browserOptions);
  const browserProcess = browser.process?.();
  if (browserProcess) {
    browserProcess.once('exit', (code, signal) => {
      console.error(
        `Chromium saíu con código ${code ?? 'null'} e sinal ${signal ?? '-'}`
      );
    });
  }
  let page;

  try {
    page = await createStealthPage(browser);

    // Define o nome do sensor
    const sensor = nomSensor() ?? 'sensor';

    // Establece a conexión á base de datos
    const dbClient = await ensureDatabaseConnection(databaseUrl);

    // Obtén as medidas da xanela
    try {
      const { sizeId, width, height } = await seleccionaMides({ client: dbClient });
      if (width && height) {
        await page.setViewport({ width, height });
        console.log(`Viewport configurado a ${width}x${height} (sizeId=${sizeId ?? 'n/a'})`);
      } else {
        console.log('Non se puido establecer un viewport específico; empregarase o predeterminado.');
      }

      // Obtén o identificador do navegador
      const navegadorId = await seleccionaNavegador({ client: dbClient }) ?? 2;

      // Obtén o identificador do buscador
      const cercadorId = await seleccionaCercador({ client: dbClient }) ?? 1;

      // Obtén o identificador da procura e a consulta que se vai executar
      const nextSearch = await seguentCerca(sensor, { client: dbClient });
      const searchId = nextSearch.searchId;
      if (!searchId) {
        throw new Error(
          `Non se atopou ningunha procura pendente para o sensor "${sensor}". ` +
            'Revisa a táboa de buscas/consultas na base de datos antes de executar o crawler.'
        );
      }
      const searchString = nextSearch.query ?? query;

      // Executa a procura
      const results = await executaProcuraGoogle(page, searchString, {
        ...searchOptions,
        onResult: async (result, index) => {
          await guardaDb({
            client: dbClient,
            sensor,
            navegadorId,
            cercadorId,
            searchId,
            posicio: index + 1,
            titol: result.title ?? null,
            url: result.url ?? null,
            descripcio: result.snippet ?? null,
            mida: sizeId ?? null,
          });
        },
      });

      // Comproba que se obtivesen resultados
      if (results.length === 0) {
        throw new Error(`Non se puideron obter resultados para a procura: ${searchString}`);
      }

      // Comproba que o número de resultados sexa o agardado
      if (typeof searchOptions.maxResults === 'number' && results.length < searchOptions.maxResults) {
        console.warn(`Obtivéronse ${results.length} resultados para "${searchString}".`);
      }

      await waitMs(page, 5_000);

      return results;
    } finally {
      await dbClient.end().catch((error) => {
        console.error('Erro pechando a conexión á base de datos:', error);
      });
    }
  } finally {
    await closeBrowserSafely(browser);
  }
}

/**
 * Analiza argumentos, variables de contorno e ficheiro de configuración e inicia o crawler.
 */
async function main() {
  const args = process.argv.slice(2);
  let configPath = DEFAULT_CONFIG_PATH;

  const configFlagIndex = args.findIndex((arg) => arg.startsWith('--config='));
  if (configFlagIndex !== -1) {
    const [, value] = args[configFlagIndex].split('=');
    if (value?.trim()) {
      configPath = path.resolve(process.cwd(), value.trim());
    }
    args.splice(configFlagIndex, 1);
  }

  const config = await loadConfig(configPath);

  let databaseUrl = config?.database?.url ?? config?.databaseUrl ?? null;
  let searchOptions = {};
  let browserOptions = {};

  if (typeof config?.search === 'object' && config.search !== null) {
    searchOptions = { ...config.search };
  }

  if (typeof config?.browser === 'object' && config.browser !== null) {
    browserOptions = { ...config.browser };
  }

  if (process.env.DATABASE_URL) {
    databaseUrl = process.env.DATABASE_URL;
  }

  for (let i = args.length - 1; i >= 0; i -= 1) {
    const arg = args[i];

    if (arg.startsWith('--db-url=')) {
      const [, value] = arg.split('=');
      databaseUrl = value?.trim() ? value.trim() : null;
      args.splice(i, 1);
    } else if (arg.startsWith('--max-results=')) {
      const [, value] = arg.split('=');
      const parsed = Number.parseInt(value ?? '', 10);
      if (!Number.isNaN(parsed) && parsed > 0) {
        searchOptions = { ...searchOptions, maxResults: parsed };
      }
      args.splice(i, 1);
    } else if (arg.startsWith('--timeout-ms=')) {
      const [, value] = arg.split('=');
      const parsed = Number.parseInt(value ?? '', 10);
      if (!Number.isNaN(parsed) && parsed > 0) {
        searchOptions = { ...searchOptions, timeoutMs: parsed };
      }
      args.splice(i, 1);
    } else if (arg.startsWith('--chromium-path=')) {
      const [, value] = arg.split('=');
      if (value?.trim()) {
        browserOptions = { ...browserOptions, executablePath: value.trim() };
      }
      args.splice(i, 1);
    } else if (arg === '--headless') {
      browserOptions = { ...browserOptions, headless: true };
      args.splice(i, 1);
    } else if (arg.startsWith('--slow-mo=')) {
      const [, value] = arg.split('=');
      const parsed = Number.parseInt(value ?? '', 10);
      if (!Number.isNaN(parsed) && parsed >= 0) {
        browserOptions = { ...browserOptions, slowMo: parsed };
      }
      args.splice(i, 1);
    }
  }

  const query =
    args.length > 0
      ? args.join(' ')
      : typeof config.query === 'string' && config.query.trim()
        ? config.query.trim()
        : 'Como facer castañas e boniatos';

  try {
    await runCrawler(query, { databaseUrl, searchOptions, browserOptions });
  } catch (error) {
    if (error instanceof GoogleBlockedError) {
      console.warn(
        `Detectado por Google (captcha/interstitial); saíndo sen gardar resultados. URL: ${error.googleUrl ?? 'descoñecida'}`
      );
      return;
    }
    console.error('Erro durante a procura:', error);
    process.exitCode = 1;
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}

export { runCrawler };


