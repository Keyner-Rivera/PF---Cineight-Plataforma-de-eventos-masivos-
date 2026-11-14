package domain

import (
	"strings"
	"testing"
	"time"
)

func TestValidarClaveNueva(t *testing.T) {
	malas := map[string]string{
		"corta":                "abc12",
		"sin números":          "solamenteletras",
		"sin letras":           "1234567890123",
		"común":                "Password1",
		"espacio al inicio":    " clave12345",
		"contiene el correo":   "anaLopez2026",
		"más de 72 bytes":      strings.Repeat("a1", 37),
		"72 bytes con acentos": strings.Repeat("ñ", 36) + "1", // 73 bytes aunque sean 37 caracteres
	}
	for nombre, clave := range malas {
		if ValidarClaveNueva(clave, "ana.lopez@correo.com") == nil {
			t.Errorf("%s (%q) debería rechazarse", nombre, clave)
		}
	}
	if err := ValidarClaveNueva("Butaca#Roja2026", "ana.lopez@correo.com"); err != nil {
		t.Fatalf("una contraseña válida debería aceptarse: %v", err)
	}
}

func TestSesionVencida(t *testing.T) {
	ahora := time.Now()
	s := Sesion{UltimaActividad: ahora.Add(-14 * time.Minute), Expira: ahora.Add(time.Hour)}
	if s.Vencida(ahora) {
		t.Fatal("con 14 min sin actividad la sesión sigue vigente")
	}
	if s.UltimaActividad = ahora.Add(-16 * time.Minute); !s.Vencida(ahora) {
		t.Fatal("con más de 15 min sin actividad la sesión vence")
	}
	if s = (Sesion{UltimaActividad: ahora, Expira: ahora.Add(-time.Second)}); !s.Vencida(ahora) {
		t.Fatal("al pasar el límite absoluto la sesión vence aunque haya actividad")
	}
}
