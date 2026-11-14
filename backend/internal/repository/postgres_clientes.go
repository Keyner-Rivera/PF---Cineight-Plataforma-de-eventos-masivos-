package repository

import (
	"context"
	"time"

	"github.com/cineight/backend/internal/domain"
)

// HistorialClientes devuelve a cada cliente con sus compras agrupadas por película.
// Sólo cuentan los boletos vigentes o usados (no los anulados ni reembolsados).
// Los clientes sin compras aparecen con la lista vacía.
func (p *Postgres) HistorialClientes(ctx context.Context) ([]domain.HistorialCliente, error) {
	rows, err := p.db.Query(ctx, `
		SELECT u.id, u.nombre, u.correo, u.activo, u.creado, coalesce(c.saldo, 0),
		       pe.slug, pe.titulo, pe.genero, pe.director, pe.reparto,
		       count(b.id), count(DISTINCT b.funcion_id), sum(b.precio), max(b.fecha_venta)
		FROM usuarios u
		LEFT JOIN cuentas_credito c ON c.usuario_id = u.id
		LEFT JOIN boletos b ON b.usuario_id = u.id AND b.estado IN ('valido','utilizado')
		LEFT JOIN funciones f ON f.id = b.funcion_id
		LEFT JOIN peliculas pe ON pe.id = f.pelicula_id
		WHERE u.rol_id = 1
		GROUP BY u.id, c.saldo, pe.id
		ORDER BY u.id, count(b.id) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	lista := []domain.HistorialCliente{}
	for rows.Next() {
		var h domain.HistorialCliente
		var (
			slug, titulo, genero, director *string
			reparto                        []string
			boletos, funciones             int
			gastado                        *int
			ultima                         *time.Time
		)
		if err := rows.Scan(&h.ID, &h.Nombre, &h.Correo, &h.Activo, &h.Creado, &h.Saldo,
			&slug, &titulo, &genero, &director, &reparto, &boletos, &funciones, &gastado, &ultima); err != nil {
			return nil, err
		}
		if n := len(lista); n == 0 || lista[n-1].ID != h.ID {
			h.Peliculas = []domain.ConsumoPelicula{}
			lista = append(lista, h)
		}
		if slug == nil {
			continue
		}
		if reparto == nil {
			reparto = []string{}
		}
		lista[len(lista)-1].Peliculas = append(lista[len(lista)-1].Peliculas, domain.ConsumoPelicula{
			Pelicula: *slug, Titulo: *titulo, Genero: *genero, Director: *director, Reparto: reparto,
			Boletos: boletos, Funciones: funciones, Gastado: *gastado, Ultima: *ultima,
		})
	}
	return lista, rows.Err()
}
