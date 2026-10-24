// Package repository es la capa de acceso a datos: PostgreSQL (fuente de verdad de ventas y créditos)
// y Redis (estado efímero: bloqueos de butacas, reservas, cola virtual, rate limiting y Pub/Sub).
// Aquí no hay reglas de negocio: sólo consultas. Las decisiones viven en internal/services.
package repository

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/cineight/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres agrupa las consultas a la base de datos relacional.
type Postgres struct {
	db *pgxpool.Pool
}

// NuevoPostgres crea el repositorio sobre un pool de conexiones.
func NuevoPostgres(db *pgxpool.Pool) *Postgres { return &Postgres{db: db} }

// AbrirPool crea el pool de conexiones (pgx + puddle) con límites explícitos: como mucho maxConns conexiones
// abiertas, para no agotar los puertos ni el límite del servidor de PostgreSQL (o del pooler de Supabase), y
// minConns siempre listas para que la primera consulta tras un rato sin tráfico no espere a conectar.
func AbrirPool(ctx context.Context, dsn string, maxConns, minConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = maxConns
	cfg.MinConns = minConns
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = time.Minute
	return pgxpool.NewWithConfig(ctx, cfg)
}

// EsViolacionUnica indica si err proviene de una restricción UNIQUE (código 23505).
func EsViolacionUnica(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// ───────── Catálogo ─────────

// Funciones devuelve la cartelera vigente con las butacas disponibles de cada función
// (aforo menos las vendidas y las fuera de servicio, sin contar dos veces una butaca vendida y luego retirada).
func (p *Postgres) Funciones(ctx context.Context) ([]domain.Funcion, error) {
	rows, err := p.db.Query(ctx, `
		SELECT f.id, f.codigo, p.slug, p.titulo, s.codigo, s.nombre, f.inicio, f.fin, f.formato, f.idioma, f.precio, f.evento,
		       s.aforo - (SELECT count(*) FROM (
		           SELECT b.asiento FROM boletos b WHERE b.funcion_id = f.id AND b.estado IN ('valido','utilizado')
		           UNION SELECT o.asiento FROM butacas_fuera_servicio o WHERE o.sala_id = f.sala_id) ocupadas)
		FROM funciones f JOIN peliculas p ON p.id = f.pelicula_id JOIN salas s ON s.id = f.sala_id
		WHERE f.fin > now() AND f.estado = 'programada' ORDER BY f.inicio`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Funcion, error) {
		var f domain.Funcion
		err := row.Scan(&f.ID, &f.Codigo, &f.Pelicula, &f.Titulo, &f.Sala, &f.SalaNombre, &f.Inicio, &f.Fin, &f.Formato, &f.Idioma, &f.Precio, &f.Evento, &f.Disponibles)
		return f, err
	})
}

// FuncionID acepta el código público (f1) o el id numérico.
func (p *Postgres) FuncionID(ctx context.Context, ref string) (int, error) {
	if n, err := strconv.Atoi(ref); err == nil {
		var id int
		if err := p.db.QueryRow(ctx, `SELECT id FROM funciones WHERE id = $1`, n).Scan(&id); err != nil {
			return 0, domain.ErrNoExiste
		}
		return id, nil
	}
	var id int
	if err := p.db.QueryRow(ctx, `SELECT id FROM funciones WHERE codigo = $1`, ref).Scan(&id); err != nil {
		return 0, domain.ErrNoExiste
	}
	return id, nil
}

// CodigoFuncion devuelve el código público de una función.
func (p *Postgres) CodigoFuncion(ctx context.Context, id int) (string, error) {
	var codigo string
	err := p.db.QueryRow(ctx, `SELECT codigo FROM funciones WHERE id = $1`, id).Scan(&codigo)
	return codigo, err
}

