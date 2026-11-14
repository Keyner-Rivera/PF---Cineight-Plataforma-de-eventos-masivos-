package services

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// La base es la autoridad: si otro cliente retiene la butaca en PostgreSQL, la reserva se rechaza aunque Redis la
// dejara pasar, y no queda nada retenido (todo o nada).
func TestLaBaseDecideSiLaButacaEstaLibre(t *testing.T) {
	r, k, _ := armar(&catalogoFalso{}, nil, 500)
	ctx := context.Background()
	rt := r.retenciones.(*retencionesFalsas)
	rt.tomar("G-05", 99, "", 5*time.Minute) // otro cliente la eligió (la base la tiene a su nombre)

	_, err := r.Crear(ctx, SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"G-04", "G-05"}})
	if estado(err) != http.StatusConflict {
		t.Fatalf("la butaca retenida por otro en la base debe dar 409, fue %v", err)
	}
	if _, ok := rt.filas["G-04"]; ok || len(k.retenidas) != 0 {
		t.Fatalf("todo o nada: G-04 no debe quedar retenida ni en la base ni en Redis (%v, %v)", rt.filas, k.retenidas)
	}
}

// El precio lo fija el servidor: la respuesta de la reserva trae el precio unitario y el total de la base, y el pago
// cobra lo que calcula la transacción, no lo que diga el cliente.
func TestElPrecioLoCalculaElServidor(t *testing.T) {
	r, _, v := armar(&catalogoFalso{}, nil, 500)
	ctx := context.Background()
	res, err := r.Crear(ctx, SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"H-01", "H-02"}})
	if err != nil || res.PrecioUnitario != 60 || res.Total != 120 {
		t.Fatalf("la reserva informa el precio del servidor (60 × 2 = 120): %+v %v", res, err)
	}
	p, _, err := r.Pagar(ctx, res.ID, 7)
	if err != nil || p.Total != 120 || v.saldo != 380 {
		t.Fatalf("el pago cobra el total recalculado (120) y deja 380: %+v saldo %d %v", p, v.saldo, err)
	}
}

// Una reserva cuyo tiempo terminó en la base no se puede pagar, aunque su registro siguiera en Redis.
func TestReservaVencidaNoSePaga(t *testing.T) {
	r, k, v := armar(&catalogoFalso{}, nil, 500)
	ctx := context.Background()
	res, err := r.Crear(ctx, SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"J-01"}})
	if err != nil {
		t.Fatal(err)
	}
	rt := r.retenciones.(*retencionesFalsas)
	rt.ahora = func() time.Time { return time.Now().Add(6 * time.Minute) } // pasaron más de los 5 min de retención
	if _, _, err := r.Pagar(ctx, res.ID, 7); estado(err) != http.StatusGone {
		t.Fatalf("pagar una reserva vencida debe dar 410, fue %v", err)
	}
	if v.cobros != 0 || len(k.retenidas) != 0 {
		t.Fatalf("no se cobra y se liberan sus candados (cobros %d, retenidas %v)", v.cobros, k.retenidas)
	}
	if liberadas, _ := rt.LimpiarRetencionesVencidas(ctx); len(liberadas[1]) != 1 {
		t.Fatalf("la limpieza periódica libera la butaca vencida en la base: %v", liberadas)
	}
}

func TestComprobanteFirmadoYVerificable(t *testing.T) {
	r, _, v := armar(&catalogoFalso{}, nil, 500)
	ctx := context.Background()
	res, _ := r.Crear(ctx, SolicitudReserva{Funcion: "f1", UsuarioID: 7, Asientos: []string{"K-01", "K-02"}})
	if _, _, err := r.Pagar(ctx, res.ID, 7); err != nil {
		t.Fatal(err)
	}
	folio := "R-" + strings.Repeat("A", 12) // los folios reales vienen de token.Nuevo; se renombra para el formato
	v.boletos[folio], v.duenos[folio] = v.boletos[res.ID], 7
	s := NuevoCuentas(nil, v, auditoriaMuda{}, []byte("secreto-de-prueba-de-32-bytes-ok"))

	c, err := s.Comprobante(ctx, folio, 7)
	if err != nil || c.Total != 120 || len(c.Firma) != 16 {
		t.Fatalf("el dueño obtiene su comprobante firmado: %+v %v", c, err)
	}
	if _, err := s.Comprobante(ctx, folio, 8); estado(err) != http.StatusNotFound {
		t.Fatalf("otro usuario no ve el comprobante (404), fue %v", err)
	}
	ok, _ := s.VerificarComprobante(ctx, folio, c.Firma)
	if !ok.Valido || ok.Comprobante.Correo != "" || ok.Comprobante.Boletos != nil || ok.Comprobante.Cliente != "Ana L." || ok.Vigentes != 2 {
		t.Fatalf("la verificación pública confirma el comprobante sin correo ni códigos de entrada: %+v %+v", ok, ok.Comprobante)
	}
	falso, _ := s.VerificarComprobante(ctx, folio, "AAAAAAAAAAAAAAAA")
	if falso.Valido {
		t.Fatal("una firma falsa no verifica")
	}
	otro := NuevoCuentas(nil, v, auditoriaMuda{}, []byte("otro-secreto-distinto-de-32-bytes"))
	if f, _ := otro.VerificarComprobante(ctx, folio, c.Firma); f.Valido {
		t.Fatal("una firma hecha con otro secreto no verifica")
	}
}
