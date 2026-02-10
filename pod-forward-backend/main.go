package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

const (
	defaultPort = "8080"
)

// PortForwardSession mantiene una sesión de port-forward activa
type PortForwardSession struct {
	Namespace string
	Pod       string
	Port      int
	LocalPort int
	PF        *portforward.PortForwarder
	StopChan  chan struct{}
	mu        sync.Mutex
	LastUsed  time.Time
}

var (
	activeSessions = make(map[string]*PortForwardSession)
	sessionsMu     sync.RWMutex
	// Mapeo de puerto local a sessionKey para búsqueda rápida
	localPortToSession = make(map[int]string)
	localPortMu        sync.RWMutex
)

func main() {
	// Configurar cliente de Kubernetes
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Error al obtener configuración de Kubernetes: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error al crear cliente de Kubernetes: %v", err)
	}

	// Handler para el endpoint de port-forward
	// Manejar tanto /forward como /api/v1/extensions/pod-forward/forward
	http.HandleFunc("/forward", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[REQUEST] %s %s - Query: %s", r.Method, r.URL.Path, r.URL.RawQuery)
		handlePortForward(w, r, clientset, config)
	})
	
	// Manejar todas las rutas bajo /api/v1/extensions/pod-forward/
	// Esto permite que aplicaciones como Grafana funcionen correctamente con sus rutas
	http.HandleFunc("/api/v1/extensions/pod-forward/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[REQUEST] %s %s - Query: %s", r.Method, r.URL.Path, r.URL.RawQuery)
		handlePortForward(w, r, clientset, config)
	})

	// Handler de health check
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	
	// Handler raíz para debugging
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[REQUEST] %s %s - Query: %s", r.Method, r.URL.Path, r.URL.RawQuery)
		if r.URL.Path == "/" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "Pod Forward Backend - Path: %s\n", r.URL.Path)
			return
		}
		// Si la ruta contiene /forward o /api/v1/extensions/pod-forward/, intentar manejarla
		if strings.Contains(r.URL.Path, "/forward") || strings.HasPrefix(r.URL.Path, "/api/v1/extensions/pod-forward/") {
			handlePortForward(w, r, clientset, config)
			return
		}
		http.NotFound(w, r)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	log.Printf("Servidor iniciado en el puerto %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handlePortForward(w http.ResponseWriter, r *http.Request, clientset *kubernetes.Clientset, config *rest.Config) {
	log.Printf("[handlePortForward] Iniciando - Path: %s, Query: %s", r.URL.Path, r.URL.RawQuery)
	
	// Obtener parámetros de la query
	namespace := r.URL.Query().Get("namespace")
	pod := r.URL.Query().Get("pod")
	portStr := r.URL.Query().Get("port")
	
	log.Printf("[handlePortForward] Parámetros - namespace: %s, pod: %s, port: %s", namespace, pod, portStr)

	// Si faltan parámetros en la query, intentar obtenerlos de la sesión activa
	// Esto permite que las peticiones subsecuentes (como navegación en Grafana) funcionen
	if namespace == "" || pod == "" || portStr == "" {
		// Buscar una sesión activa
		// Si hay múltiples sesiones, usar la más reciente (LastUsed más reciente)
		sessionsMu.RLock()
		var activeSession *PortForwardSession
		var mostRecentTime time.Time
		for _, sess := range activeSessions {
			sess.mu.Lock()
			if sess.PF != nil && sess.LastUsed.After(mostRecentTime) {
				mostRecentTime = sess.LastUsed
				activeSession = sess
			}
			sess.mu.Unlock()
		}
		sessionsMu.RUnlock()
		
		if activeSession != nil {
			// Usar la sesión activa más reciente
			activeSession.mu.Lock()
			activeSession.LastUsed = time.Now()
			localPort := activeSession.LocalPort
			activeSession.mu.Unlock()
			
			log.Printf("[handlePortForward] Usando sesión activa - namespace: %s, pod: %s, port: %d, localPort: %d", 
				activeSession.Namespace, activeSession.Pod, activeSession.Port, localPort)
			
			// Proxear directamente al pod
			proxyHTTP(w, r, localPort)
			return
		}
		
		// Si faltan parámetros y no hay sesión activa, servir una página HTML simple
		if (r.URL.Path == "/forward" || strings.HasPrefix(r.URL.Path, "/api/v1/extensions/pod-forward/forward")) && r.Method == http.MethodGet {
			serveForwardPage(w, r)
			return
		}
		
		log.Printf("[handlePortForward] No hay sesión activa y faltan parámetros - Path: %s", r.URL.Path)
		http.Error(w, "Faltan parámetros requeridos: namespace, pod, port. No hay sesión activa.", http.StatusBadRequest)
		return
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("Puerto inválido: %s", portStr), http.StatusBadRequest)
		return
	}

	// Crear clave única para la sesión
	sessionKey := fmt.Sprintf("%s/%s:%d", namespace, pod, port)

	// Obtener o crear sesión de port-forward
	session, err := getOrCreateSession(sessionKey, namespace, pod, port, clientset, config)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error al crear port-forward: %v", err), http.StatusInternalServerError)
		return
	}

	// Actualizar último uso
	session.mu.Lock()
	session.LastUsed = time.Now()
	localPort := session.LocalPort
	session.mu.Unlock()

	// Proxear todas las peticiones al pod
	proxyHTTP(w, r, localPort)
}

