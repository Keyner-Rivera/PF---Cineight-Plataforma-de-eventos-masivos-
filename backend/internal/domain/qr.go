package domain

import (
	"strconv"
	"strings"
	"time"
)

// RotacionQR es cada cuánto cambia el código dinámico del QR de un boleto (igual que ROTACION_S del frontend).
const RotacionQR = 60 * time.Second

// VentanaQR es el número de ventana de RotacionQR que contiene el instante t.
func VentanaQR(t time.Time) int64 { return t.Unix() / int64(RotacionQR/time.Second) }

// CodigoDinamico deriva el código de 6 caracteres del QR a partir del token y la ventana. Es el mismo cálculo que
// codigoDinamico() del frontend (djb2 con XOR sobre "token:ventana", en base 36), así el servidor puede comprobar
// el código que muestra el teléfono del cliente.
func CodigoDinamico(token string, ventana int64) string {
	h := uint32(5381)
	for _, c := range token + ":" + strconv.FormatInt(ventana, 10) {
		h = h*33 ^ uint32(c)
	}
	s := strings.ToUpper(strconv.FormatUint(uint64(h), 36))
	if len(s) < 7 {
		s = strings.Repeat("0", 7-len(s)) + s
	}
	return s[len(s)-6:]
}

// CodigoQRVigente indica si el código corresponde al token y a una ventana actual. Acepta la ventana anterior y la
// siguiente para tolerar el cambio de minuto y relojes de teléfono ligeramente desfasados; una captura de pantalla
// de hace más de dos minutos ya no sirve.
func CodigoQRVigente(token, codigo string, ventana int64, ahora time.Time) bool {
	actual := VentanaQR(ahora)
	if ventana < actual-1 || ventana > actual+1 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(codigo), CodigoDinamico(token, ventana))
}
