import os from 'node:os';

/**
 * Devolve os primeiros 5 caracteres do nome do host do sistema.
 * Se hai algún erro durante a lectura, devolve null.
 */
export function nomSensor() {
  try {
    const envSensor =
      process.env.SENSOR_NAME ??
      process.env.SENSOR ??
      process.env.CRAWLER_SENSOR ??
      process.env.GAL_SENSOR ??
      null;
    if (typeof envSensor === 'string' && envSensor.trim()) {
      return envSensor.trim();
    }
    const hostname = os.hostname();
    if (typeof hostname !== 'string' || hostname.length === 0) {
      return null;
    }
    return hostname.slice(0, 5);
  } catch (error) {
    console.error('Erro obtendo o nome do sensor:', error);
    return null;
  }
}

