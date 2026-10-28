# Se construye desde la raíz del repo:  docker build -f qr-api/Dockerfile .
# (la imagen incluye la carpeta frontend/, que está fuera de qr-api/)

# Etapa 1: compilar un binario estático
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY qr-api/go.mod qr-api/go.sum ./
RUN go mod download
COPY qr-api/ .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# Etapa 2: imagen mínima sin shell, ejecutada como usuario no root
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
COPY frontend /frontend
ENV FRONTEND_DIR=/frontend
EXPOSE 8080
ENTRYPOINT ["/server"]
