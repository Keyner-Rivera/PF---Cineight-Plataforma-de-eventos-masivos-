package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cineight/backend/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

// authMemoria es un doble de RepoAutenticacion con el comportamiento de las consultas reales.
type authMemoria struct {
	usuarios map[string]*domain.Credenciales // por correo
	sesiones map[string]*sesionGuardada      // por hash del token
	sig      int64
}

type sesionGuardada struct {
	domain.Sesion
	usuarioID int64
	revocada  bool
}

func nuevoAuthMemoria() *authMemoria {
	return &authMemoria{usuarios: map[string]*domain.Credenciales{}, sesiones: map[string]*sesionGuardada{}}
}

func (m *authMemoria) porID(id int64) *domain.Credenciales {
	for _, c := range m.usuarios {
		if c.ID == id {
			return c
		}
	}
	return nil
}

func (m *authMemoria) alta(correo, rol, clave string, debeCambiar bool) *domain.Credenciales {
	m.sig++
	h, _ := bcrypt.GenerateFromPassword([]byte(clave), bcrypt.MinCost)
	c := &domain.Credenciales{Usuario: domain.Usuario{ID: m.sig, Nombre: "Persona Prueba", Correo: correo, Rol: rol, Activo: true, DebeCambiarClave: debeCambiar}, Hash: string(h), ClaveDefinida: clave != ""}
	m.usuarios[correo] = c
	return c
}

func (m *authMemoria) Credenciales(_ context.Context, correo string) (domain.Credenciales, error) {
	if c, ok := m.usuarios[correo]; ok {
		return *c, nil
	}
	return domain.Credenciales{}, domain.ErrNoExiste
}
func (m *authMemoria) RegistrarFallo(_ context.Context, id int64, maximo int, bloqueo time.Duration) (*time.Time, error) {
	c := m.porID(id)
	c.Intentos++
	if c.Intentos >= maximo {
		h := time.Now().Add(bloqueo)
		c.BloqueadoHasta, c.Intentos = &h, 0
	}
	return c.BloqueadoHasta, nil
}
func (m *authMemoria) ReiniciarFallos(_ context.Context, id int64) error {
	c := m.porID(id)
	c.Intentos, c.BloqueadoHasta = 0, nil
	return nil
}
func (m *authMemoria) RegistrarCliente(_ context.Context, correo, nombre, hash string) (domain.Usuario, error) {
	if c, ok := m.usuarios[correo]; ok {
		if c.ClaveDefinida || c.Rol != domain.RolCliente {
			return domain.Usuario{}, domain.ErrYaRegistrado
		}
		c.Hash, c.ClaveDefinida = hash, true
		return c.Usuario, nil
	}
	c := m.alta(correo, domain.RolCliente, "x", false)
	c.Hash, c.Nombre = hash, nombre
	return c.Usuario, nil
}
func (m *authMemoria) CambiarClave(_ context.Context, id int64, hash string, debe bool) error {
	c := m.porID(id)
	c.Hash, c.DebeCambiarClave, c.ClaveDefinida = hash, debe, true
	return nil
}
func (m *authMemoria) PersonalSinClave(context.Context) ([]int64, error) {
	var ids []int64
	for _, c := range m.usuarios {
		if c.Rol != domain.RolCliente && !c.ClaveDefinida {
			ids = append(ids, c.ID)
		}
	}
	return ids, nil
}
func (m *authMemoria) CrearSesion(_ context.Context, hash string, uid int64, expira time.Time, _, _ string) error {
	m.sesiones[hash] = &sesionGuardada{Sesion: domain.Sesion{Hash: hash, Creada: time.Now(), UltimaActividad: time.Now(), Expira: expira}, usuarioID: uid}
	return nil
}
func (m *authMemoria) Sesion(_ context.Context, hash string) (domain.Sesion, error) {
	s, ok := m.sesiones[hash]
	if !ok || s.revocada {
		return domain.Sesion{}, domain.ErrNoExiste
	}
	out := s.Sesion
	out.Usuario = m.porID(s.usuarioID).Usuario
	return out, nil
}
func (m *authMemoria) TocarSesion(_ context.Context, hash string) error {
	m.sesiones[hash].UltimaActividad = time.Now()
	return nil
}
func (m *authMemoria) ElevarSesion(_ context.Context, hash string, hasta time.Time) error {
	m.sesiones[hash].ElevadaHasta = &hasta
	return nil
}
func (m *authMemoria) RevocarSesion(_ context.Context, hash string) error {
	if s, ok := m.sesiones[hash]; ok {
		s.revocada = true
	}
	return nil
}
func (m *authMemoria) RevocarSesionesDe(_ context.Context, uid int64, excepto string) (int64, error) {
	var n int64
	for h, s := range m.sesiones {
		if s.usuarioID == uid && h != excepto && !s.revocada {
			s.revocada = true
			n++
		}
	}
	return n, nil
}
func (m *authMemoria) Usuarios(context.Context) ([]domain.Usuario, error) { return nil, nil }
func (m *authMemoria) Usuario(_ context.Context, id int64) (domain.Usuario, error) {
	if c := m.porID(id); c != nil {
		return c.Usuario, nil
	}
	return domain.Usuario{}, domain.ErrNoExiste
}
func (m *authMemoria) CrearUsuario(_ context.Context, nombre, correo, rol, hash string) (domain.Usuario, error) {
	if _, ok := m.usuarios[correo]; ok {
		return domain.Usuario{}, domain.ErrYaRegistrado
	}
	c := m.alta(correo, rol, "x", true)
	c.Hash = hash
	return c.Usuario, nil
}
func (m *authMemoria) EditarUsuario(_ context.Context, id int64, nombre, rol string) error {
	c := m.porID(id)
	c.Nombre, c.Rol = nombre, rol
	return nil
}
func (m *authMemoria) CambiarEstadoUsuario(_ context.Context, id int64, activo bool) error {
	m.porID(id).Activo = activo
	return nil
}
func (m *authMemoria) AdminsActivos(context.Context) (int, error) {
	n := 0
	for _, c := range m.usuarios {
		if c.Rol == domain.RolAdmin && c.Activo {
			n++
		}
	}
	return n, nil
}
func (m *authMemoria) Bitacora(context.Context, int) ([]domain.EntradaBitacora, error) {
	return nil, nil
}

