# --- build: compila TypeScript a dist/ ---
FROM node:24-alpine AS build
# corepack trae pnpm en la versión fijada en package.json (packageManager)
RUN corepack enable
WORKDIR /app
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY tsconfig.json tsconfig.build.json ./
COPY src ./src
RUN pnpm build

# --- runtime: solo dependencias de producción y el JS compilado ---
FROM node:24-alpine
RUN corepack enable
WORKDIR /app
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile --prod
COPY --from=build /app/dist ./dist
# La imagen oficial ya trae el usuario "node" sin privilegios
USER node
ENV PORT=3000
EXPOSE 3000
CMD ["node", "dist/server.js"]
