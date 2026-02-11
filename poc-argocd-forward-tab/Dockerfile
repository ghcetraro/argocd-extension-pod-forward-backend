# Dockerfile para construir extensiones de ArgoCD
FROM node:18-alpine

# Instalar git (necesario para argo-ui desde git)
RUN apk add --no-cache git

# Crear directorio de trabajo
WORKDIR /app

# Copiar archivos de configuración primero (para cache de Docker)
COPY package*.json ./
COPY tsconfig.json ./
COPY webpack.config.js ./

# Configurar npm para mejor manejo de errores y git
RUN npm config set fetch-retries 5 && \
    npm config set fetch-retry-mintimeout 20000 && \
    npm config set fetch-retry-maxtimeout 120000 && \
    npm config set git-tag-version false

# Instalar dependencias con mejor manejo de errores
RUN npm install --legacy-peer-deps --verbose 2>&1 | tee /tmp/npm-install.log || \
    (echo "=== Error en npm install ===" && \
     echo "Últimas líneas del log:" && \
     tail -50 /tmp/npm-install.log && \
     echo "=== Logs de npm ===" && \
     ls -la /root/.npm/_logs/ && \
     cat /root/.npm/_logs/*-debug-*.log 2>/dev/null | tail -100 && \
     exit 1)

# Copiar código fuente
COPY src/ ./src/

# Copiar manifest.json (debe existir en el contexto de build)
COPY manifest.json ./manifest.json

# Comando por defecto - ejecuta el build y crea el tar
CMD ["sh", "-c", "npm run build && if [ -f extension.tar ]; then echo '✅ Build exitoso: extension.tar creado'; ls -lh extension.tar; else echo '❌ Error: extension.tar no se generó'; exit 1; fi"]
