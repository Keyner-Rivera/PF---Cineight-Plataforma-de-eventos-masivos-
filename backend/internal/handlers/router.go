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
	Auth      *services.Autenticacion
}

// Limites de solicitudes por minuto y por IP.
type Limites struct {
	Sensible    int64 // reservas y cola virtual (RF-53)
	Interactivo int64 // clics en el mapa de butacas y verificación de captcha
	Acceso      int64 // inicio de sesión, registro y cambio de contraseña (fuerza bruta)
}

// API agrupa los handlers.
type API struct {
	s       Servicios
	limites Limites
}

// NuevoRouter registra todas las rutas de la API con su control de acceso (RF-44) y aplica CORS y cabeceras de seguridad.
//
// Cada ruta declara quién puede usarla: pública (cartelera, ocupación, pósters), cliente (su cuenta, compras, cola),
// personal (agente o administrador: catálogo, butacas, validación), administrador (usuarios, clientes) y lectura de
// auditoría (administrador o auditor). El usuario siempre sale de la sesión, nunca del cuerpo ni de la URL.
func NuevoRouter(s Servicios, limites Limites, origenes map[string]bool) http.Handler {
	a := &API{s: s, limites: limites}
	sensible := a.limitar("sensible", limites.Sensible)
	interactivo := a.limitar("interactivo", limites.Interactivo)
	acceso := a.limitar("acceso", limites.Acceso)

	cualquiera := a.autenticar(false)
	conClaveTemporal := a.autenticar(true) // únicas rutas permitidas mientras la contraseña es temporal
	cliente := a.autenticar(false, domain.RolCliente)
	personal := a.autenticar(false, domain.RolAgente, domain.RolAdmin)
	admin := a.autenticar(false, domain.RolAdmin)
	auditoria := a.autenticar(false, domain.RolAdmin, domain.RolAuditor)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/salud", func(w http.ResponseWriter, r *http.Request) { httpx.JSON(w, 200, map[string]string{"estado": "ok"}) })

	// Sesión
	mux.Handle("POST /api/auth/registro", acceso(a.registro))
	mux.Handle("POST /api/auth/login", acceso(a.login))
	mux.HandleFunc("GET /api/auth/sesion", conClaveTemporal(a.miSesion))
	mux.HandleFunc("POST /api/auth/logout", conClaveTemporal(a.logout))
	mux.HandleFunc("POST /api/auth/logout-todas", cualquiera(a.logoutTodas))
	mux.Handle("POST /api/auth/clave", acceso(conClaveTemporal(a.cambiarClave)))
	mux.Handle("POST /api/auth/reconfirmar", acceso(admin(a.reconfirmar)))

	// Catálogo y mapa de butacas en tiempo real (públicos)
	mux.HandleFunc("GET /api/funciones", a.listarFunciones)
	mux.HandleFunc("GET /api/funciones/{id}/asientos", a.ocupacion)
	mux.HandleFunc("GET /api/ocupacion", a.ocupacionVigente)
	mux.HandleFunc("GET /api/funciones/{id}/stream", a.stream)
	mux.HandleFunc("GET /api/peliculas", a.listarPeliculas)
	mux.HandleFunc("GET /api/peliculas/{slug}/poster", a.poster)

	// Catálogo editable y butacas fuera de servicio (agente operativo y administrador)
	mux.Handle("POST /api/peliculas", sensible(personal(a.crearPelicula)))
	mux.Handle("POST /api/funciones", interactivo(personal(a.programarFuncion)))
	mux.HandleFunc("GET /api/salas/{sala}/fuera-servicio", personal(a.listarFueraServicio))
	mux.Handle("POST /api/salas/{sala}/fuera-servicio/{asiento}", interactivo(personal(a.retirarButaca)))
	mux.Handle("DELETE /api/salas/{sala}/fuera-servicio/{asiento}", interactivo(personal(a.habilitarButaca)))

	// Compra (cliente)
	mux.Handle("POST /api/funciones/{id}/asientos/{asiento}/bloqueo", interactivo(cliente(a.bloquear)))
	mux.Handle("DELETE /api/funciones/{id}/bloqueos", interactivo(cliente(a.desbloquear)))
	mux.HandleFunc("GET /api/funciones/{id}/mis-bloqueos", cliente(a.misBloqueos))
	mux.Handle("POST /api/reservas", sensible(cliente(a.crearReserva)))
	mux.HandleFunc("POST /api/reservas/{id}/pagar", cliente(a.pagar))
	mux.HandleFunc("DELETE /api/reservas/{id}", cliente(a.cancelarReserva))

	// Cuenta del cliente
	mux.HandleFunc("GET /api/cuenta", cliente(a.cuenta))
	mux.Handle("POST /api/creditos/trivia", interactivo(cliente(a.trivia)))
	mux.HandleFunc("GET /api/boletos/{token}/codigos", cliente(a.codigosBoleto))
	mux.HandleFunc("GET /api/compras/{folio}", cliente(a.comprobante))
	mux.Handle("GET /api/comprobantes/{folio}/verificar", interactivo(a.verificarComprobante))

	// Cola virtual del preestreno (cliente)
	mux.Handle("POST /api/cola/{evento}", sensible(cliente(a.entrarCola)))
	mux.HandleFunc("GET /api/cola/{evento}", cliente(a.posicionCola))
	mux.HandleFunc("DELETE /api/cola/{evento}", cliente(a.salirCola))

	// Operación
	mux.Handle("POST /api/boletos/{token}/validar", interactivo(personal(a.validarBoleto)))
	mux.HandleFunc("GET /api/metricas", auditoria(a.metricas))
	mux.HandleFunc("GET /api/bitacora", auditoria(a.bitacora))
	mux.HandleFunc("GET /api/clientes/historial", admin(a.historialClientes))

	// Usuarios y roles (administrador; crear, editar y dar de baja exigen reconfirmar la contraseña)
	mux.HandleFunc("GET /api/usuarios", admin(a.listarUsuarios))
	mux.Handle("POST /api/usuarios", interactivo(admin(a.crearUsuario)))
	mux.Handle("POST /api/usuarios/{id}", interactivo(admin(a.editarUsuario)))
	mux.Handle("POST /api/usuarios/{id}/estado", interactivo(admin(a.estadoUsuario)))

	// Seguridad
	mux.Handle("POST /api/captcha/verificar", interactivo(a.verificarCaptcha))

	// Cualquier otra ruta: 404 en JSON.
	mux.HandleFunc("/", httpx.NoEncontrado)

	// Orden de los middlewares (de afuera hacia adentro): CORS → recuperación de panics → cabeceras de seguridad → rutas.
	return httpx.CORS(origenes, httpx.Recuperar(httpx.CabecerasSeguridad(mux)))
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
