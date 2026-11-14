// Respuestas de la API en Go, tal como llegan (snake_case).

export type FuncionApi = {
  id: number;
  codigo: string;
  pelicula: string;
  titulo: string;
  sala: string;
  sala_nombre: string;
  inicio: string;
  fin: string;
  formato: string;
  idioma: string;
  precio: number;
  evento: string | null;
  disponibles: number;
};
export type AsientosApi = { vendidos: string[]; retenidos: string[]; fuera_servicio?: string[]; propios?: string[] };
/** Reserva creada: el precio lo fija el servidor con la tarifa vigente de la función. */
export type ReservaApi = { reserva_id: string; expira: string; precio_unitario: number; total: number };
/** Boleto con los datos de su función (sirven para mostrarlo aunque la función ya haya terminado). */
export type BoletoApi = {
  token: string;
  funcion: string;
  asiento: string;
  precio: number;
  estado: "valido" | "utilizado" | "anulado" | "reembolsado";
  fecha: string;
  pelicula: string;
  sala: string;
  inicio: string;
  formato: string;
  idioma: string;
  /** Folio de la compra (su comprobante). */
  reserva: string;
};
/** `total`: lo cobrado, calculado por el servidor dentro de la transacción del pago. */
export type PagoApi = { reserva_id: string; boletos: BoletoApi[]; total?: number; saldo?: number; repetido?: boolean };
/** Comprobante de una compra; `firma` (HMAC del servidor) permite verificarlo con su QR. */
export type ComprobanteApi = {
  folio: string;
  fecha: string;
  cliente: string;
  correo?: string;
  pelicula: string;
  funcion: string;
  sala: string;
  inicio: string;
  formato: string;
  idioma: string;
  asientos: string[];
  precio_unitario: number;
  total: number;
  firma?: string;
};
export type VerificacionComprobanteApi = { valido: boolean; motivo: string; comprobante?: ComprobanteApi; vigentes: number; utilizados: number };
export type MovimientoApi = { id: number; tipo: "recarga" | "retencion" | "debito" | "liberacion" | "reembolso"; monto: number; referencia: string; fecha: string };
export type CuentaApi = { saldo: number; movimientos: MovimientoApi[]; boletos: BoletoApi[] };
/** Usuario autenticado (o una cuenta en la gestión de usuarios). */
export type UsuarioApi = { id: number; nombre: string; correo: string; rol: "cliente" | "agente" | "admin" | "auditor"; activo: boolean; creado: string; debe_cambiar_clave: boolean };
/** Respuesta de iniciar sesión o registrarse: el token sólo se recibe aquí. */
export type SesionIniciadaApi = { token: string; expira: string; usuario: UsuarioApi };
export type SesionApi = { usuario: UsuarioApi; expira: string; elevada_hasta: string | null };
/** Códigos dinámicos del QR de un boleto: `codigos[i]` vale en la ventana `desde + i` (de `rotacion_segundos`). */
export type CodigosQrApi = { desde: number; codigos: string[]; rotacion_segundos: number };
export type BitacoraApi = { fecha: string; usuario: string; accion: string; detalle: string };
export type TriviaApi = { acreditado: boolean; monto: number; saldo: number };
export type ValidacionApi = { ok: boolean; motivo: string; boleto?: BoletoApi; titular?: string };
export type MetricasApi = {
  vendidos_hoy: number;
  ingresos_hoy: number;
  asientos_retenidos: number;
  colas: Record<string, number>;
  creditos_trivia: number;
  creditos_canjeados: number;
  cuentas: number;
  funciones: { codigo: string; vendidos: number; aforo: number; ingresos: number }[];
};

/** Película creada desde el panel (GET /api/peliculas). */
export type PeliculaApi = {
  slug: string;
  titulo: string;
  titulo_original: string;
  categoria: "cartelera" | "estreno" | "proximamente";
  clasificacion: string;
  duracion: number;
  genero: string;
  idioma: string;
  formatos: string[];
  director: string;
  reparto: string[];
  anio: string;
  sinopsis: string;
  trailer: string | null;
  estreno_en_dias: number | null;
  poster_url: string;
};

/** Función a programar (POST /api/peliculas y POST /api/funciones). */
export type NuevaFuncionApi = { sala: string; inicio: string; formato: string; idioma: string; precio: number };

/**
 * Resultado de POST /api/captcha/verificar. `activo` = false cuando el servidor no tiene clave secreta configurada.
 * `pase`: comprobante de un solo uso para crear la cuenta en POST /api/auth/registro.
 */
export type CaptchaApi = { valido: boolean; puntaje: number; accion?: string; motivo?: string; activo: boolean; pase?: string };

/** Compras de un cliente agrupadas por película (GET /api/clientes/historial). */
export type ConsumoPeliculaApi = {
  pelicula: string;
  titulo: string;
  genero: string;
  director: string;
  reparto: string[];
  boletos: number;
  funciones: number;
  gastado: number;
  ultima: string;
};
export type HistorialClienteApi = { id: number; nombre: string; correo: string; activo: boolean; creado: string; saldo: number; peliculas: ConsumoPeliculaApi[] };

/** Lugar en la cola virtual de un preestreno: posición en la fila o turno vigente (con su vencimiento). */
/** Pronóstico para quien espera: si su lugar alcanza butaca aunque los de adelante compren el máximo, o no. */
export type PronosticoCola = "alcanza" | "incierto" | "no-alcanza";
export type PosicionColaApi = {
  posicion: number;
  en_cola: number;
  turno_habilitado: boolean;
  espera_min: number;
  expira?: string;
  /** false = minuto de gracia: si no bloquea una butaca a tiempo, el turno pasa al siguiente. */
  confirmado?: boolean;
  capacidad: number;
  butacas_libres?: number;
  pronostico?: PronosticoCola;
  agotado?: boolean;
  cerrada?: boolean;
};

/** Mensaje del stream SSE de una función (evento "asientos"). */
/** "retirado": el personal marcó la butaca fuera de servicio; "liberado" también la devuelve a la venta. */
export type EventoAsientosApi = { tipo: "bloqueado" | "liberado" | "vendido" | "retirado"; funcion: string; asientos: string[] };
export type MotivoFueraServicio = "danada" | "fuera-servicio";
export type ButacaFueraServicioApi = { asiento: string; motivo: MotivoFueraServicio; marcada_por: string; desde: string };
