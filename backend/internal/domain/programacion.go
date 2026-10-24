package domain

// Programación automática de la cartelera (el mismo plan que db/init.sql). Todas las salas abren a las 10:00 y
// cada una alterna dos películas; la siguiente función empieza al terminar la anterior más la limpieza, redondeado
// al cuarto de hora, mientras el inicio no pase de las 22:00.
//
// Las 16 películas en exhibición forman 8 pares (una larga con una corta) y cada par ocupa dos salas del mismo
// formato con el orden invertido: en una abre la película A y en la otra la B. Así, cualquier día, cada película
// tiene función a las 10:00 y otras repartidas hasta la noche. Como cada par suma a lo más 6 h (con limpieza),
// en cada sala caben 5 funciones antes de las 22:00 y cada película suma ⌈5/2⌉ + ⌊5/2⌋ = 5 al día.
// Lo comprueban las pruebas de programacion_test.go.

// PlanSala describe qué proyecta una sala cada día.
type PlanSala struct {
	Sala      string
	Peliculas [2]string
	Formato   string
	Precio    int
	Desde     int // minuto del día de la primera función
}

const (
	DiasProgramados     = 4        // hoy y los 3 días siguientes siempre tienen funciones
	MinFuncionesDiarias = 5        // por película
	PrimeraFuncionMin   = 10 * 60  // todas las salas abren a las 10:00
	UltimaFuncionMin    = 22 * 60  // no se programan inicios después de las 22:00
	MatineHastaMin      = 13 * 60  // antes de las 13:00 hay descuento
	DescuentoMatine     = 10       // créditos
	PrecioPreestreno    = 80       // créditos
	EventoPreestreno    = "e1"     // cola virtual
	PeliculaPreestreno  = "dune-2" // la última función de mañana en la sala del preestreno
	SalaPreestreno      = "s1"
	LibresPreestreno    = 20                     // butacas que la venta en taquilla deja libres en cada preestreno
	CorreoTaquilla      = "taquilla@cineight.gt" // usuario de sistema dueño de las ventas en taquilla
	ZonaHoraria         = "America/Guatemala"
)

// Plan de las 16 salas: 8 pares de películas, cada par en dos salas del mismo formato con el orden invertido.
var Plan = []PlanSala{
	{"s1", [2]string{"dune-2", "top-gun-maverick"}, "IMAX Láser", 70, PrimeraFuncionMin},
	{"s9", [2]string{"top-gun-maverick", "dune-2"}, "IMAX Láser", 70, PrimeraFuncionMin},
	{"s2", [2]string{"the-batman", "whiplash"}, "Dolby Atmos", 60, PrimeraFuncionMin},
	{"s12", [2]string{"whiplash", "the-batman"}, "Dolby Atmos", 60, PrimeraFuncionMin},
	{"s5", [2]string{"furiosa", "deadpool-wolverine"}, "Dolby Atmos", 60, PrimeraFuncionMin},
	{"s10", [2]string{"deadpool-wolverine", "furiosa"}, "Dolby Atmos", 60, PrimeraFuncionMin},
	{"s4", [2]string{"avatar-agua", "intensa-mente-2"}, "4DX 3D", 75, PrimeraFuncionMin},
	{"s16", [2]string{"intensa-mente-2", "avatar-agua"}, "4DX 3D", 75, PrimeraFuncionMin},
	{"s7", [2]string{"oppenheimer", "civil-war"}, "2D VIP", 80, PrimeraFuncionMin},
	{"s15", [2]string{"civil-war", "oppenheimer"}, "2D VIP", 80, PrimeraFuncionMin},
	{"s3", [2]string{"gladiador", "alien-romulus"}, "2D", 50, PrimeraFuncionMin},
	{"s13", [2]string{"alien-romulus", "gladiador"}, "2D", 50, PrimeraFuncionMin},
	{"s6", [2]string{"dark-knight", "matrix"}, "2D", 50, PrimeraFuncionMin},
	{"s8", [2]string{"matrix", "dark-knight"}, "2D", 50, PrimeraFuncionMin},
	{"s11", [2]string{"planeta-simios", "todo-en-todas-partes"}, "2D", 50, PrimeraFuncionMin},
	{"s14", [2]string{"todo-en-todas-partes", "planeta-simios"}, "2D", 50, PrimeraFuncionMin},
}

// Dobladas son las películas que se proyectan en español; las demás van subtituladas.
var Dobladas = map[string]bool{"intensa-mente-2": true, "avatar-agua": true}

// FuncionPlan es una función calculada por el plan, antes de guardarse.
type FuncionPlan struct {
	Pelicula string
	Minuto   int // minuto del día en que empieza
	Formato  string
	Idioma   string
	Precio   int
}

// FuncionesDelDia encadena las funciones de una sala en un día. `paridad` alterna qué película abre
// (los días pares la primera, los impares la segunda) para que cada una tenga horarios de mañana, tarde y noche.
func FuncionesDelDia(p PlanSala, paridad int, duracion func(pelicula string) int) []FuncionPlan {
	var out []FuncionPlan
	for n, minuto := 0, p.Desde; minuto <= UltimaFuncionMin; n++ {
		peli := p.Peliculas[(paridad+n)%2]
		dur := duracion(peli)
		if dur <= 0 {
			return out
		}
		precio := p.Precio
		if minuto < MatineHastaMin {
			precio -= DescuentoMatine
		}
		idioma := "Subtitulada"
		if Dobladas[peli] {
			idioma = "Español"
		}
		out = append(out, FuncionPlan{Pelicula: peli, Minuto: minuto, Formato: p.Formato, Idioma: idioma, Precio: precio})
		minuto = (minuto + dur + LimpiezaMin + 14) / 15 * 15
	}
	return out
}
