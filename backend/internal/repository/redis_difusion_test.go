package repository

import "testing"

func TestRepartirEntregaYCierraStreamsLentos(t *testing.T) {
	d := &difusor{subs: map[int]map[chan string]struct{}{}}
	rapido, lento, otra := make(chan string, bufferStream), make(chan string, 1), make(chan string, 1)
	d.subs[7] = map[chan string]struct{}{rapido: {}, lento: {}}
	d.subs[8] = map[chan string]struct{}{otra: {}}

	d.repartir(7, "a")
	d.repartir(7, "b") // el buffer de "lento" ya está lleno: se cierra en lugar de frenar a los demás
	if len(rapido) != 2 || <-rapido != "a" {
		t.Fatalf("el stream al día debe recibir todos los cambios en orden")
	}
	if <-lento != "a" {
		t.Fatal("el stream lento recibe lo que cupo")
	}
	if _, abierto := <-lento; abierto {
		t.Fatal("el stream lento debe cerrarse para que el navegador reconecte con la foto completa")
	}
	if len(d.subs[7]) != 1 || len(otra) != 0 {
		t.Fatalf("sólo se da de baja el lento y los cambios no se mezclan entre funciones: %v", d.subs)
	}
}
