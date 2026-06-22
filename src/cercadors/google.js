import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { humanPause, performHumanScroll, randomInt, waitMs } from '../browser.js';

/**
 * Utilidades específicas para interactuar con Google desde o navegador.
 * Inclúe a resolución do banner de cookies, a captura de resultados e o formato CSV.
 */

export const DEFAULT_TIMEOUT_MS = 60_000;
export const DEFAULT_MAX_RESULTS = 10;

const SCREENSHOT_DIR = path.resolve(process.cwd(), 'logs', 'screenshots');

function sanitizeForFilename(value) {
  const normalized = value
    .normalize('NFKD')
    .replace(/[\u0300-\u036f]/g, '')
    .replace(/[^a-zA-Z0-9_-]+/g, '_')
    .replace(/_{2,}/g, '_')
    .replace(/^_+|_+$/g, '');
  return normalized.length > 0 ? normalized : 'query';
}

async function capturaContextoResultados(page, query) {
  try {
    await mkdir(SCREENSHOT_DIR, { recursive: true });
    const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
    const baseName = sanitizeForFilename(query).slice(0, 60);
    const screenshotPath = path.join(SCREENSHOT_DIR, `${timestamp}_${baseName}.png`);

    await page.screenshot({ path: screenshotPath, fullPage: true });
    const content = await page.content();

    console.log(`Captura de pantalla gardada en ${screenshotPath}`);
  } catch (error) {
    console.warn('Non se puido gardar o contexto da páxina de procura.', error);
  }
}

/**
 * Intenta aceptar as cookies de Google se aparece o banner (placeholder actualmente).
 */
export async function aceptaCookiesGoogle(page) {
  // Agarda uns segundos para que apareza o banner
  await waitMs(page, 5_000);

  try {
    // Busca o botón de aceptación percorrendo o DOM no contexto da páxina.
    const clicked = await page.evaluate(() => {
      const normalize = (text) => (text ? text.replace(/\s+/g, ' ').trim().toLowerCase() : '');

      const ACCEPT_TOKENS = [
        // Galego / castelán (Google adoita reutilizar "Aceptar todo")
        'aceptar todo',
        'aceptar todas',
        'acepto',
        // Catalán
        'accepta-ho tot',
        'acceptar-ho tot',
        'accepta tot',
        'acceptar tot',
        // Inglés
        'accept all',
        'i agree',
      ];

      const REJECT_TOKENS = [
        // Galego / castelán
        'rexeitar',
        'rechazar',
        'rexeito',
        // Catalán
        'rebutjar',
        // Inglés
        'reject',
        'decline',
        'don’t agree',
        "don't agree",
      ];

      const looksLikeAccept = (rawText) => {
        const text = normalize(rawText);
        if (!text) return false;
        if (REJECT_TOKENS.some((token) => text.includes(token))) return false;
        return ACCEPT_TOKENS.some((token) => text.includes(token));
      };

      const buttons = Array.from(document.querySelectorAll('button'));
      const target =
        buttons.find((button) => looksLikeAccept(button.textContent)) ??
        buttons.find((button) =>
          Array.from(button.querySelectorAll('div, span')).some((node) => looksLikeAccept(node.textContent ?? ''))
        );

      if (target) {
        target.click();
        return true;
      }

      return false;
    });

    if (clicked) {
      await waitMs(page, 5_000);
      return true;
    }
  } catch (error) {
    console.warn('Non se puido interactuar co banner de cookies:', error);
  }

  // Se non se atopa ningún botón válido, agarda un extra e devolve false
  await waitMs(page, 5_000);
  return false;
}

/**
 * Recorre as ligazóns visibles e constrúe resultados con título, URL e descrición.
 */
