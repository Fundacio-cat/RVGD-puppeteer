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
  let screenshotPath = null;
  let htmlPath = null;
  let content = null;

  try {
    await mkdir(SCREENSHOT_DIR, { recursive: true });
    const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
    const baseName = sanitizeForFilename(query).slice(0, 60);
    screenshotPath = path.join(SCREENSHOT_DIR, `${timestamp}_${baseName}.png`);

    await page.screenshot({ path: screenshotPath, fullPage: true });
    content = await page.content();
    htmlPath = path.join(SCREENSHOT_DIR, `${timestamp}_${baseName}.html`);
    await writeFile(htmlPath, content, 'utf8');

    console.log(`Captura de pantalla gardada en ${screenshotPath}`);
    console.log(`HTML da páxina gardado en ${htmlPath}`);
  } catch (error) {
    console.warn('Non se puido gardar o contexto da páxina de procura.', error);
  }

  return { screenshotPath, htmlPath, content };
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

const GOOGLE_REDIRECT_TIMEOUT_MS = 8_000;
const GOOGLE_REDIRECT_USER_AGENT =
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36';

/**
 * Google xa non inclúe a URL de destino en claro no href dos resultados: agora usa
 * redireccións opacas (`/goto?url=<token>`) que só se poden resolver seguindo o salto HTTP
 * (Google responde cun 302 co `Location` real; non fai falla cargar a páxina de destino).
 */
async function resolveResultUrl(rawLink) {
  if (!rawLink) {
    return null;
  }

  if (rawLink.startsWith('http')) {
    return rawLink;
  }

  let absoluteUrl;
  try {
    absoluteUrl = new URL(rawLink, 'https://www.google.com');
  } catch {
    return null;
  }

  const directParam = absoluteUrl.searchParams.get('q');
  if (directParam && directParam.startsWith('http')) {
    return directParam;
  }

  if (absoluteUrl.pathname !== '/goto' && absoluteUrl.pathname !== '/url') {
    return null;
  }

  const controller = new AbortController();
  const timeoutId = setTimeout(() => controller.abort(), GOOGLE_REDIRECT_TIMEOUT_MS);

  try {
    const response = await fetch(absoluteUrl, {
      redirect: 'manual',
      signal: controller.signal,
      headers: { 'User-Agent': GOOGLE_REDIRECT_USER_AGENT },
    });
    const location = response.headers.get('location');
    return location && location.startsWith('http') ? location : null;
  } catch (error) {
    console.warn(`resolveResultUrl: non se puido resolver a redirección de "${rawLink}".`, error);
    return null;
  } finally {
    clearTimeout(timeoutId);
  }
}

/**
 * Recorre as ligazóns visibles e constrúe resultados con título, URL e descrición.
 */
