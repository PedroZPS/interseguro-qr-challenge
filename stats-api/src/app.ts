import express from 'express';
import { computeStats } from './stats.ts';
import { requireAuth } from './auth.ts';
import type { Express, Request, Response, NextFunction } from 'express';

// Devuelve un mensaje de error si m no es una matriz rectangular de números
// finitos y no vacía; null si es válida.
function matrixError(name: string, m: unknown): string | null {
  if (!Array.isArray(m) || m.length === 0) return `${name} debe ser un arreglo de filas no vacío`;
  const cols = Array.isArray(m[0]) ? m[0].length : 0;
  if (cols === 0) return `${name} no puede tener filas vacías`;
  for (const row of m) {
    if (!Array.isArray(row) || row.length !== cols) return `${name} debe ser rectangular`;
    for (const x of row) {
      if (typeof x !== 'number' || !Number.isFinite(x)) return `${name} solo admite números finitos`;
    }
  }
  return null;
}

// app sin listen, para poder probarla con supertest.
// publicKey: clave pública PEM con la que se validan los JWT RS256 emitidos por Go.
export function createApp({ publicKey }: { publicKey?: string } = {}): Express {
  if (!publicKey) throw new Error('createApp: falta publicKey');
  const app = express();
  // 1mb sobra para dos matrices de 100x100 (el límite de la API Go).
  app.use(express.json({ limit: '1mb' }));

  app.get('/health', (_req: Request, res: Response) => res.json({ status: 'ok' }));

  app.post('/stats', requireAuth(publicKey), (req: Request, res: Response) => {
    const { q, r } = req.body ?? {};
    const error = matrixError('q', q) ?? matrixError('r', r);
    if (error) return res.status(400).json({ error });
    res.json(computeStats(q, r));
  });

  app.use((_req: Request, res: Response) => res.status(404).json({ error: 'ruta no encontrada' }));

  // Nunca se devuelve el stack: los errores 4xx del parser dan un mensaje fijo
  // y cualquier otro error es un 500 genérico.
  // eslint-disable-next-line no-unused-vars
  app.use((err: { status?: number }, _req: Request, res: Response, _next: NextFunction) => {
    if (err.status === 413) return res.status(413).json({ error: 'body demasiado grande' });
    if (err.status && err.status >= 400 && err.status < 500) return res.status(400).json({ error: 'JSON inválido' });
    console.error(err);
    res.status(500).json({ error: 'error interno' });
  });

  return app;
}