export async function procuraDatos(page, maxResults, timeoutMs, { onResult } = {}) {
  console.log('procuraDatos: agardando o selector div#search...');
  await page.waitForSelector('div#search', { timeout: timeoutMs }).catch(() => {
    console.warn('procuraDatos: non se atopou div#search no tempo especificado.');
    return null;
  });
  
  console.log('procuraDatos: realizando scroll humano...');
  await performHumanScroll(page);

  console.log('procuraDatos: recuperando ligazóns de resultados...');
  const results = [];
  const seenUrls = new Set();
  const anchors = await recuperaLigazonsResultados(page);
  const totalCandidates = anchors.length;

  if (totalCandidates === 0) {
    console.warn('Non se detectou ningún bloque de resultado en Google cos selectores actuais.');
  } else {
    console.log(`Detectados ${totalCandidates} candidatos de resultado en Google.`);
  }

  let discardedWithoutData = 0;
  let duplicateCount = 0;
  let skippedByLimit = 0;

  for (const anchor of anchors) {
    if (results.length >= maxResults) {
      await anchor.dispose().catch(() => {});
      skippedByLimit++;
      continue;
    }

    const entry = await procesaResultadoLigazon(anchor);
    await anchor.dispose().catch(() => {});

    if (!entry || !entry.link || !entry.titol) {
      discardedWithoutData++;
      continue;
    }

    if (seenUrls.has(entry.link)) {
      duplicateCount++;
      continue;
    }

    const normalizedResult = {
      title: entry.titol,
      url: entry.link,
      snippet: entry.description ?? '',
    };

    results.push(normalizedResult);

    if (typeof onResult === 'function') {
      try {
        await onResult(normalizedResult, results.length - 1);
      } catch (error) {
        console.error('procuraDatos: erro notificado por onResult.', error);
        throw error;
      }
    }

    seenUrls.add(entry.link);

    if (results.length < maxResults) {
      await humanPause(page, 260, 520);
    }
  }

  console.log(
    `Resultados acumulados: ${results.length} (candidatos: ${totalCandidates}, duplicados: ${duplicateCount}, descartados: ${discardedWithoutData}, límite: ${skippedByLimit}).`
  );

  return results;
}

async function recuperaLigazonsResultados(page) {
  if (page && typeof page.$x === 'function') {
    try {
      const xpathResults = await page.$x('//div[@id="search"]//a[h3]');
      console.log(`recuperaLigazonsResultados: XPath trobou ${xpathResults.length} resultados.`);
      return xpathResults;
    } catch (error) {
      console.warn('Non se puido obter os resultados con XPath; empregarase o fallback CSS.', error);
    }
  }

  const anchors = [];
  const candidates = await page.$$('div#search a').catch(() => []);
  console.log(`recuperaLigazonsResultados: CSS selector trobou ${candidates.length} candidatos de enllaces.`);

  for (const candidate of candidates) {
    try {
      const heading = await candidate.$('h3');
      if (heading) {
        anchors.push(candidate);
        await heading.dispose().catch(() => {});
      } else {
        await candidate.dispose().catch(() => {});
      }
    } catch {
      await candidate.dispose().catch(() => {});
    }
  }

  console.log(`recuperaLigazonsResultados: fallback CSS filtrando h3 trobou ${anchors.length} resultados.`);
  return anchors;
}

/**
 * Realiza a secuencia completa de procura en Google e devolve os resultados atopados.
 */
export async function executaProcuraGoogle(
  page,
  query,
  { maxResults = DEFAULT_MAX_RESULTS, timeoutMs = DEFAULT_TIMEOUT_MS, onResult } = {}
) {
  console.log(`Executando procura en Google por: "${query}"`);

  page.setDefaultTimeout(timeoutMs);
  page.setDefaultNavigationTimeout(timeoutMs);

  // Abre Google e prepara o contexto de procura.
  await page.goto('https://www.google.com', {
    waitUntil: 'domcontentloaded',
    timeout: timeoutMs,
  });

  await humanPause(page, 220, 420);

  const consentAccepted = await aceptaCookiesGoogle(page);
  console.log(
    consentAccepted
      ? "Aceptáronse as cookies de Google."
      : "Non se atopou ningún banner de cookies."
  );

  const searchBox =
    (await page.waitForSelector('form[action=\"/search\"] [name=\"q\"]', {
      visible: true,
      timeout: 15_000,
    }).catch(() => null)) ??
    (await page.waitForSelector('[name=\"q\"]', { visible: true, timeout: 10_000 }).catch(() => null));

  if (!searchBox) {
    throw new Error("Non se puido localizar o cadro de procura de Google.");
  }

  await searchBox.click({ clickCount: 3, delay: randomInt(40, 80) });
  await humanPause(page, 80, 160);
  await searchBox.type(query, { delay: randomInt(60, 120) });

  const navigationPromise = page
    .waitForNavigation({ waitUntil: 'domcontentloaded', timeout: timeoutMs })
    .catch(() => null);
  await page.keyboard.press('Enter');
  await navigationPromise;
  await capturaContextoResultados(page, query);

  // Debug: comprova si existe div#search
  const hasSearchDiv = await page.evaluate(() => {
    const searchDiv = document.querySelector('div#search');
    const allDivs = document.querySelectorAll('div[id]');
    const divIds = Array.from(allDivs).map(d => d.id).slice(0, 20);
    return {
      hasSearch: !!searchDiv,
      searchDivId: searchDiv?.id ?? null,
      firstDivIds: divIds,
    };
  });
  console.log('Debug estructura DOM:', JSON.stringify(hasSearchDiv, null, 2));

  const results = await procuraDatos(page, maxResults, timeoutMs, { onResult });

  if (results.length === 0) {
    console.warn(
      'A procura non devolveu resultados. Revisa os selectores ou comproba se Google modificou o DOM.'
    );
    throw new Error(`Non se puideron obter resultados para a procura: ${query}`);
  }

  return results;
}

