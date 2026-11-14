package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cineight/backend/internal/domain"
	"github.com/jackc/pgx/v5"
)

// Todas las consultas usan parámetros ($1, $2…): los datos del usuario nunca se concatenan al SQL, así que no hay
// inyección posible. Los tokens de sesión no se guardan: sólo su SHA-256 (si la base se filtrara, no servirían).

// esquemaAutenticacion agrega a una base existente lo necesario para la autenticación en el servidor.
const esquemaAutenticacion = `
	ALTER TABLE usuarios ADD COLUMN IF NOT EXISTS debe_cambiar_clave BOOLEAN NOT NULL DEFAULT FALSE;
	ALTER TABLE usuarios ADD COLUMN IF NOT EXISTS clave_actualizada TIMESTAMPTZ;
	CREATE TABLE IF NOT EXISTS sesiones (
	  id_hash          TEXT PRIMARY KEY,
	  usuario_id       BIGINT NOT NULL REFERENCES usuarios(id),
	  creada           TIMESTAMPTZ NOT NULL DEFAULT now(),
	  ultima_actividad TIMESTAMPTZ NOT NULL DEFAULT now(),
	  expira           TIMESTAMPTZ NOT NULL,
	  elevada_hasta    TIMESTAMPTZ,
	  revocada         BOOLEAN NOT NULL DEFAULT FALSE,
	  ip               TEXT NOT NULL DEFAULT '',
	  agente           TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS ix_sesiones_usuario ON sesiones (usuario_id) WHERE NOT revocada;
	ALTER TABLE sesiones ENABLE ROW LEVEL SECURITY;
	DELETE FROM sesiones WHERE expira < now() - interval '7 days';`

const columnasUsuario = `u.id, u.nombre, u.correo, r.nombre, u.activo, u.creado, u.debe_cambiar_clave`

func escanearUsuario(row pgx.Row, extra ...any) (domain.Usuario, error) {
	var u domain.Usuario
	err := row.Scan(append([]any{&u.ID, &u.Nombre, &u.Correo, &u.Rol, &u.Activo, &u.Creado, &u.DebeCambiarClave}, extra...)...)
	return u, err
}

// Credenciales devuelve el usuario con su hash y su estado de bloqueo (domain.ErrNoExiste si no hay cuenta).
func (p *Postgres) Credenciales(ctx context.Context, correo string) (domain.Credenciales, error) {
	var c domain.Credenciales
	u, err := escanearUsuario(p.db.QueryRow(ctx, `
		SELECT `+columnasUsuario+`, u.hash_contrasena, u.intentos_fallidos, u.bloqueado_hasta, u.clave_actualizada IS NOT NULL
		FROM usuarios u JOIN roles r ON r.id = u.rol_id WHERE u.correo = $1`, correo),
		&c.Hash, &c.Intentos, &c.BloqueadoHasta, &c.ClaveDefinida)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, domain.ErrNoExiste
	}
	c.Usuario = u
	return c, err
}

// RegistrarFallo suma un intento fallido; al llegar a `maximo` bloquea la cuenta durante `bloqueo` y reinicia la cuenta.
// Devuelve el fin del bloqueo si la cuenta quedó bloqueada.
func (p *Postgres) RegistrarFallo(ctx context.Context, usuarioID int64, maximo int, bloqueo time.Duration) (*time.Time, error) {
	var hasta *time.Time
	err := p.db.QueryRow(ctx, `
		UPDATE usuarios SET
		  bloqueado_hasta   = CASE WHEN intentos_fallidos + 1 >= $2 THEN now() + make_interval(secs => $3) ELSE bloqueado_hasta END,
		  intentos_fallidos = CASE WHEN intentos_fallidos + 1 >= $2 THEN 0 ELSE intentos_fallidos + 1 END
		WHERE id = $1 RETURNING bloqueado_hasta`, usuarioID, maximo, bloqueo.Seconds()).Scan(&hasta)
	return hasta, err
}

// ReiniciarFallos limpia los intentos fallidos tras un inicio de sesión correcto.
func (p *Postgres) ReiniciarFallos(ctx context.Context, usuarioID int64) error {
	_, err := p.db.Exec(ctx, `UPDATE usuarios SET intentos_fallidos = 0, bloqueado_hasta = NULL WHERE id = $1 AND (intentos_fallidos <> 0 OR bloqueado_hasta IS NOT NULL)`, usuarioID)
	return err
}

