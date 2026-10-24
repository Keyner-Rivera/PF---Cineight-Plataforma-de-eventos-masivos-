// Package handlers es la capa HTTP: decodifica solicitudes, llama a los servicios y serializa respuestas.
// No contiene reglas de negocio ni SQL.
package handlers

import (
	"errors"
	"log"
	"net/http"

	"github.com/cineight/backend/internal/domain"
	"github.com/cineight/backend/internal/services"
	"github.com/cineight/backend/pkg/httpx"
)

// Servicios que necesitan los handlers.
type Servicios struct {
	Asientos  *services.Asientos
	Reservas  *services.Reservas
	Cuentas   *services.CuentasClientes
	Cola      *services.ColaVirtual
	Panel     *services.Panel
	Seguridad *services.Seguridad
	Cartelera *services.Cartelera
	Butacas   *services.Butacas
}

// Limites de solicitudes por minuto y por IP.
type Limites struct {
	Sensible    int64 // reservas y cola virtual (RF-53)
	Interactivo int64 // clics en el mapa de butacas y verificación de captcha
}

// API agrupa los handlers.
type API struct {
	s       Servicios
	limites Limites
}

// NuevoRouter registra todas las rutas de la API y aplica CORS.
func NuevoRouter(s Servicios, limites Limites, origenes map[string]bool) http.Handler {
	a := &API{s: s, limites: limites}
	sensible := a.limitar("sensible", limites.Sensible)
	interactivo := a.limitar("interactivo", limites.Interactivo)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/salud", func(w http.ResponseWriter, r *http.Request) { httpx.JSON(w, 200, map[string]string{"estado": "ok"}) })

	// Catálogo y mapa de butacas en tiempo real
	mux.HandleFunc("GET /api/funciones", a.listarFunciones)
	mux.HandleFunc("GET /api/funciones/{id}/asientos", a.ocupacion)
	mux.HandleFunc("GET /api/ocupacion", a.ocupacionVigente)

	// Catálogo editable (agente operativo y administrador)
	mux.HandleFunc("GET /api/peliculas", a.listarPeliculas)
	mux.Handle("POST /api/peliculas", sensible(a.crearPelicula))
	mux.HandleFunc("GET /api/peliculas/{slug}/poster", a.poster)
	mux.Handle("POST /api/funciones", interactivo(a.programarFuncion))
	mux.HandleFunc("GET /api/funciones/{id}/stream", a.stream)
	mux.Handle("POST /api/funciones/{id}/asientos/{asiento}/bloqueo", interactivo(a.bloquear))
	mux.Handle("DELETE /api/funciones/{id}/bloqueos", interactivo(a.desbloquear))

	// Butacas dañadas o fuera de servicio (agente operativo y administrador)
	mux.HandleFunc("GET /api/salas/{sala}/fuera-servicio", a.listarFueraServicio)
	mux.Handle("POST /api/salas/{sala}/fuera-servicio/{asiento}", interactivo(a.retirarButaca))
	mux.Handle("DELETE /api/salas/{sala}/fuera-servicio/{asiento}", interactivo(a.habilitarButaca))

	// Compra
	mux.Handle("POST /api/reservas", sensible(a.crearReserva))
	mux.HandleFunc("POST /api/reservas/{id}/pagar", a.pagar)
	mux.HandleFunc("DELETE /api/reservas/{id}", a.cancelarReserva)

	// Cuentas y créditos
	mux.HandleFunc("POST /api/sesion", a.sesion)
	mux.HandleFunc("GET /api/usuarios/{id}/cuenta", a.cuenta)
	mux.HandleFunc("POST /api/creditos/trivia", a.trivia)

	// Operación
	mux.HandleFunc("POST /api/boletos/{token}/validar", a.validarBoleto)
	mux.Handle("POST /api/cola/{evento}", sensible(a.entrarCola))
	mux.HandleFunc("GET /api/cola/{evento}/{usuario}", a.posicionCola)
	mux.HandleFunc("DELETE /api/cola/{evento}/{usuario}", a.salirCola)
	mux.HandleFunc("GET /api/metricas", a.metricas)
	mux.HandleFunc("GET /api/clientes/historial", a.historialClientes)

	// Seguridad
	mux.Handle("POST /api/captcha/verificar", interactivo(a.verificarCaptcha))

	// Cualquier otra ruta: 404 en JSON.
	mux.HandleFunc("/", httpx.NoEncontrado)

	// Orden de los middlewares (de afuera hacia adentro): CORS → recuperación de panics → cabeceras JSON → rutas.
	return httpx.CORS(origenes, httpx.Recuperar(httpx.CabecerasJSON(mux)))
}

// limitar envuelve un handler con el límite de solicitudes por IP de su grupo (RF-53).
func (a *API) limitar(grupo string, limite int64) func(http.HandlerFunc) http.Handler {
	return func(next http.HandlerFunc) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !a.s.Seguridad.Permitir(r.Context(), grupo, httpx.IP(r), r.URL.Path, limite) {
				httpx.Error(w, http.StatusTooManyRequests, "Demasiadas solicitudes. Espera un momento.")
				return
			}
			next(w, r)
		})
	}
}

// fallar traduce un error de servicio a una respuesta HTTP. Si Redis no responde, 503 con un mensaje controlado;
// los demás errores no previstos se registran y responden 500.
func fallar(w http.ResponseWriter, err error) {
	var e *domain.Error
	if errors.As(err, &e) {
		httpx.Error(w, e.Estado, e.Mensaje)
		return
	}
	if errors.Is(err, domain.ErrNoDisponible) {
		log.Printf("redis no disponible: %v", err)
		w.Header().Set("Retry-After", "5")
		httpx.Error(w, http.StatusServiceUnavailable, domain.MensajeNoDisponible)
		return
	}
	log.Printf("error interno: %v", err)
	httpx.Error(w, http.StatusInternalServerError, "Error interno del servidor.")
}
