package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/cineight/backend/internal/domain"
	"github.com/cineight/backend/internal/services"
	"github.com/cineight/backend/pkg/httpx"
	"github.com/cineight/backend/pkg/recaptcha"
)

// ───────── Reservas y pago ─────────

// POST /api/reservas  {funcion, usuario_id, asientos} → retención de 5 min
func (a *API) crearReserva(w http.ResponseWriter, r *http.Request) {
	var s services.SolicitudReserva
	if err := httpx.Leer(r, &s); err != nil {
		httpx.Error(w, 400, fmt.Sprintf("Solicitud inválida: entre 1 y %d asientos y usuario_id requeridos.", domain.MaxAsientosPorCompra))
		return
	}
	id, expira, err := a.s.Reservas.Crear(r.Context(), s)
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 201, map[string]any{"reserva_id": id, "expira": expira})
}

// POST /api/reservas/{id}/pagar — idempotente
func (a *API) pagar(w http.ResponseWriter, r *http.Request) {
	res, estado, err := a.s.Reservas.Pagar(r.Context(), r.PathValue("id"))
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, estado, res)
}

// DELETE /api/reservas/{id}
func (a *API) cancelarReserva(w http.ResponseWriter, r *http.Request) {
	if err := a.s.Reservas.Cancelar(r.Context(), r.PathValue("id")); err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]string{"estado": "cancelada"})
}

// ───────── Cuentas y créditos ─────────

// POST /api/sesion  {correo, nombre, pase?} → {usuario_id, saldo}
// `pase` (de /api/captcha/verificar) es obligatorio sólo cuando la cuenta todavía no existe.
func (a *API) sesion(w http.ResponseWriter, r *http.Request) {
	var s struct {
		Correo string `json:"correo"`
		Nombre string `json:"nombre"`
		Pase   string `json:"pase"`
	}
	if err := httpx.Leer(r, &s); err != nil {
		httpx.Error(w, 400, "correo requerido")
		return
	}
	id, saldo, err := a.s.Cuentas.Sesion(r.Context(), s.Correo, s.Nombre, s.Pase)
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"usuario_id": id, "saldo": saldo})
}

// GET /api/usuarios/{id}/cuenta
func (a *API) cuenta(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Error(w, 404, "La cuenta no existe.")
		return
	}
	c, err := a.s.Cuentas.Cuenta(r.Context(), uid)
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, c)
}

// POST /api/creditos/trivia  {usuario_id, partida, aciertos}
func (a *API) trivia(w http.ResponseWriter, r *http.Request) {
	var s struct {
		UsuarioID int64  `json:"usuario_id"`
		Partida   string `json:"partida"`
		Aciertos  int    `json:"aciertos"`
	}
	if err := httpx.Leer(r, &s); err != nil {
		httpx.Error(w, 400, "Partida inválida.")
		return
	}
	acreditado, monto, saldo, err := a.s.Cuentas.Trivia(r.Context(), s.UsuarioID, s.Partida, s.Aciertos)
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"acreditado": acreditado, "monto": monto, "saldo": saldo})
}

// ───────── Operación ─────────

// POST /api/boletos/{token}/validar
func (a *API) validarBoleto(w http.ResponseWriter, r *http.Request) {
	// {codigo, ventana}: el código dinámico que muestra el QR del cliente y su ventana de un minuto.
	var q struct {
		Codigo  string `json:"codigo"`
		Ventana int64  `json:"ventana"`
	}
	_ = httpx.Leer(r, &q) // sin cuerpo el código queda vacío y el servicio rechaza la validación
	v, err := a.s.Cuentas.ValidarBoleto(r.Context(), r.PathValue("token"), q.Codigo, q.Ventana)
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, v)
}

// POST /api/cola/{evento}  {usuario_id, pase?} → posición o turno. `pase` (de /api/captcha/verificar con
// acción "cola") es obligatorio cuando reCAPTCHA está activo.
func (a *API) entrarCola(w http.ResponseWriter, r *http.Request) {
	var s struct {
		UsuarioID int64  `json:"usuario_id"`
		Pase      string `json:"pase"`
	}
	if err := httpx.Leer(r, &s); err != nil {
		httpx.Error(w, 400, "usuario_id requerido")
		return
	}
	p, err := a.s.Cola.Entrar(r.Context(), r.PathValue("evento"), s.UsuarioID, s.Pase)
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, p)
}

// GET /api/cola/{evento}/{usuario} — posición en la fila o turno vigente. Quien espera la consulta cada pocos
// segundos: si deja de hacerlo, pierde su lugar.
func (a *API) posicionCola(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.ParseInt(r.PathValue("usuario"), 10, 64)
	if err != nil {
		httpx.Error(w, 400, "usuario inválido")
		return
	}
	p, err := a.s.Cola.Posicion(r.Context(), r.PathValue("evento"), uid)
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, p)
}

// DELETE /api/cola/{evento}/{usuario} — abandona la fila o libera el turno (p. ej. tras comprar).
func (a *API) salirCola(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.ParseInt(r.PathValue("usuario"), 10, 64)
	if err != nil {
		httpx.Error(w, 400, "usuario inválido")
		return
	}
	if err := a.s.Cola.Salir(r.Context(), r.PathValue("evento"), uid); err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]string{"estado": "fuera de la cola"})
}

// GET /api/metricas
func (a *API) metricas(w http.ResponseWriter, r *http.Request) {
	m, err := a.s.Panel.Metricas(r.Context())
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, m)
}

// GET /api/clientes/historial — compras de cada cliente agrupadas por película (estadísticas del administrador).
func (a *API) historialClientes(w http.ResponseWriter, r *http.Request) {
	h, err := a.s.Panel.HistorialClientes(r.Context())
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, h)
}

// ───────── Seguridad ─────────

// POST /api/captcha/verificar  {token, accion} → {valido, puntaje, accion, motivo, activo, pase?}
// El frontend lo llama antes de procesar las credenciales de inicio de sesión o registro.
func (a *API) verificarCaptcha(w http.ResponseWriter, r *http.Request) {
	var s struct {
		Token  string `json:"token"`
		Accion string `json:"accion"`
	}
	if err := httpx.Leer(r, &s); err != nil {
		httpx.Error(w, 400, "Solicitud inválida.")
		return
	}
	res, pase, err := a.s.Seguridad.VerificarCaptcha(r.Context(), s.Token, s.Accion, httpx.IP(r))
	if err != nil {
		fallar(w, err)
		return
	}
	estado := http.StatusOK
	if !res.Valido {
		estado = http.StatusForbidden
	}
	httpx.JSON(w, estado, struct {
		recaptcha.Resultado
		Pase string `json:"pase,omitempty"`
	}{res, pase})
}
