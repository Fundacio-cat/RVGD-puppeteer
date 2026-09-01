// Script de diagnóstico illado: NON usa nada do noso código (nin applyStealth,
// nin performHumanScroll, nin os selectores de google.js). Só Puppeteer "pelado"
// coa mesma configuración de lanzamento do sensor. Obxectivo: comprobar se o
// bloqueo de ~2 minutos en comandos CDP (DOM.describeNode, Runtime.callFunctionOn...)
// pasa TAMÉN sen o noso código por medio, para saber se o problema é do
// Chromium/Puppeteer/contorno ou de algo que engadimos nós.
//
// Uso:
//   node scripts/diagnostic-minimal.mjs "consulta a probar"
//
// Le a mesma configuración de navegador que usa o crawler (config.json) para
// manter as condicións o máis parecidas posible.

import { readFile } from 'node:fs/promises';
import path from 'node:path';
import puppeteer from 'puppeteer';

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

async function medido(etiqueta, promesa) {
  const inicio = Date.now();
  console.log(`[${timestamp()}] Comeza: ${etiqueta}`);
  try {
    const resultado = await promesa;
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
    const page = await medido('newPage', browser.newPage());
    page.on('error', (error) => console.error(`[${timestamp()}] page 'error' (crash):`, error));
    page.on('close', () => console.warn(`[${timestamp()}] page 'close'`));

    await medido(
      'goto google.com/search directamente',
      page.goto(`https://www.google.com/search?q=${encodeURIComponent(query)}&hl=gl`, {
        waitUntil: 'domcontentloaded',
        timeout: 60_000,
      })
    );

    // Pequena espera fixa, sen humanPause nin scroll simulado.
    await new Promise((resolve) => setTimeout(resolve, 3_000));

    await medido('page.$(\'div#rso\')', page.$('div#rso'));
    await medido('page.$$(\'div#search a\')', page.$$('div#search a'));
    await medido('page.content()', page.content());
    await medido('page.evaluate(() => document.title)', page.evaluate(() => document.title));
  } finally {
    await medido('browser.close()', browser.close()).catch(() => {});
  }

  console.log(`[${timestamp()}] Fin do diagnóstico.`);
}

main().catch((error) => {
  console.error('Fallo xeral do diagnóstico:', error);
  process.exitCode = 1;
});