func getOrCreateSession(sessionKey, namespace, pod string, port int, clientset *kubernetes.Clientset, config *rest.Config) (*PortForwardSession, error) {
	sessionsMu.RLock()
	session, exists := activeSessions[sessionKey]
	sessionsMu.RUnlock()

	if exists {
		// Verificar que la sesión sigue activa
		session.mu.Lock()
		if session.PF != nil {
			session.LastUsed = time.Now()
			session.mu.Unlock()
			return session, nil
		}
		session.mu.Unlock()
	}

	// Verificar que el pod existe
	_, err := clientset.CoreV1().Pods(namespace).Get(context.TODO(), pod, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error al obtener pod: %v", err)
	}

	// Crear nueva sesión
	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(pod).
		SubResource("portforward")

	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return nil, fmt.Errorf("error al configurar transport: %v", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, req.URL())

	stopChan := make(chan struct{}, 1)
	readyChan := make(chan struct{}, 1)

	// Crear el port-forward
	ports := []string{fmt.Sprintf("0:%d", port)}
	pf, err := portforward.New(dialer, ports, stopChan, readyChan, io.Discard, io.Discard)
	if err != nil {
		return nil, fmt.Errorf("error al crear port-forward: %v", err)
	}

	// Iniciar el port-forward en una goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- pf.ForwardPorts()
	}()

	// Esperar a que el port-forward esté listo
	select {
	case <-readyChan:
		// Port-forward listo
	case err := <-errChan:
		if err != nil {
			return nil, fmt.Errorf("error al iniciar port-forward: %v", err)
		}
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("timeout al iniciar port-forward")
	}

	// Obtener el puerto local asignado
	forwardedPorts, err := pf.GetPorts()
	if err != nil || len(forwardedPorts) == 0 {
		return nil, fmt.Errorf("error al obtener puerto local")
	}

	localPort := int(forwardedPorts[0].Local)

	session = &PortForwardSession{
		Namespace: namespace,
		Pod:       pod,
		Port:      port,
		LocalPort: localPort,
		PF:        pf,
		StopChan:  stopChan,
		LastUsed:  time.Now(),
	}

	sessionsMu.Lock()
	activeSessions[sessionKey] = session
	sessionsMu.Unlock()
	
	// Registrar el mapeo de puerto local a sessionKey
	localPortMu.Lock()
	localPortToSession[localPort] = sessionKey
	localPortMu.Unlock()

	// Limpiar sesión cuando termine
	go func() {
		<-errChan
		sessionsMu.Lock()
		delete(activeSessions, sessionKey)
		sessionsMu.Unlock()
		
		localPortMu.Lock()
		delete(localPortToSession, localPort)
		localPortMu.Unlock()
	}()

	return session, nil
}

func serveForwardPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>Port Forward</title>
    <meta charset="utf-8">
</head>
<body>
    <h1>Port Forward Activo</h1>
    <p>El port-forward está activo. Puedes acceder a la aplicación del pod directamente.</p>
    <p>Parámetros: namespace=%s, pod=%s, port=%s</p>
</body>
</html>`, r.URL.Query().Get("namespace"), r.URL.Query().Get("pod"), r.URL.Query().Get("port"))
}

func proxyHTTP(w http.ResponseWriter, r *http.Request, localPort int) {
	// Construir la URL del pod local
	// Remover el prefijo /api/v1/extensions/pod-forward/ de la ruta
	path := r.URL.Path
	
	// Si la ruta es /forward o /api/v1/extensions/pod-forward/forward, usar la raíz del pod
	if path == "/forward" || path == "/api/v1/extensions/pod-forward/forward" {
		path = "/"
	} else if strings.HasPrefix(path, "/api/v1/extensions/pod-forward/") {
		// Remover el prefijo /api/v1/extensions/pod-forward/ para obtener la ruta real
		path = strings.TrimPrefix(path, "/api/v1/extensions/pod-forward")
		if path == "" {
			path = "/"
		}
	}
	
	targetURL := fmt.Sprintf("http://localhost:%d%s", localPort, path)
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}
	
	log.Printf("[proxyHTTP] Proxying %s %s -> http://localhost:%d%s", r.Method, r.URL.Path, localPort, path)

	// Crear la petición al pod
	req, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error al crear petición: %v", err), http.StatusInternalServerError)
		return
	}

	// Copiar headers importantes (excluir algunos que pueden causar problemas)
	for key, values := range r.Header {
		// Excluir headers de conexión y host
		if key == "Connection" || key == "Upgrade" || key == "Host" {
			continue
		}
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	// Realizar la petición
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error al realizar petición: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copiar headers de respuesta (excluir algunos)
	// Primero, buscar y modificar el header Location si existe
	log.Printf("[proxyHTTP] Status Code: %d, Headers recibidos: %v", resp.StatusCode, resp.Header)
	locationHeader := resp.Header.Get("Location")
	log.Printf("[proxyHTTP] Location header obtenido: '%s'", locationHeader)
	if locationHeader != "" {
		// Si es un redirect relativo o absoluto, convertirlo a la ruta del proxy
		location := locationHeader
		if strings.HasPrefix(location, "/") {
			// Redirect relativo: agregar el prefijo del proxy
			location = "/api/v1/extensions/pod-forward" + location
		} else if strings.HasPrefix(location, "http://") || strings.HasPrefix(location, "https://") {
			// Redirect absoluto: extraer el path y agregar el prefijo del proxy
			// Parsear la URL
			parsedURL, err := url.Parse(location)
			if err == nil {
				location = "/api/v1/extensions/pod-forward" + parsedURL.Path
				if parsedURL.RawQuery != "" {
					location += "?" + parsedURL.RawQuery
				}
			}
		}
		// IMPORTANTE: Usar Set en lugar de Add para Location (solo debe haber uno)
		w.Header().Set("Location", location)
		log.Printf("[proxyHTTP] Redirect modificado: %s -> %s (Status: %d)", locationHeader, location, resp.StatusCode)
	} else {
		log.Printf("[proxyHTTP] No se encontró header Location en la respuesta")
	}
	
	for key, values := range resp.Header {
		// Excluir headers de conexión y Location (ya lo manejamos arriba)
		if key == "Connection" || key == "Upgrade" || key == "Location" {
			continue
		}
		
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	log.Printf("[proxyHTTP] Respondiendo con Status: %d, Headers: %v", resp.StatusCode, w.Header())
	w.WriteHeader(resp.StatusCode)

	// Copiar el cuerpo de la respuesta
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		log.Printf("Error al copiar respuesta: %v", err)
	}
}
