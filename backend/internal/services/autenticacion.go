package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/cineight/backend/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

// Autenticacion maneja contraseñas, sesiones y cuentas (OWASP Authentication, Session Management y Password Storage).
//
//   - Contraseñas: bcrypt (costo 10, el mínimo que recomienda OWASP; más alto haría tardar varios segundos cada inicio
//     de sesión en el plan gratuito de Render, de 0.1 CPU).
//   - Sesiones: token aleatorio de 256 bits que sólo conoce el navegador; la base guarda su SHA-256. Vencen tras
//     domain.InactividadSesion sin uso o domain.DuracionMaximaSesion en total, y cerrar sesión las revoca en el servidor.
//   - Fuerza bruta: mensaje genérico ("correo o contraseña incorrectos"), mismo tiempo de respuesta exista o no la
//     cuenta, bloqueo de la cuenta tras domain.IntentosFallidosMax fallos y límite de solicitudes por IP (router).
type Autenticacion struct {
	repo      RepoAutenticacion
	guardia   GuardiaAntiBots
	auditoria Auditoria
	costo     int
	ahora     func() time.Time

	mu    sync.Mutex
	cache map[string]sesionEnCache // hash del token → sesión, para no consultar la base en cada solicitud

	turnosBcrypt chan struct{} // limita los cálculos de bcrypt simultáneos (ver bcryptSimultaneos)
}

// bcryptSimultaneos es cuántos hashes de contraseña se calculan a la vez. bcrypt es lento a propósito (~0.6 s con la
// 0.1 CPU del plan gratuito): sin este límite, una ola de inicios de sesión acapararía el procesador y frenaría las
// compras y los mapas en vivo. Los demás inicios de sesión esperan su turno (hasta esperaBcrypt).
const (
	bcryptSimultaneos = 2
	esperaBcrypt      = 15 * time.Second // menos que los 20 s que espera el navegador: recibe el aviso en lugar de cortar
)

type sesionEnCache struct {
	s     domain.Sesion
	hasta time.Time
}

// vidaCacheSesion es cuánto se reutiliza una sesión leída de la base. Revocar (cerrar sesión, baja de la cuenta)
// también la borra de la caché, así que sólo afecta a cambios hechos por otra instancia de la API.
const vidaCacheSesion = 30 * time.Second

// hashFicticio se compara cuando el correo no existe, para que la respuesta tarde lo mismo (evita enumerar cuentas).
var hashFicticio, _ = bcrypt.GenerateFromPassword([]byte("cuenta-inexistente-0000"), bcrypt.DefaultCost)

// RepoAutenticacion es el almacenamiento de cuentas y sesiones.
type RepoAutenticacion interface {
	Credenciales(ctx context.Context, correo string) (domain.Credenciales, error)
	RegistrarFallo(ctx context.Context, usuarioID int64, maximo int, bloqueo time.Duration) (*time.Time, error)
	ReiniciarFallos(ctx context.Context, usuarioID int64) error
	RegistrarCliente(ctx context.Context, correo, nombre, hash string) (domain.Usuario, error)
	CambiarClave(ctx context.Context, usuarioID int64, hash string, debeCambiar bool) error
	PersonalSinClave(ctx context.Context) ([]int64, error)
	CrearSesion(ctx context.Context, hash string, usuarioID int64, expira time.Time, ip, agente string) error
	Sesion(ctx context.Context, hash string) (domain.Sesion, error)
	TocarSesion(ctx context.Context, hash string) error
	ElevarSesion(ctx context.Context, hash string, hasta time.Time) error
	RevocarSesion(ctx context.Context, hash string) error
	RevocarSesionesDe(ctx context.Context, usuarioID int64, excepto string) (int64, error)
	Usuarios(ctx context.Context) ([]domain.Usuario, error)
	Usuario(ctx context.Context, id int64) (domain.Usuario, error)
	CrearUsuario(ctx context.Context, nombre, correo, rol, hash string) (domain.Usuario, error)
	EditarUsuario(ctx context.Context, id int64, nombre, rol string) error
	CambiarEstadoUsuario(ctx context.Context, id int64, activo bool) error
	AdminsActivos(ctx context.Context) (int, error)
	Bitacora(ctx context.Context, limite int) ([]domain.EntradaBitacora, error)
}