// AsientosVendidos lista las butacas con boleto válido o utilizado.
func (p *Postgres) AsientosVendidos(ctx context.Context, funcionID int) ([]string, error) {
	rows, err := p.db.Query(ctx, `SELECT asiento FROM boletos WHERE funcion_id = $1 AND estado IN ('valido','utilizado')`, funcionID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// FueraServicio lista las butacas retiradas de la venta en la sala de la función.
func (p *Postgres) FueraServicio(ctx context.Context, funcionID int) ([]string, error) {
	rows, err := p.db.Query(ctx, `
		SELECT o.asiento FROM butacas_fuera_servicio o JOIN funciones f ON f.sala_id = o.sala_id WHERE f.id = $1`, funcionID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// VendidosVigentes devuelve las butacas vendidas y fuera de servicio de todas las funciones vigentes, por id,
// en una sola consulta (el panel la usa en lugar de pedir función por función).
func (p *Postgres) VendidosVigentes(ctx context.Context) (codigos map[int]string, ocupacion map[int]domain.Ocupacion, err error) {
	rows, err := p.db.Query(ctx, `
		SELECT f.id, f.codigo,
		       coalesce((SELECT array_agg(b.asiento) FROM boletos b WHERE b.funcion_id = f.id AND b.estado IN ('valido','utilizado')), '{}'),
		       coalesce((SELECT array_agg(o.asiento) FROM butacas_fuera_servicio o WHERE o.sala_id = f.sala_id), '{}')
		FROM funciones f WHERE f.fin > now()`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	codigos, ocupacion = map[int]string{}, map[int]domain.Ocupacion{}
	for rows.Next() {
		var id int
		var codigo string
		var o domain.Ocupacion
		if err := rows.Scan(&id, &codigo, &o.Vendidos, &o.FueraServicio); err != nil {
			return nil, nil, err
		}
		codigos[id], ocupacion[id] = codigo, o
	}
	return codigos, ocupacion, rows.Err()
}

// ContextoReserva reúne lo necesario para validar una reserva: si la función es un preestreno,
// cuántas de las butacas pedidas ya no se pueden vender (vendidas o fuera de servicio) y cuántos boletos tiene ya el usuario.
// Devuelve domain.ErrFuncionIniciada si la función ya empezó o no está programada, y domain.ErrAsientoInexistente
// si alguna butaca no cabe en las filas y columnas de la sala.
func (p *Postgres) ContextoReserva(ctx context.Context, funcionID int, asientos []string, usuarioID int64) (evento *string, vendidos, propios int, err error) {
	var abierta bool
	var inexistentes int
	err = p.db.QueryRow(ctx, `
		SELECT f.evento, f.inicio > now() AND f.estado = 'programada',
		       (SELECT count(*) FROM unnest($2::text[]) a
		        WHERE ascii(left(a, 1)) - 64 > s.filas OR substring(a FROM 3)::int NOT BETWEEN 1 AND s.columnas),
		       (SELECT count(*) FROM boletos b WHERE b.funcion_id = f.id AND b.asiento = ANY($2) AND b.estado IN ('valido','utilizado'))
		         + (SELECT count(*) FROM butacas_fuera_servicio o WHERE o.sala_id = f.sala_id AND o.asiento = ANY($2)),
		       (SELECT count(*) FROM boletos b WHERE b.funcion_id = f.id AND b.usuario_id = $3 AND b.estado IN ('valido','utilizado'))
		FROM funciones f JOIN salas s ON s.id = f.sala_id WHERE f.id = $1`, funcionID, asientos, usuarioID).Scan(&evento, &abierta, &inexistentes, &vendidos, &propios)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		err = domain.ErrNoExiste
	case err == nil && !abierta:
		err = domain.ErrFuncionIniciada
	case err == nil && inexistentes > 0:
		err = domain.ErrAsientoInexistente
	}
	return
}

// InventarioEvento devuelve la función por venir del preestreno y cuántas de sus butacas siguen a la venta
// (aforo menos vendidas y fuera de servicio). domain.ErrNoExiste si no hay ninguna.
func (p *Postgres) InventarioEvento(ctx context.Context, evento string) (domain.InventarioEvento, error) {
	var inv domain.InventarioEvento
	err := p.db.QueryRow(ctx, `
		SELECT f.id, f.inicio > now() AND f.estado = 'programada',
		       s.aforo - (SELECT count(*) FROM (
		           SELECT b.asiento FROM boletos b WHERE b.funcion_id = f.id AND b.estado IN ('valido','utilizado')
		           UNION SELECT o.asiento FROM butacas_fuera_servicio o WHERE o.sala_id = f.sala_id) ocupadas)
		FROM funciones f JOIN salas s ON s.id = f.sala_id
		WHERE f.evento = $1 AND f.fin > now() ORDER BY f.inicio LIMIT 1`, evento).Scan(&inv.FuncionID, &inv.Abierta, &inv.Libres)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNoExiste
	}
	return inv, err
}

// BoletosDeUsuario cuenta los boletos vigentes o usados de un usuario en una función.
func (p *Postgres) BoletosDeUsuario(ctx context.Context, funcionID int, usuarioID int64) (int, error) {
	var n int
	err := p.db.QueryRow(ctx, `SELECT count(*) FROM boletos WHERE funcion_id = $1 AND usuario_id = $2 AND estado IN ('valido','utilizado')`,
		funcionID, usuarioID).Scan(&n)
	return n, err
}

// ───────── Bitácora ─────────

// Auditar registra una operación crítica (RNF-22). Los fallos se ignoran: la bitácora no debe tumbar la operación.
func (p *Postgres) Auditar(ctx context.Context, usuarioID *int64, accion, entidad string, valor any) {
	_, _ = p.db.Exec(ctx, `INSERT INTO bitacora_auditoria (usuario_id, accion, entidad, valor_nuevo) VALUES ($1, $2, $3, $4)`,
		usuarioID, accion, entidad, valor)
}
