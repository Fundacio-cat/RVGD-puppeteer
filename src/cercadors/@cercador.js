import { ensureDatabaseConnection } from '../database.js';

/**
 * Devolve o identificador do buscador seleccionado polo back-end.
 *
 * @param {string} connectionString Cadea de conexión de Postgres.
 * @param {{ client?: import('pg').Client }} [options] Cliente reutilizable opcional.
 * @returns {Promise<number|null>} Identificador do buscador ou null se hai erro.
 */

export async function seleccionaCercador({ connectionString, client } = {}) {
  if (!connectionString && !client) {
    console.warn("seleccionaCercador: non se proporcionou ningunha cadea de conexión.");
    return 1;
  }

  let dbClient = client;
  let shouldClose = false;

  if (!dbClient) {
    dbClient = await ensureDatabaseConnection(connectionString);
    shouldClose = true;
  }

  try {
    const result = await dbClient.query('SELECT selecciona_buscador() AS buscador_id;');
    const cercadorId = result.rows?.[0]?.buscador_id;
    return typeof cercadorId === 'number' ? cercadorId : null;
  } catch (error) {
    console.error('seleccionaCercador: erro executando a consulta:', error);
    return null;
  } finally {
    if (shouldClose && dbClient) {
      await dbClient.end().catch((closeError) => {
        console.error('seleccionaCercador: erro pechando a conexión:', closeError);
      });
    }
  }
}

