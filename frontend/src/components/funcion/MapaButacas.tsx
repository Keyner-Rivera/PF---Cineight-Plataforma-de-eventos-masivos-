"use client";

import { Fragment, memo } from "react";
import { Accessibility, Ban, Check, Clock, Projector, Star, Wrench } from "lucide-react";
import { FILAS, etiqueta } from "@/data/catalogo";
import type { Sala } from "@/types/catalogo";
import type { EstadoConexion } from "@/hooks/useAsientosEnVivo";

export type EstadoButaca = "libre" | "sel" | "mio" | "bloqueada" | "vendida" | "pendiente" | "fuera";

// RNF-34: el estado se comunica con color y además con símbolo.
const ESTILO: Record<EstadoButaca, [string, React.ReactNode, string]> = {
  libre: ["border-butaca-borde bg-butaca hover:bg-butaca-hover hover:-translate-y-0.5", null, "Disponible"],
  pendiente: ["border-rojo/60 bg-vino/60 text-salmon animate-pulse", null, "Bloqueando…"],
  sel: ["border-rojo bg-vino text-salmon ring-2 ring-rojo/60", <Star key="s" aria-hidden className="h-2.5 w-2.5 fill-current sm:h-3 sm:w-3" />, "Tu selección"],
  mio: ["border-oro bg-vino text-oro ring-2 ring-oro/50", <Check key="c" aria-hidden className="h-2.5 w-2.5 sm:h-3 sm:w-3" />, "Retenida por ti"],
  bloqueada: ["border-arena/30 bg-arena/10 text-arena/80 cursor-not-allowed", <Clock key="r" aria-hidden className="h-2.5 w-2.5 sm:h-3 sm:w-3" />, "Bloqueado por otro cliente"],
  vendida: ["border-borde/60 bg-fondo text-apagado cursor-not-allowed", <Ban key="v" aria-hidden className="h-2.5 w-2.5 sm:h-3 sm:w-3" />, "Ocupado"],
  fuera: ["border-dashed border-borde bg-fondo/40 text-apagado/70 cursor-not-allowed", <Wrench key="f" aria-hidden className="h-2.5 w-2.5 sm:h-3 sm:w-3" />, "Fuera de servicio"],
};

type Props = {
  sala: Sala;
  estadoDe: (asiento: string) => EstadoButaca;
  /** Butacas que se resaltan en la fila/columna (selección o reserva propia). */
  resaltadas: string[];
  onElegir: (asiento: string) => void;
  conexion: EstadoConexion;
};

/** Leyenda → pantalla → butacas: la pantalla queda siempre pegada al mapa. */
export function MapaButacas({ sala: s, estadoDe, resaltadas, onElegir, conexion }: Props) {
  const pasillos = new Set(s.pasillos);
  return (
    <div className="min-w-0">
      <div className="flex flex-wrap items-center justify-center gap-3">
        <Leyenda />
        <IndicadorConexion estado={conexion} />
      </div>
      <div className="tarjeta mt-3 overflow-x-auto px-2 pb-5 pt-6 sm:px-6">
        <div className="mx-auto w-max">
          <div className="mx-auto mb-6 w-[92%]">
            <div className="pantalla" />
            <p className="-mt-1.5 text-center text-[9px] font-bold uppercase tracking-[0.3em] text-tinta/70 sm:text-[10px]">Pantalla {s.tipo}</p>
          </div>
          <div className="mb-1.5 flex items-center gap-[2px] px-1 sm:gap-1">
            <span className="w-4 sm:w-5" />
            {Array.from({ length: s.columnas }).map((_, c) => {
              const col = String(c + 1).padStart(2, "0");
              return (
                <Fragment key={c}>
                  <span className={`butaca !h-auto text-center text-[9px] font-bold sm:text-[10px] ${resaltadas.some((a) => a.endsWith(col)) ? "text-oro" : "text-apagado"}`}>{col}</span>
                  {pasillos.has(c + 1) && <span className="pasillo" />}
                </Fragment>
              );
            })}
            <span className="w-4 sm:w-5" />
          </div>
          {Array.from({ length: s.filas }).map((_, fi) => {
            const filaActiva = resaltadas.some((a) => a.startsWith(FILAS[fi]));
            return (
              <div key={fi} className={`mb-[3px] flex items-center gap-[2px] rounded-md px-1 py-0.5 sm:mb-1 sm:gap-1 ${filaActiva ? "bg-panel-2 ring-1 ring-borde" : ""}`}>
                <span className="w-4 text-center text-[10px] font-bold text-tenue sm:w-5 sm:text-[11px]">{FILAS[fi]}</span>
                {Array.from({ length: s.columnas }).map((_, c) => {
                  const a = etiqueta(fi, c);
                  return (
                    <Fragment key={a}>
                      <Butaca a={a} estado={estadoDe(a)} onElegir={onElegir} />
                      {pasillos.has(c + 1) && <span className="pasillo" />}
                    </Fragment>
                  );
                })}
                <span className="w-4 text-center text-[10px] font-bold text-tenue sm:w-5 sm:text-[11px]">{FILAS[fi]}</span>
              </div>
            );
          })}
        </div>
        <div className="mt-5 flex flex-wrap justify-center gap-x-6 gap-y-1 border-t border-borde pt-4 text-[9px] font-semibold uppercase tracking-[0.14em] text-tenue sm:text-[10px]">
          <span className="flex items-center gap-1.5">
            <Projector aria-hidden className="h-3.5 w-3.5" /> Cabina de proyección trasera (fila {FILAS[s.filas - 1]})
          </span>
          <span className="flex items-center gap-1.5">
            <Accessibility aria-hidden className="h-3.5 w-3.5" /> Acceso adaptado en fila A
          </span>
        </div>
      </div>
    </div>
  );
}

