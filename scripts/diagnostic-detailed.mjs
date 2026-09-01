// Script de diagnóstico detallado: reproduce EXACTAMENTE o fluxo real
// (executaProcuraGoogle + procuraDatos), usando as nosas funcións reais
// (applyStealth, performHumanScroll, aceptaCookiesGoogle...), pero con cada
// paso cronometrado individualmente. Obxectivo: saber EXACTAMENTE en que paso
// aparece o primeiro atraso grande, xa que o diagnóstico mínimo (sen ningún
// código noso) funcionou perfectamente e rápido.
//
// Uso:
//   node scripts/diagnostic-detailed.mjs "consulta a probar"

import { readFile, mkdir } from 'node:fs/promises';
import path from 'node:path';
import puppeteer from 'puppeteer';
import { applyStealth, performHumanScroll, humanPause, randomInt } from '../src/browser.js';
import { aceptaCookiesGoogle, DEFAULT_TIMEOUT_MS } from '../src/cercadors/google.js';

async function loadBrowserConfig() {
  try {
    const raw = await readFile(path.resolve(process.cwd(), 'config.json'), 'utf8');
    const cfg = JSON.parse(raw);
    return cfg?.browser ?? {};
  } catch (error) {
    console.warn('Non se puido ler config.json; usaranse valores por defecto.', error);
    return {};
  }
}

function timestamp() {
  return new Date().toISOString();
}

async function medido(etiqueta, fn) {
  const inicio = Date.now();
  console.log(`[${timestamp()}] Comeza: ${etiqueta}`);
  try {
    const resultado = await fn();
    console.log(`[${timestamp()}] OK (${Date.now() - inicio}ms): ${etiqueta}`);
    return resultado;
  } catch (error) {
    console.error(`[${timestamp()}] ERRO (${Date.now() - inicio}ms) en "${etiqueta}":`, error);
    throw error;
  }
}

async function main() {
  const query = process.argv.slice(2).join(' ') || 'galiza';
  const browserConfig = await loadBrowserConfig();
  const timeoutMs = DEFAULT_TIMEOUT_MS;

  const browser = await puppeteer.launch({
    headless: browserConfig.headless ?? 'new',
    executablePath: browserConfig.executablePath,
    args: browserConfig.args ?? ['--disable-dev-shm-usage', '--disable-gpu'],
    protocolTimeout: 120_000,
  });

  const browserProcess = browser.process?.();
  browserProcess?.once('exit', (code, signal) => {
    console.error(`[${timestamp()}] Chromium saíu con código ${code ?? 'null'} e sinal ${signal ?? '-'}`);
  });
  browser.on('disconnected', () => {
    console.warn(`[${timestamp()}] browser 'disconnected'`);
  });

  try {
    const page = await medido('newPage', () => browser.newPage());
    page.on('error', (error) => console.error(`[${timestamp()}] page 'error' (crash):`, error));
    page.on('close', () => console.warn(`[${timestamp()}] page 'close'`));

    await medido('applyStealth (inclúe setViewport 1366x768)', () => applyStealth(page));

    // No pipeline real (crawler.js), o viewport defínese ANTES de navegar,
    // non despois. Corrixido aquí para non introducir un artefacto propio.
    await medido('setViewport (1920x1080, coma no sensor, ANTES de navegar)', () =>
      page.setViewport({ width: 1920, height: 1080 })
    );

    page.setDefaultTimeout(timeoutMs);
    page.setDefaultNavigationTimeout(timeoutMs);

    await medido('goto https://www.google.com (páxina de inicio)', () =>
      page.goto('https://www.google.com', { waitUntil: 'domcontentloaded', timeout: timeoutMs })
    );

    await medido('humanPause inicial', () => humanPause(page, 220, 420));

    const consentAccepted = await medido('aceptaCookiesGoogle', () => aceptaCookiesGoogle(page));
    console.log(`  -> cookies aceptadas: ${consentAccepted}`);

    const searchBox = await medido('waitForSelector caixa de busca', () =>
      page
        .waitForSelector('form[action="/search"] [name="q"]', { visible: true, timeout: 15_000 })
        .catch(() => page.waitForSelector('[name="q"]', { visible: true, timeout: 10_000 }))
    );

    await medido('click + type na caixa de busca', async () => {
      await searchBox.click({ clickCount: 3, delay: randomInt(40, 80) });
      await humanPause(page, 80, 160);
      await searchBox.type(query, { delay: randomInt(60, 120) });
    });

    await medido('Enter + waitForNavigation (resultados)', async () => {
      const navigationPromise = page
        .waitForNavigation({ waitUntil: 'domcontentloaded', timeout: timeoutMs })
        .catch(() => null);
      await page.keyboard.press('Enter');
      await navigationPromise;
    });

    await mkdir('/tmp/diagnostic-screenshots', { recursive: true }).catch(() => {});
    await medido('page.screenshot({ fullPage: true }) <-- SOSPEITOSO PRINCIPAL', () =>
      page.screenshot({ path: '/tmp/diagnostic-screenshots/diag.png', fullPage: true })
    );

    const contentAntes = await medido('page.content() despois do screenshot', () => page.content());
    const { writeFile } = await import('node:fs/promises');
    await writeFile('/tmp/diagnostic-screenshots/antes-wait.html', contentAntes, 'utf8').catch(() => {});

    try {
      await medido('waitForFunction resultados orgánicos (div#rso/search a h3)', () =>
        page.waitForFunction(
          () => {
            const container = document.querySelector('div#rso') ?? document.querySelector('div#search');
            return !!container && container.querySelectorAll('a h3').length > 0;
          },
          { timeout: timeoutMs }
        )
      );
    } catch (error) {
      // Se fallou, gardamos o DOM tal cal está nese intre e contamos á man
      // canto hai, para saber se realmente non hai resultados ou se é outra cousa.
      const diagnostico = await page
        .evaluate(() => ({
          hasRso: !!document.querySelector('div#rso'),
          hasSearch: !!document.querySelector('div#search'),
          rsoAnchorsWithH3: document.querySelector('div#rso')
            ? document.querySelector('div#rso').querySelectorAll('a h3').length
            : null,
          searchAnchorsWithH3: document.querySelector('div#search')
            ? document.querySelector('div#search').querySelectorAll('a h3').length
            : null,
          bodyTextSnippet: document.body?.innerText?.slice(0, 300) ?? null,
          url: location.href,
        }))
        .catch((evalError) => ({ evalError: String(evalError) }));
      console.error(`[${timestamp()}] Diagnóstico despois do fallo de waitForFunction:`, diagnostico);
      const contentDespois = await page.content().catch(() => null);
      if (contentDespois) {
        await writeFile('/tmp/diagnostic-screenshots/despois-wait.html', contentDespois, 'utf8').catch(() => {});
      }
      throw error;
    }

    await medido('performHumanScroll', () => performHumanScroll(page));

    await medido('page.$(\'div#rso\') despois de todo o fluxo real', () => page.$('div#rso'));
    await medido('page.$$(\'div#search a\') despois de todo o fluxo real', () => page.$$('div#search a'));
  } finally {
    await medido('browser.close()', () => browser.close()).catch(() => {});
  }

  console.log(`[${timestamp()}] Fin do diagnóstico detallado.`);
}

main().catch((error) => {
  console.error('Fallo xeral do diagnóstico:', error);
  process.exitCode = 1;
});
