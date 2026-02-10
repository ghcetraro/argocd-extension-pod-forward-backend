# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copiar go mod files
COPY go.mod go.sum* ./

# Descargar dependencias y verificar
RUN go mod download && go mod verify

# Copiar código fuente
COPY . .

# Construir la aplicación
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o pod-forward-backend .

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copiar el binario desde el builder
COPY --from=builder /app/pod-forward-backend .

# Exponer puerto
EXPOSE 8080

# Ejecutar la aplicación
CMD ["./pod-forward-backend"]