func armarAuth() (*Autenticacion, *authMemoria) {
	repo := nuevoAuthMemoria()
	a := NuevaAutenticacion(repo, &guardiaFija{pase: "P1"}, auditoriaMuda{})
	a.costo = bcrypt.MinCost // los tests no necesitan el costo real
	return a, repo
}

var origenPrueba = Origen{IP: "203.0.113.5", Agente: "test"}

func TestInicioDeSesionNoRevelaSiLaCuentaExiste(t *testing.T) {
	a, repo := armarAuth()
	repo.alta("ana@correo.com", domain.RolCliente, "Butaca#Roja2026", false)
	ctx := context.Background()
	_, errInexistente := a.IniciarSesion(ctx, "nadie@correo.com", "Butaca#Roja2026", origenPrueba)
	_, errClave := a.IniciarSesion(ctx, "ana@correo.com", "otraClave99", origenPrueba)
	if errInexistente == nil || errClave == nil || errInexistente.Error() != errClave.Error() || estado(errClave) != 401 {
		t.Fatalf("correo inexistente y contraseña incorrecta deben dar el mismo 401: %v / %v", errInexistente, errClave)
	}
	s, err := a.IniciarSesion(ctx, "ANA@correo.com ", "Butaca#Roja2026", origenPrueba)
	if err != nil || len(s.Token) < 40 || s.Usuario.Rol != domain.RolCliente {
		t.Fatalf("con la contraseña correcta inicia sesión con un token largo: %v %+v", err, s)
	}
	if _, ok := repo.sesiones[s.Token]; ok {
		t.Fatal("el token nunca se guarda tal cual: sólo su SHA-256")
	}
}

func TestBloqueoTrasIntentosFallidos(t *testing.T) {
	a, repo := armarAuth()
	repo.alta("ana@correo.com", domain.RolCliente, "Butaca#Roja2026", false)
	ctx := context.Background()
	for i := 1; i < domain.IntentosFallidosMax; i++ {
		if _, err := a.IniciarSesion(ctx, "ana@correo.com", "mala", origenPrueba); estado(err) != 401 {
			t.Fatalf("intento %d: se esperaba 401, fue %v", i, err)
		}
	}
	if _, err := a.IniciarSesion(ctx, "ana@correo.com", "mala", origenPrueba); estado(err) != 429 {
		t.Fatalf("el intento %d bloquea la cuenta (429), fue %v", domain.IntentosFallidosMax, err)
	}
	if _, err := a.IniciarSesion(ctx, "ana@correo.com", "Butaca#Roja2026", origenPrueba); estado(err) != 429 {
		t.Fatalf("bloqueada no entra ni con la contraseña correcta, fue %v", err)
	}
}

