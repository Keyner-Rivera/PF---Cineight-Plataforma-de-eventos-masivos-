// Package httpx reúne utilidades HTTP sin lógica de negocio: respuestas JSON, errores y CORS.
package httpx

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
)

// JSON escribe v como JSON con el código de estado indicado.
func JSON(w http.ResponseWriter, estado int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(estado)
	_ = json.NewEncoder(w).Encode(v)
}

// Error responde {"error": mensaje}, el formato que espera el frontend.
func Error(w http.ResponseWriter, estado int, mensaje string) {
	JSON(w, estado, map[string]string{"error": mensaje})
}

// EventoSSE escribe un evento de Server-Sent Events con v serializado en JSON ("event: …\ndata: …\n\n").
func EventoSSE(w io.Writer, evento string, v any) error {
	datos, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evento, datos)
	return err
}

// CabecerasJSON fija en toda respuesta Content-Type: application/json y X-Content-Type-Options: nosniff.
// Los handlers que entregan otro contenido (pósters, stream SSE) reemplazan el Content-Type antes de escribir.
func CabecerasJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

// NoEncontrado responde 404 en JSON para rutas que no existen (en lugar del texto plano de net/http).
func NoEncontrado(w http.ResponseWriter, _ *http.Request) {
	Error(w, http.StatusNotFound, "Ruta no encontrada.")
}

// Leer decodifica el cuerpo JSON de la solicitud en destino (máximo 64 KB).
func Leer(r *http.Request, destino any) error {
	return LeerHasta(r, destino, 64<<10)
}

// LeerHasta decodifica el cuerpo JSON con un límite de tamaño propio (p. ej. solicitudes con imágenes).
func LeerHasta(r *http.Request, destino any, maxBytes int64) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBytes)).Decode(destino)
}

// ConfiarProxy indica que la API corre detrás del proxy de un hosting (Render, Railway, Fly…): todas las
// conexiones llegan desde el proxy, así que la IP del cliente sale de las cabeceras que éste agrega.
// Sólo debe activarse detrás de un proxy: si no, un cliente podría falsear su IP con esas cabeceras.
var ConfiarProxy bool

// IP devuelve la dirección del cliente (sin puerto). Detrás de un proxy:
//   - CF-Connecting-IP si la trae: Render (y cualquier sitio detrás de Cloudflare) la fija con la IP real y reemplaza
//     la que mande el cliente. En Render, la última entrada de X-Forwarded-For es un proxy interno que rota entre
//     unas pocas IPs: limitar por ella haría que todos los clientes compartan el mismo límite por minuto.
//   - si no, la última entrada de X-Forwarded-For (la que agregó el proxy de confianza).
func IP(r *http.Request) string {
	if ConfiarProxy {
		if ip := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); ip != "" {
			return ip
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			partes := strings.Split(xff, ",")
			if ip := strings.TrimSpace(partes[len(partes)-1]); ip != "" {
				return ip
			}
		}
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// CORS aplica una lista blanca de orígenes ("*" permite cualquiera).
// Las solicitudes de un origen no permitido se atienden sin cabeceras CORS, así que el navegador las bloquea.
func CORS(origenes map[string]bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origen := r.Header.Get("Origin")
		w.Header().Add("Vary", "Origin")
		if origen != "" && (origenes[origen] || origenes["*"]) {
			w.Header().Set("Access-Control-Allow-Origin", origen)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Recuperar atrapa cualquier panic de un handler: lo registra con su traza y responde un error JSON controlado
// en lugar de cortar la conexión. http.ErrAbortHandler se relanza porque es la forma estándar de abortar una respuesta.
func Recuperar(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler {
				panic(v)
			}
			log.Printf("panic en %s %s: %v\n%s", r.Method, r.URL.Path, v, debug.Stack())
			Error(w, http.StatusInternalServerError, "Error interno del servidor.")
		}()
		next.ServeHTTP(w, r)
	})
}

// ListaOrigenes convierte "a,b,c" en un conjunto.
func ListaOrigenes(csv string) map[string]bool {
	out := map[string]bool{}
	for _, o := range strings.Split(csv, ",") {
		if o = strings.TrimSpace(o); o != "" {
			out[o] = true
		}
	}
	return out
}
