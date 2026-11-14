// Tipos de la sesión, la cuenta de créditos y la compra.

import type { Rol } from "./catalogo";

export type Usuario = { id: number; nombre: string; correo: string; rol: Rol; debeCambiarClave: boolean };
export type Movimiento = { id: string; tipo: "recarga" | "retención" | "débito" | "liberación" | "reembolso"; monto: number; ref: string; fecha: string };
export type Boleto = { token: string; funcionId: string; asiento: string; precio: number; estado: "VÁLIDO" | "UTILIZADO" | "ANULADO" | "REEMBOLSADO"; emitido: string; titular: string; folio: string };
/** `total`: lo que calculó el servidor al reservar (el pago lo vuelve a calcular). */
export type Reserva = { id: string; titular: string; funcionId: string; asientos: string[]; expira: number; total: number };
export type Resultado = { ok: boolean; error?: string };
export type Validacion = { ok: boolean; motivo: string; boleto?: Boleto };
// verificando: esperando /api/salud · conectado: la API responde · sin-conexion: la API no respondió
export type ModoApi = "verificando" | "conectado" | "sin-conexion";