// NuevaAutenticacion crea el servicio.
func NuevaAutenticacion(r RepoAutenticacion, g GuardiaAntiBots, a Auditoria) *Autenticacion {
	return &Autenticacion{repo: r, guardia: g, auditoria: a, costo: bcrypt.DefaultCost, ahora: time.Now, cache: map[string]sesionEnCache{}, turnosBcrypt: make(chan struct{}, bcryptSimultaneos)}
}

// SesionIniciada es la respuesta de iniciar sesión o registrarse. El token sólo se entrega aquí.
type SesionIniciada struct {
	Token   string         `json:"token"`
	Expira  time.Time      `json:"expira"`
	Usuario domain.Usuario `json:"usuario"`
}

// Origen identifica de dónde viene una solicitud (bitácora y sesiones).
type Origen struct{ IP, Agente string }

// errCredenciales es la única respuesta ante correo inexistente o contraseña incorrecta.
var errCredenciales = domain.NoAutorizado("Correo o contraseña incorrectos.")

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func nuevoToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// conTurnoBcrypt ejecuta f cuando hay turno libre para calcular bcrypt.
func (s *Autenticacion) conTurnoBcrypt(ctx context.Context, f func()) error {
	espera, cancelar := context.WithTimeout(ctx, esperaBcrypt)
	defer cancelar()
	select {
	case s.turnosBcrypt <- struct{}{}:
	case <-espera.Done():
		return domain.Demasiadas("Hay muchos inicios de sesión en este momento. Intenta de nuevo en unos segundos.")
	}
	defer func() { <-s.turnosBcrypt }()
	f()
	return nil
}

func (s *Autenticacion) cifrar(ctx context.Context, clave string) (hash string, err error) {
	if e := s.conTurnoBcrypt(ctx, func() {
		var h []byte
		h, err = bcrypt.GenerateFromPassword([]byte(clave), s.costo)
		hash = string(h)
	}); e != nil {
		return "", e
	}
	return hash, err
}

// coincide compara la contraseña con el hash en tiempo constante (bcrypt).
func (s *Autenticacion) coincide(ctx context.Context, hash []byte, clave string) (bool, error) {
	var ok bool
	err := s.conTurnoBcrypt(ctx, func() { ok = bcrypt.CompareHashAndPassword(hash, []byte(clave)) == nil })
	return ok, err
}

func uid(id int64) *int64 { return &id }

// Registrar crea la cuenta de un cliente (con el pase anti-bots) y abre su sesión.
func (s *Autenticacion) Registrar(ctx context.Context, nombre, correo, clave, pase string, o Origen) (SesionIniciada, error) {
	correo = domain.NormalizarCorreo(correo)
	nombre = domain.NormalizarNombre(nombre)
	if err := domain.ValidarCorreo(correo); err != nil {
		return SesionIniciada{}, err
	}
	if err := domain.ValidarNombre(nombre); err != nil {
		return SesionIniciada{}, err
	}
	if err := domain.ValidarClaveNueva(clave, correo); err != nil {
		return SesionIniciada{}, err
	}
	// Crear una cuenta regala créditos: sin el pase de reCAPTCHA un bot podría fabricar cuentas.
	if err := s.guardia.ExigirPase(ctx, pase, "alta de cuenta"); err != nil {
		return SesionIniciada{}, err
	}
	hash, err := s.cifrar(ctx, clave)
	if err != nil {
		return SesionIniciada{}, err
	}
	u, err := s.repo.RegistrarCliente(ctx, correo, nombre, hash)
	if errors.Is(err, domain.ErrYaRegistrado) {
		return SesionIniciada{}, domain.Conflicto("Ese correo ya tiene una cuenta. Inicia sesión.")
	}
	if err != nil {
		return SesionIniciada{}, err
	}
	s.auditoria.Auditar(ctx, uid(u.ID), "CUENTA_CREADA", "usuarios", map[string]string{"ip": o.IP})
	return s.abrirSesion(ctx, u, o)
}