func TestCerrarSesionRevocaElTokenEnElServidor(t *testing.T) {
	a, repo := armarAuth()
	repo.alta("ana@correo.com", domain.RolCliente, "Butaca#Roja2026", false)
	ctx := context.Background()
	s, _ := a.IniciarSesion(ctx, "ana@correo.com", "Butaca#Roja2026", origenPrueba)
	ses, err := a.Autenticar(ctx, s.Token)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CerrarSesion(ctx, ses); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Autenticar(ctx, s.Token); estado(err) != 401 {
		t.Fatalf("tras cerrar sesión el token copiado ya no sirve (401), fue %v", err)
	}
	if _, err := a.Autenticar(ctx, "token-inventado"); estado(err) != 401 {
		t.Fatalf("un token inventado da 401, fue %v", err)
	}
}

func TestSesionVencePorInactividad(t *testing.T) {
	a, repo := armarAuth()
	repo.alta("ana@correo.com", domain.RolCliente, "Butaca#Roja2026", false)
	ctx := context.Background()
	s, _ := a.IniciarSesion(ctx, "ana@correo.com", "Butaca#Roja2026", origenPrueba)
	a.ahora = func() time.Time { return time.Now().Add(domain.InactividadSesion + time.Minute) }
	if _, err := a.Autenticar(ctx, s.Token); estado(err) != 401 {
		t.Fatalf("tras %v sin actividad la sesión vence en el servidor, fue %v", domain.InactividadSesion, err)
	}
	a.ahora = time.Now
	if _, err := a.Autenticar(ctx, s.Token); estado(err) != 401 {
		t.Fatal("una sesión vencida queda revocada aunque el reloj vuelva atrás")
	}
}

