// Cliente HTTP de la API en Go (backend/cmd/api). La URL base sale de NEXT_PUBLIC_API_URL.
// Si la variable no está definida o la API no responde, la interfaz muestra "Servicio no disponible" (ver `modoApi`).

import type {
  AsientosApi,
  ButacaFueraServicioApi,
  CaptchaApi,
  CuentaApi,
  FuncionApi,
  HistorialClienteApi,
  NuevaFuncionApi,
  PeliculaApi,
  MetricasApi,
  PagoApi,
  PosicionColaApi,
  ReservaApi,
  SesionApi,
  TriviaApi,
  MotivoFueraServicio,
  ValidacionApi,
} from "@/types/api";

const BASE = (process.env.NEXT_PUBLIC_API_URL ?? "").replace(/\/+$/, "");

export const apiConfigurada = BASE !== "";
/** La API corre en esta misma computadora (desarrollo): responde al instante o no está levantada. */
export const apiLocal = /^https?:\/\/(localhost|127\.0\.0\.1)(:|\/|$)/.test(BASE);

/** Error de la API con su código HTTP (0 = sin conexión o tiempo agotado). */
export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public datos?: unknown,
  ) {
    super(message);
  }
}

async function pedir<T>(ruta: string, init: RequestInit = {}, timeoutMs = 8000): Promise<T> {
  const ctrl = new AbortController();
  const t = setTimeout(() => ctrl.abort(), timeoutMs);
  try {
    const res = await fetch(BASE + ruta, {
      ...init,
      signal: ctrl.signal,
      cache: "no-store",
      headers: init.body ? { "Content-Type": "application/json", ...init.headers } : init.headers,
    });
    const datos = await res.json().catch(() => ({}));
    if (!res.ok) throw new ApiError(res.status, (datos as { error?: string; motivo?: string }).error ?? `Error ${res.status}`, datos);
    return datos as T;
  } catch (e) {
    if (e instanceof ApiError) throw e;
    throw new ApiError(0, ctrl.signal.aborted ? "La API no respondió a tiempo." : "No se pudo conectar con la API.");
  } finally {
    clearTimeout(t);
  }
}

const post = <T>(ruta: string, cuerpo?: unknown) => pedir<T>(ruta, { method: "POST", body: cuerpo === undefined ? undefined : JSON.stringify(cuerpo) });
const del = <T>(ruta: string, cuerpo?: unknown) => pedir<T>(ruta, { method: "DELETE", body: cuerpo === undefined ? undefined : JSON.stringify(cuerpo) });
const seg = encodeURIComponent;

