// Estado guardado en el navegador y funciones puras de apoyo.
// Sólo se guarda la reserva en curso (su id; el servidor exige la sesión de su dueño para pagarla o cancelarla).
// Las cuentas, contraseñas, roles y la bitácora viven en el servidor: el navegador no guarda credenciales.

import { PARAMETROS } from "@/data/parametros";
import type { Reserva } from "@/types/cuenta";

export type Estado = { reserva: Reserva | null };

export const INICIAL: Estado = { reserva: null };

// Premio de una partida: 10 créditos por acierto; si acierta todas, 50. (Lo calcula y acredita el servidor;
// aquí sólo se usa para mostrar el resultado.)
export const premioTrivia = (aciertos: number) =>
  aciertos === PARAMETROS.preguntasTrivia ? PARAMETROS.bonoTriviaPerfecta : aciertos * PARAMETROS.creditosPorRespuesta;

export const mensaje = (e: unknown) => (e instanceof Error ? e.message : "Error inesperado.");
