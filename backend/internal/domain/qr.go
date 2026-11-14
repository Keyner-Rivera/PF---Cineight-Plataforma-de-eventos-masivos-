package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"strconv"
	"strings"
	"time"
)

// RotacionQR es cada cuánto cambia el código dinámico del QR de un boleto.
const RotacionQR = 60 * time.Second

// CodigosPorConsulta es cuántos minutos de códigos recibe el dueño del boleto en una consulta: el teléfono puede
// mostrar el QR aunque pierda la conexión en la fila de la sala durante ese tiempo.
const CodigosPorConsulta = 10

// VentanaQR es el número de ventana de RotacionQR que contiene el instante t.
func VentanaQR(t time.Time) int64 { return t.Unix() / int64(RotacionQR/time.Second) }

// CodigoDinamico es el código de 6 caracteres del QR para una ventana: HMAC-SHA256 del token y la ventana con un
// secreto que sólo conoce el servidor (en base 32: A–Z y 2–7). Sin el secreto no se puede generar un código válido
// aunque se conozca el token, y una captura de pantalla deja de servir en un par de minutos.
func CodigoDinamico(secreto []byte, token string, ventana int64) string {
	m := hmac.New(sha256.New, secreto)
	m.Write([]byte(token + ":" + strconv.FormatInt(ventana, 10)))
	return base32.StdEncoding.EncodeToString(m.Sum(nil))[:6]
}

// CodigosQR devuelve los códigos de las próximas CodigosPorConsulta ventanas a partir de la actual.
func CodigosQR(secreto []byte, token string, ahora time.Time) (desde int64, codigos []string) {
	desde = VentanaQR(ahora)
	for i := range int64(CodigosPorConsulta) {
		codigos = append(codigos, CodigoDinamico(secreto, token, desde+i))
	}
	return desde, codigos
}

// CodigoQRVigente indica si el código corresponde al token en la ventana indicada y ésta es la actual (se toleran
// la anterior y la siguiente por el cambio de minuto y relojes desfasados). Con ventana 0 (código escrito a mano)
// se prueban esas tres. La comparación es en tiempo constante.
func CodigoQRVigente(secreto []byte, token, codigo string, ventana int64, ahora time.Time) bool {
	actual := VentanaQR(ahora)
	codigo = strings.ToUpper(strings.TrimSpace(codigo))
	if len(codigo) != 6 {
		return false
	}
	candidatas := []int64{ventana}
	if ventana == 0 {
		candidatas = []int64{actual, actual - 1, actual + 1}
	}
	for _, w := range candidatas {
		if w >= actual-1 && w <= actual+1 && hmac.Equal([]byte(codigo), []byte(CodigoDinamico(secreto, token, w))) {
			return true
		}
	}
	return false
}
