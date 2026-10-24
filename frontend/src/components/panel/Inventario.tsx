"use client";

import { pelicula, sala } from "@/data/catalogo";
import { fmtFecha, fmtHora } from "@/utils/formato";
import { useApp } from "@/store/AppProvider";
import { useOcupacionVigente } from "@/hooks/usePanelDatos";
import { card, thCls } from "@/components/panel/Elementos";

/* ─────────── Inventario (RF-66) ─────────── */
export function Inventario() {
  const { funciones, ocupacion } = useApp();
  useOcupacionVigente();
  return (
    <div className={`${card} overflow-x-auto`}>
      <table className="w-full min-w-[640px] text-sm">
        <thead className="border-b border-borde">
          <tr>
            <th className={thCls}>Función</th>
            <th className={thCls}>Sala</th>
            <th className={`${thCls} text-right`}>Capacidad</th>
            <th className={`${thCls} text-right`}>Vendidos</th>
            <th className={`${thCls} text-right`}>Retenidos</th>
            <th className={`${thCls} text-right`}>Disponibles</th>
          </tr>
        </thead>
        <tbody>
          {funciones.map((f) => {
            const o = ocupacion(f.id);
            const cap = sala(f.salaId).aforo;
            return (
              <tr key={f.id} className="border-b border-borde/60 last:border-0">
                <td className="py-2.5 pr-3">
                  <p className="font-bold">{pelicula(f.peliculaId).titulo}</p>
                  <p className="text-[11px] text-tenue">
                    {fmtFecha(f.inicio)} {fmtHora(f.inicio)}
                  </p>
                </td>
                <td className="pr-3 text-xs">
                  {sala(f.salaId).nombre} · {sala(f.salaId).tipo}
                </td>
                <td className="pr-3 text-right">{cap}</td>
                <td className="pr-3 text-right">{o.vendidos.size}</td>
                <td className="pr-3 text-right text-arena">{o.retenidos.size}</td>
                <td className="text-right font-bold text-oro">{o.libres(cap)}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
