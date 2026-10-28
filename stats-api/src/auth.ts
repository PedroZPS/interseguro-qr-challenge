import jwt from 'jsonwebtoken';
import type { Request, Response, NextFunction, RequestHandler } from 'express';

// Middleware que exige un JWT RS256 firmado por Go (se valida con la clave pública).
// Se fija algorithms: ['RS256'] para rechazar "none" y el ataque de confusión de
// algoritmo (HS256 firmado con la clave pública como secreto).
export function requireAuth(publicKey: string): RequestHandler {
  return (req: Request, res: Response, next: NextFunction) => {
    const [scheme, token] = (req.get('Authorization') ?? '').split(' ');
    if (scheme !== 'Bearer' || !token) {
      return res.status(401).json({ error: 'token requerido' });
    }
    try {
      const claims = jwt.verify(token, publicKey, { algorithms: ['RS256'] }) as jwt.JwtPayload;
      // verify solo comprueba exp si existe: un token que nunca caduca no se acepta.
      if (typeof claims.exp !== 'number') throw new Error('sin exp');
    } catch {
      // Mensaje fijo: no se explica por qué falló el token.
      return res.status(401).json({ error: 'token inválido' });
    }
    next();
  };
}