export async function procuraDatos(page, maxResults, timeoutMs, { onResult } = {}) {
  console.log('procuraDatos: agardando resultados orgánicos...');
  // Con módulos coma "Escolma xerada por IA" ou "Máis preguntas", Google pinta antes
  // o contedor baleiro que o seu contido; agardar só pola existencia de div#search
  // pode facer que a extracción se adiante e atope 0 candidatos. Ademais, priorizamos
  // div#rso (só resultados orgánicos "clásicos") fronte a div#search, que tamén inclúe
  // módulos coma a Escolma xerada por IA que de momento non queremos gardar.
  await page
    .waitForFunction(
      () => {
        const container = document.querySelector('div#rso') ?? document.querySelector('div#search');
        return !!container && container.querySelectorAll('a h3').length > 0;
      },
      { timeout: timeoutMs }
    )
    .catch(() => {
      console.warn('procuraDatos: non se atoparon resultados orgánicos no tempo especificado.');
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

  for (const [index, anchor] of anchors.entries()) {
    if (results.length >= maxResults) {
      await anchor.dispose().catch(() => {});
      skippedByLimit++;
      continue;
    }

    const entry = await procesaResultadoLigazon(anchor);
    await anchor.dispose().catch(() => {});

    if (!entry || entry.discardReason || !entry.link || !entry.titol) {
      discardedWithoutData++;
      const reason = entry?.discardReason ?? 'entrada_nula_ou_incompleta';
      const hrefPreview = entry?.href ?? entry?.link ?? '(sen href)';
      const titolPreview = entry?.titol ?? '(sen título)';
      console.warn(
        `Descartado candidato #${index + 1}: motivo=${reason}; href=${hrefPreview}; título=${titolPreview}`
      );
      continue;
    }

    const resolvedUrl = await resolveResultUrl(entry.link);
    if (!resolvedUrl) {
      discardedWithoutData++;
      console.warn(
        `Descartado candidato #${index + 1}: non se puido resolver a URL real; href=${entry.link}; título=${entry.titol}`
      );
      continue;
    }

    if (seenUrls.has(resolvedUrl)) {
      duplicateCount++;
      console.warn(
        `Descartado candidato #${index + 1} por duplicado: href=${resolvedUrl}; título=${entry.titol}`
      );
      continue;
    }

    const normalizedResult = {
      title: entry.titol,
      url: resolvedUrl,
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

    seenUrls.add(resolvedUrl);

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
  // Priorizamos div#rso (resultados orgánicos "clásicos") fronte a div#search, que
  // tamén pode incluír módulos coma a Escolma xerada por IA que de momento non
  // queremos gardar. Se non existe div#rso, recorremos a div#search coma antes.
  const rsoHandle = await page.$('div#rso').catch((error) => {
    console.warn('recuperaLigazonsResultados: erro comprobando div#rso; asúmese que non existe.', error);
    return null;
  });
  const hasRso = !!rsoHandle;
  await rsoHandle?.dispose().catch(() => {});
  const containerSelector = hasRso ? 'div#rso' : 'div#search';
  console.log(`recuperaLigazonsResultados: usando contedor "${containerSelector}".`);

  if (page && typeof page.$x === 'function') {
    try {
      const xpathResults = await page.$x(`//div[@id="${hasRso ? 'rso' : 'search'}"]//a[h3]`);
      console.log(`recuperaLigazonsResultados: XPath trobou ${xpathResults.length} resultados.`);
      return xpathResults;
    } catch (error) {
      console.warn('Non se puido obter os resultados con XPath; empregarase o fallback CSS.', error);
    }
  }

  const anchors = [];
  const candidates = await page.$$(`${containerSelector} a`).catch((error) => {
    console.warn(`recuperaLigazonsResultados: erro consultando "${containerSelector} a".`, error);
    return [];
  });
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

const GOOGLE_BLOCK_URL_TOKENS = ['/sorry/', '/interstitial/'];

const GOOGLE_BLOCK_CONTENT_TOKENS = [
  // Marcado técnico do captcha, independente do idioma.
  'g-recaptcha',
  'captcha-form',
  'recaptcha__enterprise.js',
  'solvesimplechallenge',
  // Galego
  'detectamos un tráfico inusual',
  'resolve o captcha para continuar',
  // Castelán (fallback habitual de Google cando non hai tradución completa)
  'hemos detectado un tráfico inusual',
  'estas solicitudes parecen generadas por ordenador',
  // Catalán
  'els nostres sistemes han detectat un trànsit inusual',
  'resoleu el captcha',
  // Inglés
  'unusual traffic',
  "verify you're not a robot",
];

/**
 * Detecta se Google amosou un captcha ou unha páxina interticial de bloqueo
 * en lugar dos resultados de procura habituais.
 */
function detectaBlocGoogle({ url, content } = {}) {
  const normalizedUrl = (url ?? '').toLowerCase();
  if (GOOGLE_BLOCK_URL_TOKENS.some((token) => normalizedUrl.includes(token))) {
    return true;
  }

  if (!content) {
    return false;
  }

  const normalizedContent = content.toLowerCase();
  return GOOGLE_BLOCK_CONTENT_TOKENS.some((token) => normalizedContent.includes(token));
}

/**
 * Erro específico para cando Google bloquea o sensor cun captcha/interstitial.
 * Permítelle ao chamador (crawler.js) saír de xeito limpo sen tratalo como un fallo.
 */
export class GoogleBlockedError extends Error {
  constructor(message, { url } = {}) {
    super(message);
    this.name = 'GoogleBlockedError';
    this.googleUrl = url ?? null;
  }
}

async function assertNonBloqueadoPorGoogle(page, content = null) {
  const currentUrl = page.url();
  const resolvedContent = content ?? (await page.content().catch(() => null));

  if (detectaBlocGoogle({ url: currentUrl, content: resolvedContent })) {
    throw new GoogleBlockedError(
      `Google detectou o sensor e amosou un captcha/interstitial (${currentUrl}).`,
      { url: currentUrl }
    );
  }
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

  if (detectaBlocGoogle({ url: page.url() })) {
    throw new GoogleBlockedError(
      `Google detectou o sensor ao cargar a páxina inicial (${page.url()}).`,
      { url: page.url() }
    );
  }

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
  const { content: paxinaContent } = await capturaContextoResultados(page, query);
  await assertNonBloqueadoPorGoogle(page, paxinaContent);

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
 * Se se descarta, devolve discardReason + href/titol para diagnóstico.
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

      const discard = (reason, extra = {}) => ({
        discardReason: reason,
        link: null,
        titol: extra.titol ?? null,
        href: extra.href ?? null,
        description: extra.description ?? null,
      });

      const evaluateNode = (context, xpath) => {
        try {
          return document
            .evaluate(xpath, context, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null)
            .singleNodeValue;
        } catch {
          return null;
        }
      };

      const rawHref = node.getAttribute('href') ?? null;
      const h3 = node.querySelector('h3');
      if (!h3) {
        return discard('sen_h3', { href: rawHref });
      }

      const titol = normalize(h3.textContent ?? '');

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
        return discard('modulo_mais_preguntes', {
          href: rawHref,
          titol,
          description: fallbackText,
        });
      }

      const link = rawHref;
      if (!link) {
        return discard('sen_href', { titol });
      }

      if (!titol) {
        return discard('titulo_baleiro', { href: link });
      }

      if (/[?¿]\s*$/.test(titol)) {
        return discard('titulo_pregunta', { href: link, titol });
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
    return { discardReason: 'erro_avaliacion', link: null, titol: null, href: null };
  }
}