func TestCambiarClaveCierraLasDemasSesiones(t *testing.T) {
	a, repo := armarAuth()
	repo.alta("ana@correo.com", domain.RolCliente, "Butaca#Roja2026", false)
	ctx := context.Background()
	telefono, _ := a.IniciarSesion(ctx, "ana@correo.com", "Butaca#Roja2026", origenPrueba)
	laptop, _ := a.IniciarSesion(ctx, "ana@correo.com", "Butaca#Roja2026", origenPrueba)
	ses, _ := a.Autenticar(ctx, laptop.Token)
	if err := a.CambiarClave(ctx, ses, "Butaca#Roja2026", "123"); estado(err) != 400 {
		t.Fatalf("una contraseña nueva débil se rechaza, fue %v", err)
	}
	if err := a.CambiarClave(ctx, ses, "Butaca#Roja2026", "Palomitas#Dulces77"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Autenticar(ctx, telefono.Token); estado(err) != 401 {
		t.Fatal("cambiar la contraseña cierra las sesiones de los otros dispositivos")
	}
	if _, err := a.Autenticar(ctx, laptop.Token); err != nil {
		t.Fatalf("la sesión desde la que se cambió sigue abierta: %v", err)
	}
	if _, err := a.IniciarSesion(ctx, "ana@correo.com", "Palomitas#Dulces77", origenPrueba); err != nil {
		t.Fatalf("entra con la nueva contraseña: %v", err)
	}
}

func TestGestionDeUsuariosExigeReconfirmar(t *testing.T) {
	a, repo := armarAuth()
	adminC := repo.alta("admin@cineight.gt", domain.RolAdmin, "Taquilla#Segura99", false)
	ctx := context.Background()
	s, _ := a.IniciarSesion(ctx, "admin@cineight.gt", "Taquilla#Segura99", origenPrueba)
	ses, _ := a.Autenticar(ctx, s.Token)
	if _, err := a.CrearUsuario(ctx, ses, "Carlos Agente", "carlos@cineight.gt", domain.RolAgente, "Temporal#Clave55"); estado(err) != 403 {
		t.Fatalf("sin reconfirmar la contraseña no puede crear usuarios (403), fue %v", err)
	}
	if _, err := a.Reconfirmar(ctx, ses, "Taquilla#Segura99"); err != nil {
		t.Fatal(err)
	}
	ses, _ = a.Autenticar(ctx, s.Token)
	u, err := a.CrearUsuario(ctx, ses, "Carlos Agente", "carlos@cineight.gt", domain.RolAgente, "Temporal#Clave55")
	if err != nil || !u.DebeCambiarClave {
		t.Fatalf("la cuenta nueva debe cambiar su contraseña temporal al entrar: %v %+v", err, u)
	}
	if err := a.EditarUsuario(ctx, ses, adminC.ID, "Admin Sistema", domain.RolAgente); estado(err) != 400 {
		t.Fatalf("nadie cambia su propio rol, fue %v", err)
	}
	if err := a.CambiarEstadoUsuario(ctx, ses, adminC.ID, false); estado(err) != 400 {
		t.Fatalf("nadie se da de baja a sí mismo, fue %v", err)
	}
	// Dar de baja cierra sus sesiones al instante.
	sc, _ := a.IniciarSesion(ctx, "carlos@cineight.gt", "Temporal#Clave55", origenPrueba)
	if err := a.CambiarEstadoUsuario(ctx, ses, u.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Autenticar(ctx, sc.Token); estado(err) != 401 {
		t.Fatalf("la cuenta dada de baja pierde su sesión abierta, fue %v", err)
	}
	if _, err := a.IniciarSesion(ctx, "carlos@cineight.gt", "Temporal#Clave55", origenPrueba); estado(err) != 403 {
		t.Fatalf("y no puede volver a entrar (403), fue %v", err)
	}
}

func TestRegistroYCuentasAnteriores(t *testing.T) {
	a, repo := armarAuth()
	ctx := context.Background()
	if _, err := a.Registrar(ctx, "Ana López", "ana@correo.com", "Butaca#Roja2026", "", origenPrueba); estado(err) != 403 {
		t.Fatalf("registrarse exige el pase anti-bots, fue %v", err)
	}
	if _, err := a.Registrar(ctx, "Ana López", "ana@correo.com", "corta1", "P1", origenPrueba); estado(err) != 400 {
		t.Fatalf("contraseña débil: 400, fue %v", err)
	}
	// Cliente de antes de la autenticación en el servidor: tiene cuenta (saldo y boletos) pero no contraseña.
	repo.alta("luis@correo.com", domain.RolCliente, "", false)
	if _, err := a.IniciarSesion(ctx, "luis@correo.com", "loquesea1", origenPrueba); err == nil || !strings.Contains(err.Error(), "Registrarse") {
		t.Fatalf("debe indicarle que cree su contraseña con «Registrarse»: %v", err)
	}
	a.guardia = &guardiaFija{pase: "P2"}
	if _, err := a.Registrar(ctx, "Luis Pérez", "luis@correo.com", "Butaca#Roja2026", "P2", origenPrueba); err != nil {
		t.Fatalf("la cuenta anterior recibe su contraseña al registrarse: %v", err)
	}
	a.guardia = &guardiaFija{pase: "P3"}
	if _, err := a.Registrar(ctx, "Otro Intruso", "luis@correo.com", "Intruso#Clave88", "P3", origenPrueba); estado(err) != 409 {
		t.Fatalf("una cuenta con contraseña no se puede volver a registrar (409), fue %v", err)
	}
}

func TestPersonalRecibeClaveTemporal(t *testing.T) {
	a, repo := armarAuth()
	c := repo.alta("agente@cineight.gt", domain.RolAgente, "", false)
	if err := a.PrepararPersonal(context.Background(), "Inicial#Personal1"); err != nil {
		t.Fatal(err)
	}
	if !c.ClaveDefinida || !c.DebeCambiarClave {
		t.Fatalf("la cuenta del personal recibe la contraseña inicial y debe cambiarla: %+v", c)
	}
	s, err := a.IniciarSesion(context.Background(), "agente@cineight.gt", "Inicial#Personal1", origenPrueba)
	if err != nil || !s.Usuario.DebeCambiarClave {
		t.Fatalf("entra y se le pide cambiarla: %v %+v", err, s.Usuario)
	}
}
