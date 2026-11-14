"use client";

import { useBitacora } from "@/hooks/useDatosServidor";
import { card, thCls } from "@/components/panel/Elementos";

/* ─────────── Bitácora (RF-55, RNF-22) ─────────── */
// Registro del servidor (tabla bitacora_auditoria): inicios de sesión, intentos fallidos, bloqueos, cierres de sesión,
// cambios de contraseña, gestión de usuarios, ventas, validaciones de acceso y butacas fuera de servicio.
export function Bitacora() {
  const bitacora = useBitacora();
  return (
    <div className={`${card} overflow-x-auto`}>
      {bitacora === null ? (
        <p className="text-sm text-tenue">Cargando la bitácora…</p>
      ) : bitacora.length === 0 ? (
        <p className="text-sm text-tenue">Sin registros todavía. Las operaciones (ventas, sesiones, validaciones, cambios) aparecen aquí.</p>
      ) : (
        <table className="w-full min-w-[640px] text-sm">
          <thead className="border-b border-borde">
            <tr>
              <th className={thCls}>Fecha y hora</th>
              <th className={thCls}>Usuario</th>
              <th className={thCls}>Acción</th>
              <th className={thCls}>Detalle</th>
            </tr>
          </thead>
          <tbody>
            {bitacora.map((l, i) => (
              <tr key={i} className="border-b border-borde/60 align-top last:border-0">
                <td className="whitespace-nowrap py-2 pr-3 text-xs text-tenue">{new Date(l.fecha).toLocaleString("es-GT")}</td>
                <td className="pr-3 text-xs">{l.usuario}</td>
                <td className="pr-3">
                  <span className={/FALL|EXPIR|BLOQUEAD|BAJA/.test(l.accion) ? "badge-vino" : "badge-borde"}>{l.accion}</span>
                </td>
                <td className="max-w-xs truncate py-2 text-xs text-tenue" title={l.detalle}>
                  {l.detalle}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
