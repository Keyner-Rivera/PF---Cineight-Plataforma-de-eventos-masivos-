package handlers

import (
	"fmt"
	"net/http"

	"github.com/cineight/backend/internal/domain"
	"github.com/cineight/backend/internal/services"
	"github.com/cineight/backend/pkg/httpx"
	"github.com/cineight/backend/pkg/recaptcha"
)

// ───────── Reservas y pago ─────────
// El usuario siempre sale de la sesión: lo que el cliente mande como usuario_id se ignora.

// POST /api/reservas  {funcion, asientos} → retención de 5 min
func (a *API) crearReserva(w http.ResponseWriter, r *http.Request) {
	var s services.SolicitudReserva
	if err := httpx.Leer(r, &s); err != nil {
		httpx.Error(w, 400, fmt.Sprintf("Solicitud inválida: entre 1 y %d asientos requeridos.", domain.MaxAsientosPorCompra))
		return
	}
	s.UsuarioID = sesion(r).Usuario.ID
	res, err := a.s.Reservas.Crear(r.Context(), s)
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 201, res)
}

// POST /api/reservas/{id}/pagar — idempotente; sólo el dueño de la reserva
func (a *API) pagar(w http.ResponseWriter, r *http.Request) {
	res, estado, err := a.s.Reservas.Pagar(r.Context(), r.PathValue("id"), sesion(r).Usuario.ID)
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, estado, res)
}

// DELETE /api/reservas/{id} — sólo el dueño de la reserva
func (a *API) cancelarReserva(w http.ResponseWriter, r *http.Request) {
	if err := a.s.Reservas.Cancelar(r.Context(), r.PathValue("id"), sesion(r).Usuario.ID); err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]string{"estado": "cancelada"})
}

// ───────── Cuenta y créditos (del usuario de la sesión) ─────────

// GET /api/cuenta → saldo, movimientos y boletos
func (a *API) cuenta(w http.ResponseWriter, r *http.Request) {
	c, err := a.s.Cuentas.Cuenta(r.Context(), sesion(r).Usuario.ID)
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, c)
}

// POST /api/creditos/trivia  {partida, aciertos}
func (a *API) trivia(w http.ResponseWriter, r *http.Request) {
	var s struct {
		Partida  string `json:"partida"`
		Aciertos int    `json:"aciertos"`
	}
	if err := httpx.Leer(r, &s); err != nil {
		httpx.Error(w, 400, "Partida inválida.")
		return
	}
	acreditado, monto, saldo, err := a.s.Cuentas.Trivia(r.Context(), sesion(r).Usuario.ID, s.Partida, s.Aciertos)
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"acreditado": acreditado, "monto": monto, "saldo": saldo})
}

// GET /api/boletos/{token}/codigos → {desde, codigos} de los próximos minutos (sólo el dueño del boleto)
func (a *API) codigosBoleto(w http.ResponseWriter, r *http.Request) {
	desde, codigos, err := a.s.Cuentas.CodigosQR(r.Context(), r.PathValue("token"), sesion(r).Usuario.ID)
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"desde": desde, "codigos": codigos, "rotacion_segundos": int(domain.RotacionQR.Seconds())})
}

// GET /api/compras/{folio} → comprobante firmado de una compra (sólo su dueño)
func (a *API) comprobante(w http.ResponseWriter, r *http.Request) {
	c, err := a.s.Cuentas.Comprobante(r.Context(), r.PathValue("folio"), sesion(r).Usuario.ID)
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, c)
}

// GET /api/comprobantes/{folio}/verificar?firma=… → autenticidad del comprobante (pública: la usa su QR)
func (a *API) verificarComprobante(w http.ResponseWriter, r *http.Request) {
	v, err := a.s.Cuentas.VerificarComprobante(r.Context(), r.PathValue("folio"), r.URL.Query().Get("firma"))
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, v)
}

// ───────── Operación ─────────

// POST /api/boletos/{token}/validar  {codigo, ventana} (agente o administrador)
func (a *API) validarBoleto(w http.ResponseWriter, r *http.Request) {
	// El código dinámico que muestra el QR del cliente y su ventana de un minuto (0 = código escrito a mano).
	var q struct {
		Codigo  string `json:"codigo"`
		Ventana int64  `json:"ventana"`
	}
	_ = httpx.Leer(r, &q) // sin cuerpo el código queda vacío y el servicio rechaza la validación
	v, err := a.s.Cuentas.ValidarBoleto(r.Context(), r.PathValue("token"), q.Codigo, q.Ventana, sesion(r).Usuario.ID)
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, v)
}

// POST /api/cola/{evento}  {pase?} → posición o turno. `pase` (de /api/captcha/verificar con
// acción "cola") es obligatorio cuando reCAPTCHA está activo.
func (a *API) entrarCola(w http.ResponseWriter, r *http.Request) {
	var s struct {
		Pase string `json:"pase"`
	}
	_ = httpx.Leer(r, &s)
	p, err := a.s.Cola.Entrar(r.Context(), r.PathValue("evento"), sesion(r).Usuario.ID, s.Pase)
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, p)
}

// GET /api/cola/{evento} — posición en la fila o turno vigente. Quien espera la consulta cada pocos segundos:
// si deja de hacerlo, pierde su lugar.
func (a *API) posicionCola(w http.ResponseWriter, r *http.Request) {
	p, err := a.s.Cola.Posicion(r.Context(), r.PathValue("evento"), sesion(r).Usuario.ID)
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, p)
}

// DELETE /api/cola/{evento} — abandona la fila o libera el turno (p. ej. tras comprar).
func (a *API) salirCola(w http.ResponseWriter, r *http.Request) {
	if err := a.s.Cola.Salir(r.Context(), r.PathValue("evento"), sesion(r).Usuario.ID); err != nil {
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