// IniciarSesion verifica las credenciales y abre una sesión.
func (s *Autenticacion) IniciarSesion(ctx context.Context, correo, clave string, o Origen) (SesionIniciada, error) {
	correo = domain.NormalizarCorreo(correo)
	if correo == "" || clave == "" || len(clave) > domain.ClaveMax {
		return SesionIniciada{}, errCredenciales
	}
	c, err := s.repo.Credenciales(ctx, correo)
	if errors.Is(err, domain.ErrNoExiste) || (err == nil && !c.ClaveDefinida && c.Rol == domain.RolCliente) {
		if _, err := s.coincide(ctx, hashFicticio, clave); err != nil { // mismo tiempo de respuesta
			return SesionIniciada{}, err
		}
		s.auditoria.Auditar(ctx, nil, "INICIO_FALLIDO", "usuarios", map[string]string{"correo": correo, "ip": o.IP})
		if err == nil {
			// Cuenta de cliente anterior a la autenticación en el servidor: debe registrarse para crear su contraseña.
			return SesionIniciada{}, domain.NoAutorizado("Por seguridad actualizamos el inicio de sesión: usa «Registrarse» con este mismo correo para crear tu contraseña. Conservas tu saldo y tus boletos.")
		}
		return SesionIniciada{}, errCredenciales
	}
	if err != nil {
		return SesionIniciada{}, err
	}
	if c.BloqueadoHasta != nil && s.ahora().Before(*c.BloqueadoHasta) {
		minutos := int(c.BloqueadoHasta.Sub(s.ahora()).Minutes()) + 1
		return SesionIniciada{}, domain.Demasiadas(fmt.Sprintf("Cuenta bloqueada temporalmente por intentos fallidos. Intenta de nuevo en %d min.", minutos))
	}
	valida, err := s.coincide(ctx, []byte(c.Hash), clave)
	if err != nil {
		return SesionIniciada{}, err
	}
	if !valida {
		hasta, err := s.repo.RegistrarFallo(ctx, c.ID, domain.IntentosFallidosMax, domain.BloqueoPorIntentos)
		if err != nil {
			return SesionIniciada{}, err
		}
		s.auditoria.Auditar(ctx, uid(c.ID), "INICIO_FALLIDO", "usuarios", map[string]string{"ip": o.IP})
		if hasta != nil && s.ahora().Before(*hasta) {
			s.auditoria.Auditar(ctx, uid(c.ID), "CUENTA_BLOQUEADA", "usuarios", map[string]string{"ip": o.IP})
			return SesionIniciada{}, domain.Demasiadas(fmt.Sprintf("Demasiados intentos fallidos: la cuenta quedó bloqueada %d minutos.", int(domain.BloqueoPorIntentos.Minutes())))
		}
		return SesionIniciada{}, errCredenciales
	}
	if !c.Activo {
		return SesionIniciada{}, domain.Prohibido("Esta cuenta fue dada de baja. Contacta al administrador.")
	}
	if err := s.repo.ReiniciarFallos(ctx, c.ID); err != nil {
		return SesionIniciada{}, err
	}
	return s.abrirSesion(ctx, c.Usuario, o)
}

func (s *Autenticacion) abrirSesion(ctx context.Context, u domain.Usuario, o Origen) (SesionIniciada, error) {
	token := nuevoToken()
	expira := s.ahora().Add(domain.DuracionMaximaSesion)
	agente := o.Agente
	if len(agente) > 200 {
		agente = agente[:200]
	}
	if err := s.repo.CrearSesion(ctx, hashToken(token), u.ID, expira, o.IP, agente); err != nil {
		return SesionIniciada{}, err
	}
	s.auditoria.Auditar(ctx, uid(u.ID), "INICIO_SESION", "sesiones", map[string]string{"rol": u.Rol, "ip": o.IP})
	return SesionIniciada{Token: token, Expira: expira, Usuario: u}, nil
}

