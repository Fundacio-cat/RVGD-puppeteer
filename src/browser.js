import puppeteer from 'puppeteer';
import { access } from 'node:fs/promises';
import { constants as fsConstants } from 'node:fs';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';

/**
 * Utilidades relacionadas co navegador e a adaptación do comportamento para que pareza humano.
 * Inclúe a configuración stealth, o lanzamento do navegador e pequenas pausas humanizadas.
 */

function delay(ms) {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

/**
 * Agarda un número determinado de milisegundos, empregando `page.waitForTimeout` se é posible.
 */
export async function waitMs(page, ms) {
  if (page && typeof page.waitForTimeout === 'function') {
    try {
      await page.waitForTimeout(ms);
      return;
    } catch {
      // Se falla, empregamos o fallback xenérico.
    }
  }

  await delay(ms);
}

/**
 * Devolve un enteiro aleatorio entre `min` e `max` (ambos inclusive) para achegar variabilidade humana.
 */
export function randomInt(min, max) {
  return Math.floor(Math.random() * (max - min + 1)) + min;
}

/**
 * Fai unha pausa aleatoria dentro do rango especificado para simular o comportamento humano.
 */
export async function humanPause(page, minMs, maxMs) {
  const duration = randomInt(minMs, maxMs);
  await waitMs(page, duration);
  return duration;
}

/**
 * Realiza desprazamentos cara abaixo e, ocasionalmente, cara arriba para evitar patróns robotizados.
 */
export async function performHumanScroll(page) {
  try {
    const scrollTimes = randomInt(1, 2);
    for (let i = 0; i < scrollTimes; i += 1) {
      await page.mouse.wheel(0, randomInt(320, 640));
      await humanPause(page, 180, 420);
    }

    if (Math.random() < 0.45) {
      await page.mouse.wheel(0, -randomInt(200, 360));
      await humanPause(page, 160, 320);
    }
  } catch (error) {
    // Continúa sen desprazamento para manter a robustez, pero rexistra o erro:
    // un timeout aquí adoita indicar que o navegador deixou de responder aos
    // comandos de Puppeteer (páxina moi pesada, protocolTimeout esgotado, etc.).
    console.warn('performHumanScroll: erro durante o desprazamento humanizado.', error);
  }

  await humanPause(page, 220, 420);
}

/**
 * Modifica o axente de usuario e diversas propiedades do navegador para reducir a detección.
 */
export async function applyStealth(page) {
  const userAgent =
    'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36';

  await page.setUserAgent(userAgent);
  await page.setExtraHTTPHeaders({
    'Accept-Language': 'gl-ES,gl;q=0.9,es-ES;q=0.8,pt-PT;q=0.7,en-US;q=0.6',
  });

  await page.evaluateOnNewDocument(() => {
    Object.defineProperty(navigator, 'webdriver', { get: () => false });

    window.navigator.chrome = {
      runtime: {},
    };

    Object.defineProperty(navigator, 'languages', {
      get: () => ['gl-ES', 'gl', 'es-ES', 'pt-PT', 'en-US'],
    });

    Object.defineProperty(navigator, 'plugins', {
      get: () => [1, 2, 3, 4, 5],
    });

    const permissions = window.navigator.permissions;
    if (permissions && typeof permissions.query === 'function') {
      const originalQuery = permissions.query.bind(permissions);
      permissions.query = (parameters) =>
        parameters?.name === 'notifications'
          ? Promise.resolve({ state: 'denied', onchange: null })
          : originalQuery(parameters);
    }

    // Sen GPU real, Chromium renderiza por software e o WebGL expón "Google
    // SwiftShader" coma renderer: é un dos sinais de detección de automatización
    // máis coñecidos e usados polos sistemas antibot. Suplantámolo por valores
    // habituais nun equipo de escritorio normal.
    const spoofWebGLRenderer = (prototype) => {
      if (!prototype || typeof prototype.getParameter !== 'function') {
        return;
      }
      const originalGetParameter = prototype.getParameter;
      prototype.getParameter = function (parameter) {
        if (parameter === 37445) {
          return 'Google Inc. (Intel)';
        }
        if (parameter === 37446) {
          return 'ANGLE (Intel, Intel(R) Iris(R) Xe Graphics, OpenGL 4.5)';
        }
        return originalGetParameter.call(this, parameter);
      };
    };
    spoofWebGLRenderer(window.WebGLRenderingContext?.prototype);
    spoofWebGLRenderer(window.WebGL2RenderingContext?.prototype);
  });

  await page.setViewport({ width: 1366, height: 768, deviceScaleFactor: 1 });
}

const DEFAULT_PROTOCOL_TIMEOUT_MS = 120_000;

const BASE_LAUNCH_ARGS = [
  '--no-sandbox',
  '--disable-setuid-sandbox',
  '--disable-blink-features=AutomationControlled',
  '--lang=gl-ES,gl;q=0.9,es-ES;q=0.8,pt-PT;q=0.7,en-US;q=0.6',
  '--window-size=1366,768',
];

const LINUX_EXTRA_LAUNCH_ARGS = [
  '--disable-dev-shm-usage',
  '--disable-gpu',
];

const LINUX_ARM_EXTRA_LAUNCH_ARGS = [
  '--single-process',
  '--no-zygote',
];

const LINUX_CHROMIUM_CANDIDATES = [
  '/usr/bin/chromium-browser',
  '/usr/bin/chromium',
  '/usr/lib/chromium/chromium',
  '/usr/lib/chromium-browser/chromium-browser',
  '/snap/bin/chromium',
];

const execFileAsync = promisify(execFile);
const HEADLESS_MODE_ALIASES = new Map([
  ['new', 'new'],
  ['chrome', 'new'],
  ['true', 'old'],
  ['1', 'old'],
  ['old', 'old'],
  ['legacy', 'old'],
  ['shell', 'shell'],
  ['false', false],
  ['0', false],
]);

const chromiumVersionCache = new Map();

function normalizeHeadless(value) {
  if (value === null || value === undefined) {
    return undefined;
  }

  if (typeof value === 'boolean') {
    return value ? 'old' : false;
  }

  if (typeof value === 'string') {
    const trimmed = value.trim();
    if (trimmed === '') {
      return undefined;
    }

    const alias = HEADLESS_MODE_ALIASES.get(trimmed.toLowerCase());
    if (alias !== undefined) {
      return alias;
    }

    return trimmed;
  }

  return value;
}

async function detectChromiumVersion(executablePath) {
  if (!executablePath) {
    return null;
  }

  if (chromiumVersionCache.has(executablePath)) {
    return chromiumVersionCache.get(executablePath);
  }

  try {
    const { stdout } = await execFileAsync(executablePath, ['--version'], {
      timeout: 3_000,
      windowsHide: true,
    });
    const output = stdout?.trim() ?? '';
    const match = output.match(/(\d+)\.(\d+)\.(\d+)\.(\d+)/);
    const major = match ? Number.parseInt(match[1], 10) : null;
    const info = {
      major: Number.isNaN(major) ? null : major,
      full: output || null,
    };
    chromiumVersionCache.set(executablePath, info);
    return info;
  } catch (error) {
    console.warn('Non se puido determinar a versión de Chromium:', error?.message ?? error);
    const info = { major: null, full: null };
    chromiumVersionCache.set(executablePath, info);
    return info;
  }
}

async function findFirstExecutable(candidates) {
  for (const candidate of candidates) {
    try {
      await access(candidate, fsConstants.X_OK);
      return candidate;
    } catch {
      // Se o candidato non existe ou non é executable, probamos o seguinte.
    }
  }

  return null;
}

async function isExecutable(filePath) {
  if (!filePath) {
    return false;
  }
  try {
    await access(filePath, fsConstants.X_OK);
    return true;
  } catch {
    return false;
  }
}

function buildDefaultLaunchArgs(userArgs = []) {
  const defaultArgs = [...BASE_LAUNCH_ARGS];

  if (process.platform === 'linux') {
    defaultArgs.push(...LINUX_EXTRA_LAUNCH_ARGS);

    if (process.arch.startsWith('arm')) {
      defaultArgs.push(...LINUX_ARM_EXTRA_LAUNCH_ARGS);
    }
  }

  const mergedArgs = [...defaultArgs, ...userArgs];
  return [...new Set(mergedArgs)];
}

function parsePositiveInt(value, { allowZero = false } = {}) {
  if (value === null || value === undefined) {
    return null;
  }

  const trimmed = `${value}`.trim();
  if (trimmed === '') {
    return null;
  }

  const parsed = Number.parseInt(trimmed, 10);
  if (Number.isNaN(parsed) || parsed < (allowZero ? 0 : 1)) {
    return null;
  }

  return parsed;
}

function parseSlowMo(value) {
  return parsePositiveInt(value, { allowZero: true });
}

export async function launchBrowser(launchOptions = {}) {
  const userArgs = Array.isArray(launchOptions.args) ? launchOptions.args : [];
  const {
    headless: requestedHeadlessFromLaunch,
    slowMo: requestedSlowMo,
    args: _ignoredArgs,
    protocolTimeout: requestedProtocolTimeout,
    ...remainingLaunchOptions
  } = launchOptions;
  const envHeadlessBoolean = normalizeHeadless(process.env.PUPPETEER_HEADLESS);
  const envHeadlessMode = normalizeHeadless(process.env.PUPPETEER_HEADLESS_MODE);
  const requestedHeadless = normalizeHeadless(requestedHeadlessFromLaunch);
  let headlessPreference =
    requestedHeadless ??
    envHeadlessMode ??
    envHeadlessBoolean ??
    'old';

  if (headlessPreference === false || headlessPreference === 'shell') {
    console.warn(
      'Solicitouse executar con interface gráfica, mais o sensor precisa headless; aplicarase o modo "old".'
    );
    headlessPreference = 'old';
  }

  if (headlessPreference === 'chrome') {
    headlessPreference = 'new';
  }

  const envSlowMo =
    parseSlowMo(process.env.PUPPETEER_SLOWMO) ??
    parseSlowMo(process.env.PUPPETEER_SLOWMO_MS);
  const defaultSlowMo =
    headlessPreference === 'old' ? 80 : headlessPreference === 'new' ? 40 : 0;
  const slowMoPreference = requestedSlowMo ?? envSlowMo ?? defaultSlowMo;
  const envProtocolTimeout =
    parsePositiveInt(process.env.PUPPETEER_PROTOCOL_TIMEOUT) ??
    parsePositiveInt(process.env.PUPPETEER_PROTOCOL_TIMEOUT_MS);
  const protocolTimeoutPreference =
    requestedProtocolTimeout ?? envProtocolTimeout ?? DEFAULT_PROTOCOL_TIMEOUT_MS;

  const resolvedOptions = {
    headless: headlessPreference,
    slowMo: slowMoPreference,
    protocolTimeout: protocolTimeoutPreference,
    defaultViewport: null,
    // Evita o transporte por WebSocket (un socket TCP local) e comunícase con
    // Chromium mediante pipes do sistema operativo: máis robusto en servidores
    // onde se observaron desconexións CDP intermitentes e inexplicables.
    pipe: true,
    ...remainingLaunchOptions,
    args: buildDefaultLaunchArgs(userArgs),
  };

  if (resolvedOptions.executablePath) {
    const ok = await isExecutable(resolvedOptions.executablePath);
    if (!ok) {
      console.warn(
        `A ruta executablePath="${resolvedOptions.executablePath}" non é executable. ` +
          'Ignórase para que Puppeteer empregue o navegador por defecto.'
      );
      delete resolvedOptions.executablePath;
    }
  }

  if (!resolvedOptions.executablePath) {
    const envExecutable =
      process.env.PUPPETEER_EXECUTABLE_PATH ??
      process.env.CHROMIUM_PATH ??
      process.env.CHROME_PATH ??
      null;
    if (envExecutable) {
      resolvedOptions.executablePath = envExecutable;
    }
  }

  if (!resolvedOptions.executablePath && process.platform === 'linux') {
    const chromiumExecutable = await findFirstExecutable(LINUX_CHROMIUM_CANDIDATES);

    if (chromiumExecutable) {
      resolvedOptions.executablePath = chromiumExecutable;
    }
  }

  if (!resolvedOptions.executablePath) {
    console.warn(
      'Non se atopou ningún executable de Chromium/Chrome. ' +
        'Configura `browserOptions.executablePath`, `--chromium-path` ou `PUPPETEER_EXECUTABLE_PATH`.'
    );
  }

  if (
    process.platform === 'linux' &&
    resolvedOptions.executablePath &&
    (resolvedOptions.headless === 'new' || resolvedOptions.headless === true)
  ) {
    const forceNew =
      ['1', 'true', 'TRUE'].includes(process.env.PUPPETEER_HEADLESS_FORCE_NEW ?? '');
    if (!forceNew) {
      const versionInfo = await detectChromiumVersion(resolvedOptions.executablePath);
      if (versionInfo?.major !== null && versionInfo.major < 120) {
        console.warn(
          `Chromium ${versionInfo.major} detectado na ruta personalizada. ` +
            'O modo headless "new" pode ser inestable; cambiando a "old".'
        );
        resolvedOptions.headless = 'old';
      }
    }
  }

  if (resolvedOptions.headless === true) {
    resolvedOptions.headless = 'new';
  }

  if (typeof resolvedOptions.headless === 'string') {
    console.log(`Iniciando Chromium en modo headless "${resolvedOptions.headless}".`);
  } else {
    console.log(`Iniciando Chromium con headless ${resolvedOptions.headless ? 'activado' : 'desactivado'}.`);
  }

  return puppeteer.launch(resolvedOptions);
}

/**
 * Rexistra listeners de diagnóstico para detectar canda o proceso renderizador
 * morre ou queda nun estado zombi. Sen isto, un crash silencioso só se
 * manifesta coma un ProtocolError xenérico despois de esgotar o protocolTimeout.
 */
function attachCrashDiagnostics(page) {
  page.on('error', (error) => {
    console.error('createStealthPage: a páxina CRASHEOU (proceso renderizador morto).', error);
  });
  page.on('close', () => {
    console.warn('createStealthPage: a páxina pechouse inesperadamente.');
  });
}

/**
 * Abre unha lapela nova e aplícalle a configuración stealth antes de devolvela.
 */
export async function createStealthPage(browser) {
  const page = await browser.newPage();
  attachCrashDiagnostics(page);
  await applyStealth(page);
  return page;
}

