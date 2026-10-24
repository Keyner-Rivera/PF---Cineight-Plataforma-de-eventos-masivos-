package services

import (
	"context"
	"testing"
	"time"

	"github.com/cineight/backend/internal/domain"
)

// mantenimientoFalso guarda las butacas retiradas de la sala s1 (10×10).
type mantenimientoFalso struct{ fuera map[string]string }

func (m *mantenimientoFalso) ButacasFueraServicio(_ context.Context, sala string) ([]domain.ButacaFueraServicio, error) {
	if sala != "s1" {
		return nil, domain.ErrNoExiste
	}
	lista := []domain.ButacaFueraServicio{}
	for a, motivo := range m.fuera {
		lista = append(lista, domain.ButacaFueraServicio{Asiento: a, Motivo: motivo, Desde: time.Now()})
	}
	return lista, nil
}
func (m *mantenimientoFalso) MarcarFueraServicio(_ context.Context, sala, asiento, motivo, _ string) error {
	if sala != "s1" || asiento > "J-10" {
		return domain.ErrNoExiste
	}
	m.fuera[asiento] = motivo
	return nil
}
func (m *mantenimientoFalso) HabilitarButaca(_ context.Context, _, asiento string) error {
	if _, ok := m.fuera[asiento]; !ok {
		return domain.ErrNoExiste
	}
	delete(m.fuera, asiento)
	return nil
}
func (m *mantenimientoFalso) FuncionesVigentesDeSala(context.Context, string) (map[int]string, error) {
	return map[int]string{1: "f1"}, nil
}

type auditoriaMuda struct{}

func (auditoriaMuda) Auditar(context.Context, *int64, string, string, any) {}

func TestRetirarYHabilitarButaca(t *testing.T) {
	repo := &mantenimientoFalso{fuera: map[string]string{}}
	invalidada := false
	s := NuevoButacas(repo, NuevoAsientos(&catalogoFalso{}, nuevosCandados(), nil, time.Minute), auditoriaMuda{}, func(context.Context) { invalidada = true })
	ctx := context.Background()

	if err := s.Retirar(ctx, "s1", "C-04", "danada", "admin@cineight.gt"); err != nil || repo.fuera["C-04"] != "danada" {
		t.Fatalf("la butaca debería quedar dañada: %v %v", err, repo.fuera)
	}
	if !invalidada {
		t.Fatal("retirar una butaca cambia las disponibles: la cartelera en caché debe invalidarse")
	}
	if err := s.Retirar(ctx, "s1", "C-05", "rota", ""); estado(err) != 400 {
		t.Fatalf("un motivo desconocido es 400, fue %v", err)
	}
	if err := s.Retirar(ctx, "s1", "Z-99", "danada", ""); estado(err) != 400 {
		t.Fatalf("una etiqueta inválida es 400, fue %v", err)
	}
	if err := s.Retirar(ctx, "s1", "P-16", "danada", ""); estado(err) != 404 {
		t.Fatalf("una butaca fuera de la sala es 404, fue %v", err)
	}
	if _, err := s.FueraServicio(ctx, "s99"); estado(err) != 404 {
		t.Fatalf("una sala inexistente es 404, fue %v", err)
	}
	if err := s.Habilitar(ctx, "s1", "C-04", ""); err != nil || len(repo.fuera) != 0 {
		t.Fatalf("la butaca debería volver a la venta: %v %v", err, repo.fuera)
	}
	if err := s.Habilitar(ctx, "s1", "C-04", ""); estado(err) != 404 {
		t.Fatalf("habilitar una butaca que no estaba retirada es 404, fue %v", err)
	}
}