// Autenticar resuelve el token de una solicitud. Una sesión vencida por inactividad o tiempo máximo se revoca.
func (s *Autenticacion) Autenticar(ctx context.Context, token string) (domain.Sesion, error) {
	if token == "" || len(token) > 100 {
		return domain.Sesion{}, domain.NoAutorizado("Inicia sesión para continuar.")
	}
	hash := hashToken(token)
	ahora := s.ahora()
	s.mu.Lock()
	c, enCache := s.cache[hash]
	s.mu.Unlock()
	ses := c.s
	if !enCache || ahora.After(c.hasta) {
		var err error
		if ses, err = s.repo.Sesion(ctx, hash); err != nil {
			s.olvidar(hash)
			if errors.Is(err, domain.ErrNoExiste) {
				return ses, domain.NoAutorizado("Tu sesión terminó. Vuelve a iniciar sesión.")
			}
			return ses, err
		}
	}
	if ses.Vencida(ahora) || !ses.Usuario.Activo {
		_ = s.repo.RevocarSesion(ctx, hash)
		s.olvidar(hash)
		return ses, domain.NoAutorizado("Tu sesión expiró por seguridad. Vuelve a iniciar sesión.")
	}
	// La actividad se registra como mucho una vez por minuto (no una escritura por solicitud).
	if ahora.Sub(ses.UltimaActividad) > time.Minute {
		if err := s.repo.TocarSesion(ctx, hash); err == nil {
			ses.UltimaActividad = ahora
		}
	}
	s.mu.Lock()
	s.cache[hash] = sesionEnCache{s: ses, hasta: ahora.Add(vidaCacheSesion)}
	if len(s.cache) > 10000 { // no crece sin límite: se vacía y se vuelve a llenar desde la base
		s.cache = map[string]sesionEnCache{hash: s.cache[hash]}
	}
	s.mu.Unlock()
	return ses, nil
}

func (s *Autenticacion) olvidar(hash string) {
	s.mu.Lock()
	delete(s.cache, hash)
	s.mu.Unlock()
}

// olvidarUsuario borra de la caché todas las sesiones de un usuario (tras revocarlas en la base).
func (s *Autenticacion) olvidarUsuario(usuarioID int64) {
	s.mu.Lock()
	for h, c := range s.cache {
		if c.s.Usuario.ID == usuarioID {
			delete(s.cache, h)
		}
	}
	s.mu.Unlock()
}

// CerrarSesion revoca la sesión en el servidor: el token deja de servir aunque alguien lo haya copiado.
func (s *Autenticacion) CerrarSesion(ctx context.Context, ses domain.Sesion) error {
	s.olvidar(ses.Hash)
	if err := s.repo.RevocarSesion(ctx, ses.Hash); err != nil {
		return err
	}
	s.auditoria.Auditar(ctx, uid(ses.Usuario.ID), "CIERRE_SESION", "sesiones", nil)
	return nil
}

// CerrarTodas revoca todas las sesiones del usuario en cualquier dispositivo.
func (s *Autenticacion) CerrarTodas(ctx context.Context, ses domain.Sesion) (int64, error) {
	n, err := s.repo.RevocarSesionesDe(ctx, ses.Usuario.ID, "")
	s.olvidarUsuario(ses.Usuario.ID)
	if err == nil {
		s.auditoria.Auditar(ctx, uid(ses.Usuario.ID), "CIERRE_TODAS_LAS_SESIONES", "sesiones", map[string]int64{"sesiones": n})
	}
	return n, err
}

// verificarClave comprueba la contraseña del usuario de la sesión (cuenta los fallos igual que el inicio de sesión).
func (s *Autenticacion) verificarClave(ctx context.Context, ses domain.Sesion, clave string) error {
	c, err := s.repo.Credenciales(ctx, ses.Usuario.Correo)
	if err != nil {
		return err
	}
	valida := false
	if len(clave) <= domain.ClaveMax {
		if valida, err = s.coincide(ctx, []byte(c.Hash), clave); err != nil {
			return err
		}
	}
	if !valida {
		hasta, err := s.repo.RegistrarFallo(ctx, c.ID, domain.IntentosFallidosMax, domain.BloqueoPorIntentos)
		if err != nil {
			return err
		}
		if hasta != nil && s.ahora().Before(*hasta) {
			// Demasiados intentos con una sesión abierta: se cierran todas por si la sesión está en manos de otro.
			_, _ = s.repo.RevocarSesionesDe(ctx, c.ID, "")
			s.olvidarUsuario(c.ID)
			s.auditoria.Auditar(ctx, uid(c.ID), "CUENTA_BLOQUEADA", "usuarios", map[string]string{"motivo": "reconfirmación fallida"})
			return domain.NoAutorizado("Demasiados intentos fallidos: cerramos tu sesión por seguridad.")
		}
		return domain.Invalido("Contraseña incorrecta.")
	}
	return s.repo.ReiniciarFallos(ctx, c.ID)
}

