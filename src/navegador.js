import { ensureDatabaseConnection } from './database.js';

/**
 * Obtén o identificador do navegador a empregar mediante a función
 * `selecciona_navegador()` na base de datos.
 *
 * Devolve:
 *   - 1 se o navegador seleccionado é Firefox.
 *   - 2 se o navegador seleccionado é Chrome.
 *   - null se hai algún erro ou non se puido determinar.
 *
 * @param {string} connectionString Cadea de conexión de Postgres.
 * @param {{ client?: import('pg').Client }} [options] Cliente reutilizable opcional.
 */

/*
// O código orixinal que consultaba a función `selecciona_navegador()` en Postgres:
export async function seleccionaNavegador({ connectionString, client } = {}) {
  if (!connectionString && !client) {
    console.warn("seleccionaNavegador: non se proporcionou ningunha cadea de conexión.");
    return 2;
  }

  let dbClient = client;
  let shouldClose = false;

  if (!dbClient) {
    dbClient = await ensureDatabaseConnection(connectionString);
    shouldClose = true;
  }

  try {
    const result = await dbClient.query('SELECT selecciona_navegador() AS navegador_id;');
    const navegadorId = result.rows?.[0]?.navegador_id;

    if (navegadorId === 1 || navegadorId === 2) {
      return navegadorId;
    }

    console.warn('seleccionaNavegador: valor descoñecido recibido:', navegadorId);
    return null;
  } catch (error) {
    console.error('seleccionaNavegador: erro executando a consulta:', error);
    return null;
  } finally {
    if (shouldClose && dbClient) {
      await dbClient.end().catch((closeError) => {
        console.error('seleccionaNavegador: erro pechando a conexión:', closeError);
      });
    }
  }
}
*/

// Devolve sempre 1 (Chrome)
export async function seleccionaNavegador() {
  return 1;
}
