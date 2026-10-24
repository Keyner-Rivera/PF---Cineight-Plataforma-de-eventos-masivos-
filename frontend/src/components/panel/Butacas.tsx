"use client";

import { useEffect, useState } from "react";
import { Armchair, RotateCcw, Wrench } from "lucide-react";
import { FILAS, SALAS, etiqueta } from "@/data/catalogo";
import { fmtFecha, fmtHora } from "@/utils/formato";
import { api } from "@/services/api";
import { mensaje } from "@/store/estado";
import { useApp } from "@/store/AppProvider";
import type { ButacaFueraServicioApi, MotivoFueraServicio } from "@/types/api";
import { Boton } from "@/components/ui/Boton";
import { Caja } from "@/components/panel/Elementos";

const MOTIVOS: Record<MotivoFueraServicio, string> = { danada: "Dañada", "fuera-servicio": "Fuera de servicio" };

/* ─────────── Butacas dañadas o fuera de servicio ─────────── */
// Una butaca marcada no se vende en ninguna función de su sala hasta que se habilite de nuevo.
export function Butacas() {
  const { usuario, toast, registrar } = useApp();
  const [salaId, setSalaId] = useState(SALAS[0].id);
  const [motivo, setMotivo] = useState<MotivoFueraServicio>("danada");
  const [lista, setLista] = useState<ButacaFueraServicioApi[] | null>(null);
  const [error, setError] = useState("");
  const [version, setVersion] = useState(0); // cambia para volver a pedir la lista (reintento o tras un cambio)
  const [enCurso, setEnCurso] = useState<string | null>(null);
  const s = SALAS.find((x) => x.id === salaId)!;
  const pas = new Set(s.pasillos);
  const marcadas = new Map((lista ?? []).map((b) => [b.asiento, b]));

  useEffect(() => {
    let vivo = true;
    api
      .fueraServicio(salaId)
      .then((datos) => {
        if (!vivo) return;
        setLista(datos);
        setError("");
      })
      .catch((e) => vivo && setError(mensaje(e)));
    return () => {
      vivo = false;
    };
  }, [salaId, version]);

  const cambiarSala = (id: string) => {
    setLista(null);
    setError("");
    setSalaId(id);
  };

  const alternar = async (asiento: string) => {
    if (enCurso) return;
    const actual = marcadas.get(asiento);
    setEnCurso(asiento);
    try {
      if (actual) {
        await api.habilitarButaca(salaId, asiento, usuario?.correo ?? "");
        registrar("BUTACA_HABILITADA", `${s.nombre} · ${asiento}`);
        toast(`Butaca ${asiento} habilitada de nuevo.`, "ok");
      } else {
        await api.retirarButaca(salaId, asiento, motivo, usuario?.correo ?? "");
        registrar("BUTACA_FUERA_SERVICIO", `${s.nombre} · ${asiento} · ${MOTIVOS[motivo]}`);
        toast(`Butaca ${asiento} marcada como «${MOTIVOS[motivo].toLowerCase()}».`, "ok");
      }
    } catch (e) {
      toast(mensaje(e), "error");
    } finally {
      setEnCurso(null);
      setVersion((v) => v + 1);
    }
  };

  return (
    <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_360px]">
      <Caja
        icono={Armchair}
        titulo="Mapa de la sala"
        sub="Toca una butaca disponible para retirarla de la venta, o una marcada para habilitarla."
        accion={
          <label className="etiqueta flex items-center gap-2">
            Sala
            <select value={salaId} onChange={(e) => cambiarSala(e.target.value)} className="campo !mt-0 !w-auto !py-1.5 normal-case tracking-normal">
              {SALAS.map((x) => (
                <option key={x.id} value={x.id}>
                  {x.nombre} · {x.tipo}
                </option>
              ))}
            </select>
          </label>
        }
      >
        <div role="radiogroup" aria-label="Motivo al marcar" className="mb-4 flex flex-wrap items-center gap-2">
          <span className="etiqueta mr-1">Marcar como</span>
          {(Object.keys(MOTIVOS) as MotivoFueraServicio[]).map((k) => (
            <button
              key={k}
              role="radio"
              aria-checked={motivo === k}
              onClick={() => setMotivo(k)}
              className={`rounded-full border px-3 py-1.5 text-xs font-bold transition-colors ${motivo === k ? "border-rojo bg-vino text-salmon" : "border-borde bg-fondo text-tenue hover:border-arena"}`}
            >
              {MOTIVOS[k]}
            </button>
          ))}
        </div>

        {error ? (
          <div className="rounded-md border border-rojo/50 bg-vino/40 p-4 text-sm">
            <p className="text-salmon">{error}</p>
            <Boton variante="borde" tamano="sm" icono={RotateCcw} onClick={() => setVersion((v) => v + 1)} className="mt-3">
              Reintentar
            </Boton>
          </div>
        ) : (
          <div className={`overflow-x-auto rounded-md border border-borde bg-fondo p-3 transition-opacity ${lista ? "" : "opacity-50"}`}>
            <div className="mx-auto w-max">
              <div className="pantalla mx-auto mb-3 w-[90%]" />
              {Array.from({ length: s.filas }).map((_, fi) => (
                <div key={fi} className="mb-[3px] flex items-center gap-[3px]">
                  <span className="w-4 text-center text-[9px] font-bold text-tenue">{FILAS[fi]}</span>
                  {Array.from({ length: s.columnas }).map((_, c) => {
                    const a = etiqueta(fi, c);
                    const b = marcadas.get(a);
                    const txt = b ? `${MOTIVOS[b.motivo]} · toca para habilitar` : "Disponible · toca para marcar";
                    return (
                      <span key={a} className="flex">
                        <button
                          disabled={!lista || enCurso !== null}
                          onClick={() => alternar(a)}
                          title={`${a} · ${txt}`}
                          aria-label={`Butaca ${a}, ${txt}`}
                          aria-pressed={!!b}
                          className={`grid h-5 w-5 place-items-center rounded-[4px] border transition-[transform,background-color] active:scale-90 disabled:cursor-wait ${
                            b
                              ? b.motivo === "danada"
                                ? "border-rojo/70 bg-vino text-salmon"
                                : "border-dashed border-arena/70 bg-arena/15 text-arena"
                              : "border-butaca-borde bg-butaca hover:bg-butaca-hover"
                          } ${enCurso === a ? "animate-pulse" : ""}`}
                        >
                          {b && <Wrench aria-hidden className="h-2.5 w-2.5" />}
                        </button>
                        {pas.has(c + 1) && <span className="w-2.5" />}
                      </span>
                    );
                  })}
                </div>
              ))}
            </div>
          </div>
        )}

        <div className="mt-3 flex flex-wrap gap-4 text-[10px] font-bold uppercase tracking-[0.12em] text-tenue">
          <span className="flex items-center gap-1.5"><span className="h-3 w-3 rounded-sm bg-butaca" /> Disponible</span>
          <span className="flex items-center gap-1.5"><span className="h-3 w-3 rounded-sm border border-rojo/70 bg-vino" /> Dañada</span>
          <span className="flex items-center gap-1.5"><span className="h-3 w-3 rounded-sm border border-dashed border-arena/70 bg-arena/15" /> Fuera de servicio</span>
        </div>
        <p className="mt-2 text-[11px] text-apagado">Aplica a todas las funciones de la sala. Los boletos ya vendidos de esa butaca siguen siendo válidos.</p>
      </Caja>

      <Caja icono={Wrench} titulo="Retiradas de la venta" sub={`${s.nombre} · ${lista ? `${lista.length} de ${s.aforo} butacas` : "cargando…"}`}>
        {lista && lista.length === 0 && <p className="text-sm text-tenue">Todas las butacas de esta sala están disponibles.</p>}
        <ul className="space-y-2">
          {(lista ?? []).map((b) => (
            <li key={b.asiento} className="flex items-center justify-between gap-3 rounded-md border border-borde bg-fondo px-3 py-2">
              <div className="min-w-0">
                <p className="text-sm font-bold">
                  {b.asiento} <span className={`ml-1 text-[11px] font-semibold ${b.motivo === "danada" ? "text-salmon" : "text-arena"}`}>{MOTIVOS[b.motivo]}</span>
                </p>
                <p className="truncate text-[11px] text-apagado">
                  {fmtFecha(b.desde)} {fmtHora(b.desde)}
                  {b.marcada_por && ` · ${b.marcada_por}`}
                </p>
              </div>
              <Boton variante="borde" tamano="sm" icono={RotateCcw} className="shrink-0" cargando={enCurso === b.asiento} disabled={enCurso !== null} onClick={() => alternar(b.asiento)}>
                Habilitar
              </Boton>
            </li>
          ))}
        </ul>
      </Caja>
    </div>
  );
}