/**
 * Procesa un elemento de ligazón para extraer o título, a URL e a descrición.
 */
async function procesaResultadoLigazon(anchor) {
  try {
    return await anchor.evaluate((node) => {
      const normalize = (text) => {
        if (!text) {
          return null;
        }
        const cleaned = text.replace(/\s+/g, ' ').trim();
        return cleaned.length > 0 ? cleaned : null;
      };

      const evaluateNode = (context, xpath) => {
        try {
          return document
            .evaluate(xpath, context, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null)
            .singleNodeValue;
        } catch {
          return null;
        }
      };

      const h3 = node.querySelector('h3');
      if (!h3) {
        return null;
      }

      let fallbackText = null;
      let esPregunta = false;

      try {
        const divPreguntes = evaluateNode(
          h3,
          './parent::span/parent::div/parent::div/parent::div/parent::div/parent::div/parent::div/parent::div/parent::div/parent::div/parent::div/parent::div/parent::div/parent::div/parent::div/parent::div/parent::div/parent::div'
        );
        if (divPreguntes) {
          const divMesPreguntes = evaluateNode(divPreguntes, './div/div[1]/div/div');
          if (divMesPreguntes) {
            const mesPreguntes = evaluateNode(divMesPreguntes, './/span');
            if (mesPreguntes) {
              const text = mesPreguntes.textContent ?? '';
              const normalized = text.trim().toLowerCase();
              fallbackText = normalize(text);
              if (normalized.includes('preguntes')) {
                esPregunta = true;
              }
            }
          }
        }
      } catch {
        // Ignóranse erros de detección para continuar o procesamento.
      }

      if (esPregunta) {
        return { link: null, titol: null, description: fallbackText };
      }

      const link = node.getAttribute('href') ?? null;
      if (!link || !link.startsWith('http')) {
        return null;
      }

      const titol = normalize(h3.textContent ?? '');
      if (!titol) {
        return null;
      }

      if (/[?¿]\s*$/.test(titol)) {
        return null;
      }

      const collectLastSpanText = (element) => {
        if (!element) {
          return null;
        }
        const snapshot = document.evaluate(
          './/span',
          element,
          null,
          XPathResult.ORDERED_NODE_SNAPSHOT_TYPE,
          null
        );
        if (!snapshot || snapshot.snapshotLength === 0) {
          return null;
        }
        const target = snapshot.snapshotItem(snapshot.snapshotLength - 1);
        return normalize(target?.textContent ?? '');
      };

      let description = null;
      const primaryParent = evaluateNode(
        node,
        './parent::span/parent::div/parent::div/parent::div/parent::div/parent::div'
      );
      if (primaryParent) {
        let descriptionNode = evaluateNode(primaryParent, './div/div[2]');
        let text = collectLastSpanText(descriptionNode);
        if (!text) {
          descriptionNode = evaluateNode(primaryParent, './div/div[3]');
          text = collectLastSpanText(descriptionNode);
        }
        if (text) {
          description = text;
        }
      }

      const secondaryParent = evaluateNode(
        node,
        './parent::span/parent::div/parent::div/parent::div/parent::div'
      );
      if (secondaryParent) {
        let descriptionNode = evaluateNode(secondaryParent, './div[3]');
        let text = normalize(descriptionNode?.textContent ?? '');
        if (!text) {
          descriptionNode = evaluateNode(secondaryParent, './div[2]');
          text = normalize(descriptionNode?.textContent ?? '');
        }
        if (text) {
          description = text;
        }
      }

      if (!description) {
        description = fallbackText;
      }

      return {
        link,
        titol,
        description: description ?? null,
      };
    });
  } catch (error) {
    console.error('procuraDatos: erro procesando un resultado.', error);
    return null;
  }
}

