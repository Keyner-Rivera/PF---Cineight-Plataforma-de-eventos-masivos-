"use client";

import { useEffect } from "react";
import { FILAS, etiqueta, pelicula, sala } from "@/data/catalogo";
import { fmtFecha, fmtHora } from "@/utils/formato";
import type { Funcion } from "@/types/catalogo";
import { useApp } from "@/store/AppProvider";

/* Vista de ocupación para el personal: no permite seleccionar ni comprar */
export function MapaSoloLectura({ f }: { f: Funcion }) {
  const { ocupacion, cargarOcupacion } = useApp();
  useEffect(() => {
    cargarOcupacion([f.id]);
  }, [f.id, cargarOcupacion]);
  const s = sala(f.salaId);
  const o = ocupacion(f.id);
  const pas = new Set(s.pasillos);
  const libres = o.libres(s.aforo);
  return (
    <div>
      <p className="text-sm text-tenue">
        {pelicula(f.peliculaId).titulo} · {s.nombre} {s.tipo} · {fmtFecha(f.inicio)} {fmtHora(f.inicio)}
      </p>
      <div className="mt-3 grid grid-cols-2 gap-2 text-center sm:grid-cols-4">
        {[
          ["Vendidas", o.vendidos.size, "text-tinta"],
          ["Retenidas", o.retenidos.size, "text-arena"],
          ["Fuera de servicio", o.fuera.size, "text-apagado"],
          ["Disponibles", libres, "text-oro"],
        ].map(([t, v, c]) => (
          <div key={t as string} className="rounded-md border border-borde bg-fondo py-2">
            <p className="etiqueta">{t}</p>
            <p className={`text-xl font-black ${c}`}>{v}</p>
          </div>
        ))}
      </div>
      <div className="mt-4 overflow-x-auto rounded-md border border-borde bg-fondo p-3">
        <div className="mx-auto w-max">
          <div className="pantalla mx-auto mb-3 w-[90%]" />
          {Array.from({ length: s.filas }).map((_, fi) => (
            <div key={fi} className="mb-[3px] flex items-center gap-[3px]">
              <span className="w-4 text-center text-[9px] font-bold text-tenue">{FILAS[fi]}</span>
              {Array.from({ length: s.columnas }).map((_, c) => {
                const a = etiqueta(fi, c);
                const cls = o.fuera.has(a) ? "border border-dashed border-borde bg-fondo" : o.vendidos.has(a) ? "bg-rojo/80" : o.retenidos.has(a) ? "bg-arena/60" : "bg-butaca";
                return (
                  <span key={a} className="flex">
                    <span title={a} className={`h-4 w-4 rounded-[3px] ${cls}`} />
                    {pas.has(c + 1) && <span className="w-2.5" />}
                  </span>
                );
              })}
            </div>
          ))}
        </div>
      </div>
      <div className="mt-3 flex flex-wrap gap-4 text-[10px] font-bold uppercase tracking-[0.12em] text-tenue">
        <span className="flex items-center gap-1.5"><span className="h-3 w-3 rounded-sm bg-butaca" /> Disponible</span>
        <span className="flex items-center gap-1.5"><span className="h-3 w-3 rounded-sm bg-arena/60" /> Retenida (TTL)</span>
        <span className="flex items-center gap-1.5"><span className="h-3 w-3 rounded-sm bg-rojo/80" /> Vendida</span>
        <span className="flex items-center gap-1.5"><span className="h-3 w-3 rounded-sm border border-dashed border-borde bg-fondo" /> Fuera de servicio</span>
      </div>
      <p className="mt-2 text-[11px] text-apagado">El personal consulta la ocupación; la compra en línea es exclusiva de los clientes.</p>
    </div>
  );
}