// RegistrarCliente crea la cuenta del cliente con su contraseña y el bono de bienvenida. Si el correo pertenece a una
// cuenta de cliente anterior a la autenticación en el servidor (sin contraseña), le asigna la contraseña y conserva su
// saldo y sus boletos. Si ya tiene contraseña devuelve domain.ErrYaRegistrado.
func (p *Postgres) RegistrarCliente(ctx context.Context, correo, nombre, hash string) (domain.Usuario, error) {
	var u domain.Usuario
	err := pgx.BeginFunc(ctx, p.db, func(tx pgx.Tx) error {
		var id int64
		err := tx.QueryRow(ctx, `
			INSERT INTO usuarios (nombre, correo, hash_contrasena, rol_id, clave_actualizada) VALUES ($1, $2, $3, 1, now())
			ON CONFLICT (correo) DO UPDATE SET hash_contrasena = EXCLUDED.hash_contrasena, nombre = EXCLUDED.nombre, clave_actualizada = now()
			WHERE usuarios.clave_actualizada IS NULL AND usuarios.rol_id = 1
			RETURNING id`, nombre, correo, hash).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrYaRegistrado
		}
		if err != nil {
			return err
		}
		if err := crearCuentaConBono(ctx, tx, id); err != nil {
			return err
		}
		u, err = escanearUsuario(tx.QueryRow(ctx, `SELECT `+columnasUsuario+` FROM usuarios u JOIN roles r ON r.id = u.rol_id WHERE u.id = $1`, id))
		return err
	})
	return u, err
}

// crearCuentaConBono abre la cuenta de créditos con el bono de bienvenida (una sola vez por usuario).
func crearCuentaConBono(ctx context.Context, tx pgx.Tx, id int64) error {
	tag, err := tx.Exec(ctx, `INSERT INTO cuentas_credito (usuario_id, saldo) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, domain.BonoBienvenida)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO movimientos_credito (usuario_id, tipo, monto, referencia) VALUES ($1, 'recarga', $2, $3)`,
		id, domain.BonoBienvenida, fmt.Sprintf("bono-bienvenida:%d", id))
	return err
}

// CambiarClave guarda el nuevo hash y quita la obligación de cambiarla.
func (p *Postgres) CambiarClave(ctx context.Context, usuarioID int64, hash string, debeCambiar bool) error {
	_, err := p.db.Exec(ctx, `UPDATE usuarios SET hash_contrasena = $2, debe_cambiar_clave = $3, clave_actualizada = now() WHERE id = $1`, usuarioID, hash, debeCambiar)
	return err
}

// PersonalSinClave lista las cuentas del personal que todavía no tienen contraseña en el servidor.
func (p *Postgres) PersonalSinClave(ctx context.Context) ([]int64, error) {
	rows, err := p.db.Query(ctx, `SELECT id FROM usuarios WHERE rol_id <> 1 AND activo AND clave_actualizada IS NULL`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

// ───────── Sesiones ─────────

// CrearSesion guarda una sesión nueva (por el hash de su token).
func (p *Postgres) CrearSesion(ctx context.Context, hash string, usuarioID int64, expira time.Time, ip, agente string) error {
	_, err := p.db.Exec(ctx, `INSERT INTO sesiones (id_hash, usuario_id, expira, ip, agente) VALUES ($1, $2, $3, $4, $5)`,
		hash, usuarioID, expira, ip, agente)
	return err
}

// Sesion devuelve una sesión no revocada con su usuario (domain.ErrNoExiste si no existe o fue revocada).
func (p *Postgres) Sesion(ctx context.Context, hash string) (domain.Sesion, error) {
	var s domain.Sesion
	u, err := escanearUsuario(p.db.QueryRow(ctx, `
		SELECT `+columnasUsuario+`, s.creada, s.ultima_actividad, s.expira, s.elevada_hasta
		FROM sesiones s JOIN usuarios u ON u.id = s.usuario_id JOIN roles r ON r.id = u.rol_id
		WHERE s.id_hash = $1 AND NOT s.revocada`, hash),
		&s.Creada, &s.UltimaActividad, &s.Expira, &s.ElevadaHasta)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, domain.ErrNoExiste
	}
	s.Usuario, s.Hash = u, hash
	return s, err
}

// TocarSesion registra actividad (renueva el plazo de inactividad).
func (p *Postgres) TocarSesion(ctx context.Context, hash string) error {
	_, err := p.db.Exec(ctx, `UPDATE sesiones SET ultima_actividad = now() WHERE id_hash = $1 AND NOT revocada`, hash)
	return err
}

// ElevarSesion marca la sesión como reconfirmada hasta `hasta`.
func (p *Postgres) ElevarSesion(ctx context.Context, hash string, hasta time.Time) error {
	_, err := p.db.Exec(ctx, `UPDATE sesiones SET elevada_hasta = $2 WHERE id_hash = $1 AND NOT revocada`, hash, hasta)
	return err
}

// RevocarSesion invalida una sesión (cierre de sesión o vencimiento).
func (p *Postgres) RevocarSesion(ctx context.Context, hash string) error {
	_, err := p.db.Exec(ctx, `UPDATE sesiones SET revocada = TRUE WHERE id_hash = $1`, hash)
	return err
}

// RevocarSesionesDe invalida todas las sesiones de un usuario salvo `excepto` (vacío = todas). Devuelve cuántas.
func (p *Postgres) RevocarSesionesDe(ctx context.Context, usuarioID int64, excepto string) (int64, error) {
	tag, err := p.db.Exec(ctx, `UPDATE sesiones SET revocada = TRUE WHERE usuario_id = $1 AND NOT revocada AND id_hash <> $2`, usuarioID, excepto)
	return tag.RowsAffected(), err
}

// ───────── Gestión de usuarios (administrador) ─────────

// Usuarios lista todas las cuentas salvo las de sistema.
func (p *Postgres) Usuarios(ctx context.Context) ([]domain.Usuario, error) {
	rows, err := p.db.Query(ctx, `SELECT `+columnasUsuario+` FROM usuarios u JOIN roles r ON r.id = u.rol_id
		WHERE u.correo <> $1 ORDER BY u.activo DESC, u.rol_id DESC, u.creado DESC LIMIT 2000`, domain.CorreoTaquilla)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Usuario, error) { return escanearUsuario(row) })
}