// Reconfirmar eleva la sesión durante domain.ReconfirmacionAdmin (gestionar usuarios lo exige).
func (s *Autenticacion) Reconfirmar(ctx context.Context, ses domain.Sesion, clave string) (time.Time, error) {
	if err := s.verificarClave(ctx, ses, clave); err != nil {
		s.auditoria.Auditar(ctx, uid(ses.Usuario.ID), "REAUTENTICACION_FALLIDA", "sesiones", nil)
		return time.Time{}, err
	}
	hasta := s.ahora().Add(domain.ReconfirmacionAdmin)
	if err := s.repo.ElevarSesion(ctx, ses.Hash, hasta); err != nil {
		return time.Time{}, err
	}
	s.olvidar(ses.Hash)
	s.auditoria.Auditar(ctx, uid(ses.Usuario.ID), "REAUTENTICACION", "sesiones", nil)
	return hasta, nil
}

// CambiarClave reemplaza la contraseña y cierra las demás sesiones del usuario (por si alguien más la conocía).
func (s *Autenticacion) CambiarClave(ctx context.Context, ses domain.Sesion, actual, nueva string) error {
	if err := s.verificarClave(ctx, ses, actual); err != nil {
		return err
	}
	if actual == nueva {
		return domain.Invalido("La contraseña nueva debe ser distinta de la actual.")
	}
	if err := domain.ValidarClaveNueva(nueva, ses.Usuario.Correo); err != nil {
		return err
	}
	hash, err := s.cifrar(ctx, nueva)
	if err != nil {
		return err
	}
	if err := s.repo.CambiarClave(ctx, ses.Usuario.ID, hash, false); err != nil {
		return err
	}
	_, _ = s.repo.RevocarSesionesDe(ctx, ses.Usuario.ID, ses.Hash)
	s.olvidarUsuario(ses.Usuario.ID)
	s.auditoria.Auditar(ctx, uid(ses.Usuario.ID), "CAMBIO_CONTRASENA", "usuarios", nil)
	return nil
}

// PrepararPersonal asigna la contraseña inicial a las cuentas del personal que aún no tienen (bases creadas antes de
// la autenticación en el servidor). Deben cambiarla al entrar. Si `inicial` está vacía se genera una aleatoria y se
// escribe una sola vez en el registro del servidor.
func (s *Autenticacion) PrepararPersonal(ctx context.Context, inicial string) error {
	ids, err := s.repo.PersonalSinClave(ctx)
	if err != nil || len(ids) == 0 {
		return err
	}
	if inicial == "" {
		inicial = "Cine-" + strings.ToLower(nuevoToken()[:10]) + "7"
		log.Printf("SEGURIDAD: contraseña inicial del personal (admin@, agente@, auditor@cineight.gt): %s · cámbiala al entrar o define CLAVE_INICIAL_PERSONAL", inicial)
	}
	hash, err := s.cifrar(ctx, inicial)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.repo.CambiarClave(ctx, id, hash, true); err != nil {
			return err
		}
	}
	log.Printf("seguridad: %d cuentas del personal recibieron la contraseña inicial (deben cambiarla al entrar)", len(ids))
	return nil
}

// ───────── Gestión de usuarios (administrador con la sesión reconfirmada) ─────────

func exigirElevada(ses domain.Sesion, ahora time.Time) error {
	if !ses.Elevada(ahora) {
		return domain.Prohibido("Por seguridad, vuelve a confirmar tu contraseña de administrador.")
	}
	return nil
}

// Usuarios lista las cuentas.
func (s *Autenticacion) Usuarios(ctx context.Context) ([]domain.Usuario, error) {
	return s.repo.Usuarios(ctx)
}

