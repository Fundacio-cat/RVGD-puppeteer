import { Client } from 'pg';

/**
 * Utilidades de persistencia para traballar cunha base de datos Postgres.
 * Encárgase de validar nomes de táboa, crear o esquema e facer upserts dos resultados.
 */

export const DEFAULT_TABLE_NAME = 'google_results';
const TABLE_NAME_PATTERN = /^[a-zA-Z_][a-zA-Z0-9_]*$/;

/**
 * Verifica que o nome da táboa cumpra as restricións básicas de Postgres.
 */
export function validateTableName(tableName) {
  if (!TABLE_NAME_PATTERN.test(tableName)) {
    throw new Error(
      `Nome de táboa Postgres non válido: "${tableName}". Só se permiten letras, números e guións baixos, e non pode comezar cun número.`
    );
  }
  return tableName;
}

function escapeSqlLiteral(value) {
  return String(value).replace(/'/g, "''");
}

function formatSqlValue(value) {
  if (value === null || value === undefined) {
    return 'NULL';
  }

  if (value instanceof Date) {
    return `'${value.toISOString()}'`;
  }

  if (typeof value === 'number' && Number.isFinite(value)) {
    return String(value);
  }

  if (typeof value === 'boolean') {
    return value ? 'TRUE' : 'FALSE';
  }

  return `'${escapeSqlLiteral(value)}'`;
}

/**
 * Establece unha conexión a Postgres empregando a cadea proporcionada.
 */
export async function ensureDatabaseConnection(connectionString) {
  if (!connectionString) {
    throw new Error('Cómpre proporcionar a cadea de conexión de Postgres (connectionString).');
  }

  const client = new Client({ connectionString });
  await client.connect();
  return client;
}

/**
 * Obtén o tamaño de xanela que se debe empregar (id, largura e altura).
 * Devolve { sizeId, width, height } ou valores nulos en caso de erro.
 */
export async function seleccionaMides({ connectionString, client } = {}) {
  let dbClient = client;
  let shouldClose = false;

  if (!dbClient) {
    if (!connectionString) {
      console.warn('seleccionaMides: non se proporcionou ningunha cadea de conexión.');
      return { sizeId: null, width: null, height: null };
    }

    dbClient = await ensureDatabaseConnection(connectionString);
    shouldClose = true;
  }

  try {
    const sizeIdResult = await dbClient.query('SELECT selecciona_medidas() AS medida_id;');
    const sizeId = sizeIdResult.rows?.[0]?.medida_id ?? null;

    if (sizeId === null) {
      console.warn('seleccionaMides: non se atopou ningún tamaño dispoñible.');
      return { sizeId: null, width: null, height: null };
    }

    const dimensionsResult = await dbClient.query(
      'SELECT anchura, altura FROM medidas WHERE medidaid = $1;',
      [sizeId]
    );
    const width = dimensionsResult.rows?.[0]?.anchura ?? null;
    const height = dimensionsResult.rows?.[0]?.altura ?? null;

    if (width === null || height === null) {
      console.warn(`seleccionaMides: non se atoparon dimensións para o tamaño ${sizeId}.`);
    }

    return { sizeId, width, height };
  } catch (error) {
    console.error('seleccionaMides: erro executando as consultas:', error);
    return { sizeId: null, width: null, height: null };
  } finally {
    if (shouldClose && dbClient) {
      await dbClient.end().catch((closeError) => {
        console.error('seleccionaMides: erro pechando a conexión:', closeError);
      });
    }
  }
}

/**
 * Obtén o seguinte identificador de procura e a consulta asociada para un sensor concreto.
 * Devolve { searchId, query } ou { searchId: null, query: null } en caso de erro.
 */
export async function seguentCerca(sensor, { connectionString, client } = {}) {
  if (!sensor || typeof sensor !== 'string') {
    console.warn('seguentCerca: cómpre proporcionar o nome do sensor como cadea.');
    return { searchId: null, query: null };
  }

  let dbClient = client;
  let shouldClose = false;

  if (!dbClient) {
    if (!connectionString) {
      console.warn('seguentCerca: non se proporcionou ningunha cadea de conexión.');
      return { searchId: null, query: null };
    }

    dbClient = await ensureDatabaseConnection(connectionString);
    shouldClose = true;
  }

  try {
    const searchIdResult = await dbClient.query('SELECT seguinte_busca_filtrada($1) AS busca_id;', [sensor]);
    const searchId = searchIdResult.rows?.[0]?.busca_id ?? null;

    if (searchId === null) {
      console.warn(`seguentCerca: non se atopou ningunha procura pendente para o sensor "${sensor}".`);
      return { searchId: null, query: null };
    }

    const queryResult = await dbClient.query('SELECT consulta FROM buscas WHERE buscaid = $1;', [searchId]);
    const query = queryResult.rows?.[0]?.consulta ?? null;

    if (query === null) {
      console.warn(`seguentCerca: non se atopou a consulta para o ID de procura ${searchId}.`);
    }

    return { searchId, query };
  } catch (error) {
    console.error('seguentCerca: erro executando as consultas:', error);
    return { searchId: null, query: null };
  } finally {
    if (shouldClose && dbClient) {
      await dbClient.end().catch((closeError) => {
        console.error('seguentCerca: erro pechando a conexión:', closeError);
      });
    }
  }
}

/**
 * Insire un único rexistro na táboa `resultados`.
 */
export async function guardaDb({
  connectionString,
  client,
  sensor,
  navegadorId,
  cercadorId,
  searchId,
  posicio,
  titol,
  url,
  descripcio,
  mida,
}) {
  let dbClient = client;
  let shouldClose = false;

  if (!dbClient) {
    if (!connectionString) {
      console.error('guardaDb: ERRO - cómpre proporcionar unha cadea de conexión ou un cliente existente.');
      throw new Error('guardaDb: cómpre proporcionar unha cadea de conexión ou un cliente existente.');
    }

    dbClient = await ensureDatabaseConnection(connectionString);
    shouldClose = true;
  }

  try {
    const now = new Date();

    const insertQuery = `
      INSERT INTO resultados (sensor, hora, navegador, buscador, posicion, titulo, url, descripcion, lingua, busca, medida)
      VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
    `;

    const values = [
      sensor ?? null,
      now,
      navegadorId ?? null,
      cercadorId ?? null,
      posicio ?? null,
      titol ?? null,
      url ?? null,
      descripcio ?? null,
      null, // lingua sempre null
      searchId ?? null,
      mida ?? null,
    ];

    let insertError = null;

    try {
      await dbClient.query(insertQuery, values);
    } catch (error) {
      insertError = error;
    } finally {
      if (shouldClose && dbClient) {
        try {
          await dbClient.end();
        } catch (closeError) {
          console.error('guardaDb: erro pechando a conexión:', closeError);
        }
      }
    }

    if (insertError) {
      console.error('guardaDb: Erro detallado ao inserir na base de datos:', insertError);
      throw new Error('guardaDb: Non se puido inserir o resultado na base de datos.');
    }
  } catch (error) {
    console.error('guardaDb: Non se puido inserir o resultado na base de datos.', error);
    throw error;
  }
}