// Usuario devuelve una cuenta por id.
func (p *Postgres) Usuario(ctx context.Context, id int64) (domain.Usuario, error) {
	u, err := escanearUsuario(p.db.QueryRow(ctx, `SELECT `+columnasUsuario+` FROM usuarios u JOIN roles r ON r.id = u.rol_id WHERE u.id = $1 AND u.correo <> $2`, id, domain.CorreoTaquilla))
	if errors.Is(err, pgx.ErrNoRows) {
		return u, domain.ErrNoExiste
	}
	return u, err
}

// CrearUsuario da de alta una cuenta con contraseña temporal (debe cambiarla al entrar). Los clientes reciben su bono.
func (p *Postgres) CrearUsuario(ctx context.Context, nombre, correo, rol, hash string) (domain.Usuario, error) {
	var u domain.Usuario
	err := pgx.BeginFunc(ctx, p.db, func(tx pgx.Tx) error {
		var id int64
		err := tx.QueryRow(ctx, `
			INSERT INTO usuarios (nombre, correo, hash_contrasena, rol_id, debe_cambiar_clave, clave_actualizada)
			SELECT $1, $2, $3, r.id, TRUE, now() FROM roles r WHERE r.nombre = $4
			ON CONFLICT (correo) DO NOTHING RETURNING id`, nombre, correo, hash, rol).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrYaRegistrado
		}
		if err != nil {
			return err
		}
		if rol == domain.RolCliente {
			if err := crearCuentaConBono(ctx, tx, id); err != nil {
				return err
			}
		}
		u, err = escanearUsuario(tx.QueryRow(ctx, `SELECT `+columnasUsuario+` FROM usuarios u JOIN roles r ON r.id = u.rol_id WHERE u.id = $1`, id))
		return err
	})
	return u, err
}

// EditarUsuario cambia nombre y rol. Un cliente que pasa a otro rol conserva su cuenta de créditos.
func (p *Postgres) EditarUsuario(ctx context.Context, id int64, nombre, rol string) error {
	tag, err := p.db.Exec(ctx, `UPDATE usuarios SET nombre = $2, rol_id = (SELECT id FROM roles WHERE nombre = $3) WHERE id = $1 AND correo <> $4`,
		id, nombre, rol, domain.CorreoTaquilla)
	if err == nil && tag.RowsAffected() == 0 {
		err = domain.ErrNoExiste
	}
	if err == nil && rol == domain.RolCliente {
		err = pgx.BeginFunc(ctx, p.db, func(tx pgx.Tx) error { return crearCuentaConBono(ctx, tx, id) })
	}
	return err
}

// CambiarEstadoUsuario da de baja o reactiva una cuenta.
func (p *Postgres) CambiarEstadoUsuario(ctx context.Context, id int64, activo bool) error {
	tag, err := p.db.Exec(ctx, `UPDATE usuarios SET activo = $2 WHERE id = $1 AND correo <> $3`, id, activo, domain.CorreoTaquilla)
	if err == nil && tag.RowsAffected() == 0 {
		err = domain.ErrNoExiste
	}
	return err
}

// AdminsActivos cuenta los administradores activos (siempre debe quedar al menos uno).
func (p *Postgres) AdminsActivos(ctx context.Context) (int, error) {
	var n int
	err := p.db.QueryRow(ctx, `SELECT count(*) FROM usuarios WHERE rol_id = 3 AND activo`).Scan(&n)
	return n, err
}

// Bitacora devuelve las últimas entradas de la bitácora de auditoría.
func (p *Postgres) Bitacora(ctx context.Context, limite int) ([]domain.EntradaBitacora, error) {
	rows, err := p.db.Query(ctx, `
		SELECT b.fecha, coalesce(u.correo, 'sistema'), b.accion, coalesce(b.valor_nuevo::text, '')
		FROM bitacora_auditoria b LEFT JOIN usuarios u ON u.id = b.usuario_id
		ORDER BY b.fecha DESC LIMIT $1`, limite)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[domain.EntradaBitacora])
}

// BoletoDe devuelve el estado de un boleto si pertenece al usuario (domain.ErrNoExiste si no es suyo).
func (p *Postgres) BoletoDe(ctx context.Context, token string, usuarioID int64) (string, error) {
	var estado string
	err := p.db.QueryRow(ctx, `SELECT estado FROM boletos WHERE token = $1 AND usuario_id = $2`, token, usuarioID).Scan(&estado)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNoExiste
	}
	return estado, err
}
