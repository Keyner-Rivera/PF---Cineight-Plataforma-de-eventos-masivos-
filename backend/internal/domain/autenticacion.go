package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Reglas de autenticación y sesión (OWASP Authentication y Session Management Cheat Sheets, ASVS V2/V3).
const (
	InactividadSesion    = 15 * time.Minute // la sesión expira en el servidor tras este tiempo sin solicitudes (RNF-24)
	DuracionMaximaSesion = 12 * time.Hour   // límite absoluto, aunque haya actividad: obliga a volver a autenticarse
	ReconfirmacionAdmin  = 5 * time.Minute  // gestionar usuarios exige haber reconfirmado la contraseña hace menos de esto
	IntentosFallidosMax  = 5                // fallos seguidos antes de bloquear la cuenta (RF-43)
	BloqueoPorIntentos   = 15 * time.Minute
	ClaveMin             = 8
	ClaveMax             = 72 // bcrypt sólo usa los primeros 72 bytes: más largo se rechaza en lugar de truncarse en silencio
)

// Roles del control de acceso (RF-44). El id coincide con la tabla roles.
const (
	RolCliente = "cliente"
	RolAgente  = "agente"
	RolAdmin   = "admin"
	RolAuditor = "auditor"
)

// RolesValidos son los roles que puede tener una cuenta.
var RolesValidos = map[string]bool{RolCliente: true, RolAgente: true, RolAdmin: true, RolAuditor: true}

// Usuario autenticado, tal como lo ve el frontend.
type Usuario struct {
	ID               int64     `json:"id"`
	Nombre           string    `json:"nombre"`
	Correo           string    `json:"correo"`
	Rol              string    `json:"rol"`
	Activo           bool      `json:"activo"`
	Creado           time.Time `json:"creado"`
	DebeCambiarClave bool      `json:"debe_cambiar_clave"`
}

// Credenciales de un usuario para verificar el inicio de sesión. Hash nunca sale del servidor.
type Credenciales struct {
	Usuario
	Hash           string
	Intentos       int
	BloqueadoHasta *time.Time
	ClaveDefinida  bool // false: cuenta anterior a la autenticación en el servidor (sin contraseña todavía)
}

// Sesion es una sesión vigente en el servidor. El token sólo lo conoce el navegador; aquí se guarda su SHA-256.
type Sesion struct {
	Usuario         Usuario
	Hash            string
	Creada          time.Time
	UltimaActividad time.Time
	Expira          time.Time
	ElevadaHasta    *time.Time
}

// Elevada indica si el usuario reconfirmó su contraseña recientemente (acciones sensibles).
func (s Sesion) Elevada(ahora time.Time) bool {
	return s.ElevadaHasta != nil && ahora.Before(*s.ElevadaHasta)
}

// Vencida indica si la sesión superó su límite absoluto o el tiempo de inactividad.
func (s Sesion) Vencida(ahora time.Time) bool {
	return !ahora.Before(s.Expira) || ahora.Sub(s.UltimaActividad) > InactividadSesion
}

// EntradaBitacora es un registro de la bitácora de auditoría (RNF-22).
type EntradaBitacora struct {
	Fecha   time.Time `json:"fecha"`
	Usuario string    `json:"usuario"`
	Accion  string    `json:"accion"`
	Detalle string    `json:"detalle"`
}

// ErrYaRegistrado: el correo ya pertenece a una cuenta con contraseña.
var ErrYaRegistrado = errors.New("correo ya registrado")

var clavesComunes = map[string]bool{
	"12345678": true, "123456789": true, "1234567890": true, "password": true, "password1": true, "contraseña": true,
	"contrasena": true, "qwerty123": true, "abc12345": true, "11111111": true, "00000000": true, "iloveyou1": true,
	"admin123": true, "cineight123": true, "cineight1": true,
}

var soloLetrasYNumeros = regexp.MustCompile(`[^\p{L}\d]`)

// ValidarClaveNueva aplica las mismas reglas que el formulario del frontend (defensa en profundidad): longitud,
// letras y números, sin espacios en los extremos, que no sea común ni contenga el usuario del correo.
func ValidarClaveNueva(clave, correo string) error {
	malo := func(msg string) error { return Invalido("La contraseña no cumple: " + msg + ".") }
	switch {
	case len(clave) < ClaveMin || len(clave) > ClaveMax:
		return malo("entre 8 y 72 caracteres")
	case strings.TrimSpace(clave) != clave:
		return malo("sin espacios al inicio ni al final")
	case !strings.ContainsFunc(clave, unicode.IsLetter) || !strings.ContainsFunc(clave, unicode.IsDigit):
		return malo("letras y al menos un número")
	case clavesComunes[strings.ToLower(clave)]:
		return malo("que no sea común")
	}
	limpiar := func(t string) string { return soloLetrasYNumeros.ReplaceAllString(strings.ToLower(t), "") }
	if usuario := limpiar(strings.SplitN(correo, "@", 2)[0]); len([]rune(usuario)) >= 4 && strings.Contains(limpiar(clave), usuario) {
		return malo("que no contenga tu correo")
	}
	return nil
}
