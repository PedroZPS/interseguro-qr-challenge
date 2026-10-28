import { generateKeyPairSync } from 'node:crypto';
import jwt from 'jsonwebtoken';

const newPair = () =>
  generateKeyPairSync('rsa', {
    modulusLength: 2048,
    publicKeyEncoding: { type: 'spki', format: 'pem' },
    privateKeyEncoding: { type: 'pkcs8', format: 'pem' },
  });

// Par que la app confía (Go firma con la privada; Node solo conoce la pública).
export const { publicKey, privateKey } = newPair();
// Par ajeno: tokens firmados con esta clave deben rechazarse.
export const attacker = newPair();

// Token RS256 válido por defecto; opts permite romperlo (expiresIn, algorithm...).
export const sign = (opts: jwt.SignOptions = {}, key: string = privateKey) =>
  jwt.sign({ sub: 'reviewer' }, key, { algorithm: 'RS256', expiresIn: '1h', ...opts });

export const bearer = (token = sign()) => ({ Authorization: `Bearer ${token}` });
