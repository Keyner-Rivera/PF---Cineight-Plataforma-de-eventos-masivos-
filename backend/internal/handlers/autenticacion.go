package handlers

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/cineight/backend/internal/domain"
	"github.com/cineight/backend/internal/services"
	"github.com/cineight/backend/pkg/httpx"
)

type claveSesion struct{}

// sesion devuelve la sesión que el middleware autenticar dejó en el contexto.
func sesion(r *http.Request) domain.Sesion { return r.Context().Value(claveSesion{}).(domain.Sesion) }

// tokenBearer extrae el token de "Authorization: Bearer <token>". El token viaja en una cabecera (no en una cookie):
// el navegador no lo envía solo, así que otra página no puede hacer solicitudes en nombre del usuario (CSRF).
func tokenBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// autenticar exige una sesión vigente y, si se indican, uno de los roles. Con claveTemporal=false, una cuenta que
// todavía tiene su contraseña temporal sólo puede cambiarla (y cerrar sesión). Las respuestas privadas no se guardan
// en cachés (Cache-Control: no-store).
func (a *API) autenticar(claveTemporal bool, roles ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			ses, err := a.s.Auth.Autenticar(r.Context(), tokenBearer(r))
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Bearer realm="cineight"`)
				fallar(w, err)
				return
			}
			if ses.Usuario.DebeCambiarClave && !claveTemporal {
				httpx.Error(w, http.StatusForbidden, "Debes cambiar tu contraseña temporal antes de continuar.")
				return
			}
			if len(roles) > 0 && !slices.Contains(roles, ses.Usuario.Rol) {
				httpx.Error(w, http.StatusForbidden, "No tienes permiso para esta acción.")
				return
			}
			next(w, r.WithContext(context.WithValue(r.Context(), claveSesion{}, ses)))
		}
	}
}

func origen(r *http.Request) services.Origen {
	return services.Origen{IP: httpx.IP(r), Agente: r.UserAgent()}
}

// POST /api/auth/registro  {nombre, correo, clave, pase} → {token, expira, usuario}
func (a *API) registro(w http.ResponseWriter, r *http.Request) {
	var s struct{ Nombre, Correo, Clave, Pase string }
	if err := httpx.Leer(r, &s); err != nil {
		httpx.Error(w, 400, "Solicitud inválida.")
		return
	}
	res, err := a.s.Auth.Registrar(r.Context(), s.Nombre, s.Correo, s.Clave, s.Pase, origen(r))
	if err != nil {
		fallar(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, 201, res)
}

// POST /api/auth/login  {correo, clave} → {token, expira, usuario}
func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var s struct{ Correo, Clave string }
	if err := httpx.Leer(r, &s); err != nil {
		httpx.Error(w, 400, "Solicitud inválida.")
		return
	}
	res, err := a.s.Auth.IniciarSesion(r.Context(), s.Correo, s.Clave, origen(r))
	if err != nil {
		fallar(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, 200, res)
}

// GET /api/auth/sesion → usuario de la sesión (el frontend la valida al cargar)
func (a *API) miSesion(w http.ResponseWriter, r *http.Request) {
	s := sesion(r)
	httpx.JSON(w, 200, map[string]any{"usuario": s.Usuario, "expira": s.Expira, "elevada_hasta": s.ElevadaHasta})
}

// POST /api/auth/logout — revoca la sesión en el servidor
func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if err := a.s.Auth.CerrarSesion(r.Context(), sesion(r)); err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]string{"estado": "sesión cerrada"})
}

// POST /api/auth/logout-todas — revoca las sesiones del usuario en todos sus dispositivos
func (a *API) logoutTodas(w http.ResponseWriter, r *http.Request) {
	n, err := a.s.Auth.CerrarTodas(r.Context(), sesion(r))
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]int64{"sesiones_cerradas": n})
}

// POST /api/auth/clave  {actual, nueva} — cierra las demás sesiones del usuario
func (a *API) cambiarClave(w http.ResponseWriter, r *http.Request) {
	var s struct{ Actual, Nueva string }
	if err := httpx.Leer(r, &s); err != nil {
		httpx.Error(w, 400, "Solicitud inválida.")
		return
	}
	if err := a.s.Auth.CambiarClave(r.Context(), sesion(r), s.Actual, s.Nueva); err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]string{"estado": "contraseña actualizada"})
}

// POST /api/auth/reconfirmar  {clave} → {elevada_hasta}
func (a *API) reconfirmar(w http.ResponseWriter, r *http.Request) {
	var s struct{ Clave string }
	if err := httpx.Leer(r, &s); err != nil {
		httpx.Error(w, 400, "Solicitud inválida.")
		return
	}
	hasta, err := a.s.Auth.Reconfirmar(r.Context(), sesion(r), s.Clave)
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"elevada_hasta": hasta})
}

// ───────── Usuarios (administrador) ─────────

// GET /api/usuarios
func (a *API) listarUsuarios(w http.ResponseWriter, r *http.Request) {
	lista, err := a.s.Auth.Usuarios(r.Context())
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, lista)
}

// POST /api/usuarios  {nombre, correo, rol, clave}
func (a *API) crearUsuario(w http.ResponseWriter, r *http.Request) {
	var s struct{ Nombre, Correo, Rol, Clave string }
	if err := httpx.Leer(r, &s); err != nil {
		httpx.Error(w, 400, "Solicitud inválida.")
		return
	}
	u, err := a.s.Auth.CrearUsuario(r.Context(), sesion(r), s.Nombre, s.Correo, s.Rol, s.Clave)
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 201, u)
}

func idDeRuta(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, 404, "La cuenta no existe.")
		return 0, false
	}
	return id, true
}

// POST /api/usuarios/{id}  {nombre, rol}
func (a *API) editarUsuario(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}
	var s struct{ Nombre, Rol string }
	if err := httpx.Leer(r, &s); err != nil {
		httpx.Error(w, 400, "Solicitud inválida.")
		return
	}
	if err := a.s.Auth.EditarUsuario(r.Context(), sesion(r), id, s.Nombre, s.Rol); err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]string{"estado": "actualizado"})
}

// POST /api/usuarios/{id}/estado  {activo}
func (a *API) estadoUsuario(w http.ResponseWriter, r *http.Request) {
	id, ok := idDeRuta(w, r)
	if !ok {
		return
	}
	var s struct {
		Activo bool `json:"activo"`
	}
	if err := httpx.Leer(r, &s); err != nil {
		httpx.Error(w, 400, "Solicitud inválida.")
		return
	}
	if err := a.s.Auth.CambiarEstadoUsuario(r.Context(), sesion(r), id, s.Activo); err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]string{"estado": "actualizado"})
}

// GET /api/bitacora — bitácora de auditoría del servidor
func (a *API) bitacora(w http.ResponseWriter, r *http.Request) {
	lista, err := a.s.Auth.Bitacora(r.Context())
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, lista)
}