// Botón nativo memoizado: una sala tiene hasta 192 butacas; un evento SSE sólo repinta las que cambian.
// El "toque" se resuelve con CSS (active:scale-90), que corre en el compositor.
const Butaca = memo(function Butaca({ a, estado, onElegir }: { a: string; estado: EstadoButaca; onElegir: (a: string) => void }) {
  const [cls, sym, txt] = ESTILO[estado];
  const ocupada = estado === "vendida" || estado === "bloqueada" || estado === "pendiente" || estado === "fuera";
  return (
    <button
      disabled={ocupada}
      onClick={() => onElegir(a)}
      title={`${a} · ${txt}`}
      aria-label={`Butaca ${a}, ${txt}`}
      aria-pressed={estado === "sel" || estado === "mio"}
      className={`butaca grid place-items-center rounded-[5px] border text-[9px] font-bold transition-[transform,background-color,border-color] duration-150 sm:text-[11px] ${ocupada ? "" : "active:scale-90"} ${cls}`}
    >
      {sym}
    </button>
  );
});

function Leyenda() {
  const items: EstadoButaca[] = ["libre", "sel", "bloqueada", "vendida", "fuera"];
  return (
    <div className="flex w-fit max-w-full flex-wrap justify-center gap-x-5 gap-y-2 rounded-lg border border-borde bg-panel px-4 py-2.5 text-[9px] font-bold uppercase tracking-[0.14em] text-tenue sm:text-[10px]">
      {items.map((k) => (
        <span key={k} className="flex items-center gap-2">
          <span className={`grid h-4 w-4 place-items-center rounded border ${ESTILO[k][0]}`}>{ESTILO[k][1]}</span>
          <span className={k === "sel" ? "text-tinta" : ""}>{k === "bloqueada" ? "Bloqueado" : ESTILO[k][2]}</span>
        </span>
      ))}
    </div>
  );
}

// Muestra si el mapa recibe los cambios de otros clientes en tiempo real.
function IndicadorConexion({ estado }: { estado: EstadoConexion }) {
  const [punto, texto, titulo] = {
    "en-vivo": ["bg-oro", "En vivo", "Las butacas que eligen otros clientes aparecen al instante"],
    conectando: ["bg-tenue animate-pulse", "Conectando", "Abriendo la conexión en tiempo real…"],
    reconectando: ["bg-rojo animate-pulse", "Reconectando", "Se perdió la conexión en tiempo real; reintentando"],
  }[estado];
  return (
    <span title={titulo} className="flex items-center gap-2 rounded-full border border-borde bg-panel px-3 py-1.5 text-[9px] font-bold uppercase tracking-[0.14em] text-tenue sm:text-[10px]">
      <span className={`h-1.5 w-1.5 rounded-full ${punto}`} /> {texto}
    </span>
  );
}