export const api = {
  salud: (timeoutMs = 2500) => pedir<{ estado: string }>("/api/salud", {}, timeoutMs),

  // Catálogo y mapa de butacas
  funciones: () => pedir<FuncionApi[]>("/api/funciones"),
  asientos: (funcion: string, usuarioId?: number) => pedir<AsientosApi>(`/api/funciones/${seg(funcion)}/asientos${usuarioId ? `?usuario=${usuarioId}` : ""}`),
  /** Ocupación de todas las funciones vigentes en una sola respuesta (panel, listados grandes). */
  ocupacion: () => pedir<Record<string, AsientosApi>>("/api/ocupacion"),
  /** Candado individual de una butaca (SET NX EX en Redis) mientras el cliente arma su selección. */
  bloquearAsiento: (funcion: string, asiento: string, usuarioId: number) =>
    post<{ asiento: string; estado: string }>(`/api/funciones/${seg(funcion)}/asientos/${seg(asiento)}/bloqueo`, { usuario_id: usuarioId }),
  /** Suelta butacas bloqueadas por el usuario (sólo las que siguen a su nombre). */
  // keepalive: la solicitud se completa aunque el usuario cierre la pestaña o cambie de página.
  desbloquearAsientos: (funcion: string, asientos: string[], usuarioId: number) =>
    pedir<{ estado: string }>(`/api/funciones/${seg(funcion)}/bloqueos`, { method: "DELETE", keepalive: true, body: JSON.stringify({ usuario_id: usuarioId, asientos }) }),
  /** URL del stream SSE con los cambios de butacas de una función. */
  urlStream: (funcion: string) => `${BASE}/api/funciones/${seg(funcion)}/stream`,

  // Catálogo editable (agente operativo y administrador)
  peliculas: () => pedir<PeliculaApi[]>("/api/peliculas"),
  /** Crea la película con su póster (data URL) y sus funciones en una sola transacción. */
  crearPelicula: (datos: Omit<PeliculaApi, "slug" | "poster_url"> & { poster: string; funciones: NuevaFuncionApi[]; creada_por: string }) =>
    pedir<{ slug: string; funciones: string[] }>("/api/peliculas", { method: "POST", body: JSON.stringify(datos) }, 20000),
  programarFuncion: (pelicula: string, f: NuevaFuncionApi) => post<{ codigo: string }>("/api/funciones", { pelicula, ...f }),
  /** Butacas dañadas o fuera de servicio de una sala: no se venden en ninguna de sus funciones. */
  fueraServicio: (sala: string) => pedir<ButacaFueraServicioApi[]>(`/api/salas/${seg(sala)}/fuera-servicio`),
  retirarButaca: (sala: string, asiento: string, motivo: MotivoFueraServicio, por: string) =>
    post<{ estado: string }>(`/api/salas/${seg(sala)}/fuera-servicio/${seg(asiento)}`, { motivo, por }),
  habilitarButaca: (sala: string, asiento: string, por: string) => del<{ estado: string }>(`/api/salas/${seg(sala)}/fuera-servicio/${seg(asiento)}?por=${seg(por)}`),
  /** URL absoluta de un recurso de la API (p. ej. el póster de una película creada desde el panel). */
  url: (ruta: string) => BASE + ruta,

  // Compra
  crearReserva: (funcion: string, usuarioId: number, asientos: string[]) => post<ReservaApi>("/api/reservas", { funcion, usuario_id: usuarioId, asientos }),
  pagar: (reservaId: string) => post<PagoApi>(`/api/reservas/${seg(reservaId)}/pagar`),
  cancelarReserva: (reservaId: string) => del<{ estado: string }>(`/api/reservas/${seg(reservaId)}`),

  // Cuentas y créditos
  /** Vincula al cliente con su cuenta. `pase` (de verificarCaptcha) es obligatorio si la cuenta aún no existe. */
  sesion: (correo: string, nombre: string, pase?: string) => post<SesionApi>("/api/sesion", { correo, nombre, ...(pase ? { pase } : {}) }),
  cuenta: (usuarioId: number) => pedir<CuentaApi>(`/api/usuarios/${usuarioId}/cuenta`),
  acreditarTrivia: (usuarioId: number, partida: string, aciertos: number) => post<TriviaApi>("/api/creditos/trivia", { usuario_id: usuarioId, partida, aciertos }),

  // Cola virtual de preestrenos (Redis). Quien espera consulta su posición cada pocos segundos o pierde su lugar.
  entrarCola: (evento: string, usuarioId: number, pase?: string) => post<PosicionColaApi>(`/api/cola/${seg(evento)}`, { usuario_id: usuarioId, ...(pase ? { pase } : {}) }),
  posicionCola: (evento: string, usuarioId: number) => pedir<PosicionColaApi>(`/api/cola/${seg(evento)}/${usuarioId}`),
  salirCola: (evento: string, usuarioId: number) => del<{ estado: string }>(`/api/cola/${seg(evento)}/${usuarioId}`),

  // Operación
  /** Marca el boleto como usado si `codigo` es el código dinámico vigente de su QR (ventana de un minuto). */
  validarBoleto: (token: string, codigo: string, ventana: number) => post<ValidacionApi>(`/api/boletos/${seg(token)}/validar`, { codigo, ventana }),
  metricas: () => pedir<MetricasApi>("/api/metricas"),
  /** Compras de cada cliente agrupadas por película (estadísticas del administrador). */
  historialClientes: () => pedir<HistorialClienteApi[]>("/api/clientes/historial"),

  // Seguridad
  /** Verifica en el servidor un token de reCAPTCHA v3 antes de procesar credenciales. */
  verificarCaptcha: (token: string, accion: string) => post<CaptchaApi>("/api/captcha/verificar", { token, accion }),
};
