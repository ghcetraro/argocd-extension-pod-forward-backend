# ArgoCD Forward Tab Extension

Extensión de ArgoCD que agrega una solapa "FORWARD" junto a "Terminal" para realizar port-forward a pods.

## Características

- Agrega una solapa "FORWARD" en la vista de recursos de tipo Pod
- Permite configurar el puerto para el port-forward
- Abre el port-forward en una nueva pestaña
- Interfaz simple y fácil de usar

## Desarrollo

### Construir la extensión

```bash
npm install --legacy-peer-deps
npm run build
```

Esto generará `extension.tar.gz` que contiene los recursos de la extensión.

### Construir con Docker

```bash
docker build -t argocd-forward-tab-builder .
docker run --rm -v $(pwd):/output argocd-forward-tab-builder
```

## Instalación

La extensión debe ser instalada en ArgoCD usando el extension-installer. Agrega la siguiente configuración en `values.yaml`:

```yaml
argo-cd:
  server:
    extensions:
      enabled: true
      extensionList:
        - name: extension-forward-tab
          env:
            - name: EXTENSION_URL
              value: https://github.com/tu-usuario/poc-argocd-forward-tab/releases/download/v0.1.0/extension.tar.gz
            - name: EXTENSION_CHECKSUM_URL
              value: https://github.com/tu-usuario/poc-argocd-forward-tab/releases/download/v0.1.0/extension_checksums.txt
```

## Requisitos

- ArgoCD 2.4+
- Backend de port-forward configurado (opcional, si se usa la funcionalidad de port-forward)
