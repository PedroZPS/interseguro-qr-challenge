import { createApp } from './app.ts';

// JWT_PUBLIC_KEY: clave pública PEM codificada en base64 (las variables de
// entorno llevan mal los saltos de línea).
const encoded = process.env.JWT_PUBLIC_KEY;
if (!encoded) {
  console.error('Falta JWT_PUBLIC_KEY');
  process.exit(1);
}
const publicKey = Buffer.from(encoded, 'base64').toString('utf8');

const port = process.env.PORT || 3000;
createApp({ publicKey }).listen(port, () => console.log(`stats-api escuchando en :${port}`));
