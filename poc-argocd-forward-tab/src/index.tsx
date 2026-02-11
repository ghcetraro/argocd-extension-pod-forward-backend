import * as React from "react";

// Componente de la solapa FORWARD
// Según la documentación, el componente recibe: application, resource, tree
const ForwardTab: React.FC<{ 
  application?: any; 
  resource: any; 
  tree?: any;
}> = ({ resource, application, tree }) => {
  const [podName, setPodName] = React.useState<string>("");
  const [namespace, setNamespace] = React.useState<string>("");
  const [port, setPort] = React.useState<string>("8080");
  const [forwardUrl, setForwardUrl] = React.useState<string>("");

  React.useEffect(() => {
    if (resource) {
      setPodName(resource.metadata?.name || "");
      setNamespace(resource.metadata?.namespace || "default");
      
      // Intentar obtener el puerto del contenedor
      const containerPort = resource.spec?.containers?.[0]?.ports?.[0]?.containerPort;
      if (containerPort) {
        setPort(containerPort.toString());
      }
    }
  }, [resource]);

  const handlePortForward = () => {
    if (!podName || !namespace) {
      alert("No se pudo obtener la información del pod");
      return;
    }

    // Construir la URL del port-forward
    // Si estamos en el puerto 9090, usar el puerto 443 (HTTPS estándar) para el backend
    // El IngressRoute está configurado para websecure (puerto 443)
    let baseUrl = window.location.origin.replace(/\/$/, '');
    
    // Si la URL contiene el puerto 9090, cambiarlo a 443 para usar el IngressRoute
    if (baseUrl.includes(':9090')) {
      baseUrl = baseUrl.replace(':9090', '');
    }
    
    const url = `${baseUrl}/api/v1/extensions/pod-forward/forward?namespace=${namespace}&pod=${podName}&port=${port}`;
    setForwardUrl(url);
    
    // Abrir en nueva pestaña
    // El IngressRoute de Traefik enrutará esto al backend
    window.open(url, '_blank', 'noopener,noreferrer');
  };

  return (
    <div style={{ padding: "20px" }}>
      <h2>🔗 Port Forward</h2>
      <div style={{ marginBottom: "20px" }}>
        <p><strong>Pod:</strong> {podName || "N/A"}</p>
        <p><strong>Namespace:</strong> {namespace || "N/A"}</p>
      </div>
      
      <div style={{ marginBottom: "20px" }}>
        <label style={{ display: "block", marginBottom: "8px" }}>
          <strong>Puerto:</strong>
        </label>
        <input
          type="text"
          value={port}
          onChange={(e) => setPort(e.target.value)}
          placeholder="8080"
          style={{
            padding: "8px",
            width: "200px",
            border: "1px solid #ccc",
            borderRadius: "4px"
          }}
        />
      </div>

      <button
        onClick={handlePortForward}
        style={{
          padding: "10px 20px",
          backgroundColor: "#0DADEA",
          color: "white",
          border: "none",
          borderRadius: "4px",
          cursor: "pointer",
          fontSize: "14px",
          fontWeight: "bold"
        }}
      >
        Iniciar Port Forward
      </button>

      {forwardUrl && (
        <div style={{ marginTop: "20px", padding: "10px", backgroundColor: "#f5f5f5", borderRadius: "4px" }}>
          <p><strong>URL del Port Forward:</strong></p>
          <code style={{ wordBreak: "break-all" }}>{forwardUrl}</code>
        </div>
      )}

      <div style={{ marginTop: "30px", padding: "15px", backgroundColor: "#e8f4f8", borderRadius: "4px" }}>
        <h3>Instrucciones:</h3>
        <ol>
          <li>Ingresa el puerto del pod al que deseas hacer port-forward</li>
          <li>Haz clic en "Iniciar Port Forward"</li>
          <li>Se abrirá una nueva pestaña con la conexión establecida</li>
        </ol>
      </div>
    </div>
  );
};

// Variable para evitar registros duplicados
let extensionRegistered = false;

// Registrar la extensión como una solapa
// Usar una función que se ejecute cuando el DOM esté listo y extensionsAPI esté disponible
const registerExtension = () => {
  // Evitar registros duplicados
  if (extensionRegistered) {
    return;
  }

  if (typeof window === "undefined") {
    return;
  }

  // Intentar obtener extensionsAPI de diferentes formas
  const extensionsAPI = (window as any).extensionsAPI || 
                        (window as any).__ARGOCD_EXTENSIONS_API__ ||
                        (window as any).argocd?.extensionsAPI;

  if (extensionsAPI && typeof extensionsAPI.registerResourceExtension === "function") {
    try {
      // Según la documentación: registerResourceExtension(component, group, kind, tabTitle, opts?)
      // group y kind son patrones glob
      // Para Pods (recursos core de Kubernetes): group = "" (vacío) o "**" para todos los grupos
      // kind = "Pod" (exacto) o "*" para todos los tipos
      extensionsAPI.registerResourceExtension(
        ForwardTab,           // Componente React
        "",                    // group: "" para recursos core de Kubernetes (Pods, Services, etc.)
        "Pod",                 // kind: tipo de recurso exacto
        "FORWARD",             // tabTitle: título de la solapa
        {                      // opts: opciones adicionales
          icon: "fa fa-link"   // icono de FontAwesome
        }
      );
      extensionRegistered = true; // Marcar como registrado
      console.log("[ForwardTab] ✅ Extensión registrada correctamente");
    } catch (error) {
      console.error("[ForwardTab] ❌ Error al intentar registrar extensión:", error);
    }
  } else {
    // Si extensionsAPI no está disponible, intentar de nuevo después de un delay
    // Solo mostrar el warning una vez
    if (!extensionRegistered) {
      console.warn("[ForwardTab] ⚠️ extensionsAPI no está disponible, reintentando...");
    }
  }
};

// Ejecutar el registro inmediatamente cuando el script se carga
// ArgoCD carga las extensiones antes de que el DOM esté listo
if (typeof window !== "undefined") {
  // Intentar registrar inmediatamente
  registerExtension();
  
  // También intentar cuando el DOM esté listo (solo si aún no se registró)
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", () => {
      if (!extensionRegistered) {
        registerExtension();
      }
    });
  }
  
  // Reintentar periódicamente por si extensionsAPI se carga más tarde (solo si aún no se registró)
  let retryCount = 0;
  const maxRetries = 10;
  const retryInterval = setInterval(() => {
    if (extensionRegistered) {
      clearInterval(retryInterval);
      return;
    }
    
    retryCount++;
    if (retryCount >= maxRetries) {
      clearInterval(retryInterval);
      console.warn("[ForwardTab] ⚠️ No se pudo registrar la extensión después de múltiples intentos");
      console.warn("[ForwardTab] extensionsAPI disponible:", !!(window as any).extensionsAPI);
      return;
    }
    
    registerExtension();
  }, 500);
}

export default ForwardTab;