// CrearUsuario da de alta una cuenta con contraseña temporal (debe cambiarla al entrar).
func (s *Autenticacion) CrearUsuario(ctx context.Context, ses domain.Sesion, nombre, correo, rol, clave string) (domain.Usuario, error) {
	if err := exigirElevada(ses, s.ahora()); err != nil {
		return domain.Usuario{}, err
	}
	correo, nombre = domain.NormalizarCorreo(correo), domain.NormalizarNombre(nombre)
	if err := domain.ValidarCorreo(correo); err != nil {
		return domain.Usuario{}, err
	}
	if err := domain.ValidarNombre(nombre); err != nil {
		return domain.Usuario{}, err
	}
	if !domain.RolesValidos[rol] {
		return domain.Usuario{}, domain.Invalido("Rol inválido.")
	}
	if err := domain.ValidarClaveNueva(clave, correo); err != nil {
		return domain.Usuario{}, err
	}
	hash, err := s.cifrar(ctx, clave)
	if err != nil {
		return domain.Usuario{}, err
	}
	u, err := s.repo.CrearUsuario(ctx, nombre, correo, rol, hash)
	if errors.Is(err, domain.ErrYaRegistrado) {
		return u, domain.Conflicto("Ya existe una cuenta con ese correo.")
	}
	if err == nil {
		s.auditoria.Auditar(ctx, uid(ses.Usuario.ID), "USUARIO_CREADO", "usuarios", map[string]string{"correo": correo, "rol": rol})
	}
	return u, err
}

// EditarUsuario cambia nombre y rol. Nadie cambia su propio rol y siempre queda al menos un administrador activo.
func (s *Autenticacion) EditarUsuario(ctx context.Context, ses domain.Sesion, id int64, nombre, rol string) error {
	if err := exigirElevada(ses, s.ahora()); err != nil {
		return err
	}
	nombre = domain.NormalizarNombre(nombre)
	if err := domain.ValidarNombre(nombre); err != nil {
		return err
	}
	if !domain.RolesValidos[rol] {
		return domain.Invalido("Rol inválido.")
	}
	u, err := s.repo.Usuario(ctx, id)
	if errors.Is(err, domain.ErrNoExiste) {
		return domain.NoEncontrado("La cuenta no existe.")
	}
	if err != nil {
		return err
	}
	if id == ses.Usuario.ID && rol != u.Rol {
		return domain.Invalido("No puedes cambiar tu propio rol.")
	}
	if u.Rol == domain.RolAdmin && rol != domain.RolAdmin && u.Activo {
		if n, err := s.repo.AdminsActivos(ctx); err != nil || n <= 1 {
			return errors.Join(err, domain.Invalido("Debe existir al menos un administrador activo."))
		}
	}
	if err := s.repo.EditarUsuario(ctx, id, nombre, rol); err != nil {
		return err
	}
	if rol != u.Rol { // los permisos cambiaron: las sesiones abiertas con el rol anterior se cierran
		_, _ = s.repo.RevocarSesionesDe(ctx, id, "")
		s.olvidarUsuario(id)
	}
	s.auditoria.Auditar(ctx, uid(ses.Usuario.ID), "USUARIO_EDITADO", "usuarios", map[string]string{"correo": u.Correo, "rol_anterior": u.Rol, "rol": rol})
	return nil
}

// CambiarEstadoUsuario da de baja (cerrando sus sesiones) o reactiva una cuenta.
func (s *Autenticacion) CambiarEstadoUsuario(ctx context.Context, ses domain.Sesion, id int64, activo bool) error {
	if err := exigirElevada(ses, s.ahora()); err != nil {
		return err
	}
	u, err := s.repo.Usuario(ctx, id)
	if errors.Is(err, domain.ErrNoExiste) {
		return domain.NoEncontrado("La cuenta no existe.")
	}
	if err != nil {
		return err
	}
	if !activo && id == ses.Usuario.ID {
		return domain.Invalido("No puedes dar de baja tu propia cuenta.")
	}
	if !activo && u.Rol == domain.RolAdmin && u.Activo {
		if n, err := s.repo.AdminsActivos(ctx); err != nil || n <= 1 {
			return errors.Join(err, domain.Invalido("Debe existir al menos un administrador activo."))
		}
	}
	if err := s.repo.CambiarEstadoUsuario(ctx, id, activo); err != nil {
		return err
	}
	if !activo {
		_, _ = s.repo.RevocarSesionesDe(ctx, id, "")
		s.olvidarUsuario(id)
	}
	accion := map[bool]string{true: "USUARIO_REACTIVADO", false: "USUARIO_DADO_DE_BAJA"}[activo]
	s.auditoria.Auditar(ctx, uid(ses.Usuario.ID), accion, "usuarios", map[string]string{"correo": u.Correo})
	return nil
}

// Bitacora devuelve las últimas entradas de la bitácora de auditoría.
func (s *Autenticacion) Bitacora(ctx context.Context) ([]domain.EntradaBitacora, error) {
	return s.repo.Bitacora(ctx, 300)
}
