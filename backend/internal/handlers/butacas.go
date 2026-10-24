package handlers

import (
	"net/http"

	"github.com/cineight/backend/pkg/httpx"
)

// GET /api/salas/{sala}/fuera-servicio → [{asiento, motivo, marcada_por, desde}]
func (a *API) listarFueraServicio(w http.ResponseWriter, r *http.Request) {
	lista, err := a.s.Butacas.FueraServicio(r.Context(), r.PathValue("sala"))
	if err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, lista)
}

type solicitudFueraServicio struct {
	Motivo string `json:"motivo"` // danada | fuera-servicio
	Por    string `json:"por"`    // correo del personal que la marca (bitácora)
}

// POST /api/salas/{sala}/fuera-servicio/{asiento}  {motivo, por}
func (a *API) retirarButaca(w http.ResponseWriter, r *http.Request) {
	var s solicitudFueraServicio
	if err := httpx.Leer(r, &s); err != nil {
		httpx.Error(w, 400, "Solicitud inválida.")
		return
	}
	if err := a.s.Butacas.Retirar(r.Context(), r.PathValue("sala"), r.PathValue("asiento"), s.Motivo, s.Por); err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]string{"asiento": r.PathValue("asiento"), "estado": "fuera-servicio"})
}

// DELETE /api/salas/{sala}/fuera-servicio/{asiento}?por=correo
func (a *API) habilitarButaca(w http.ResponseWriter, r *http.Request) {
	if err := a.s.Butacas.Habilitar(r.Context(), r.PathValue("sala"), r.PathValue("asiento"), r.URL.Query().Get("por")); err != nil {
		fallar(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]string{"asiento": r.PathValue("asiento"), "estado": "disponible"})
}
