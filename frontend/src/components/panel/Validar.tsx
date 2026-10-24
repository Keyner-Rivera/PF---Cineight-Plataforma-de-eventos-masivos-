"use client";

import { Camera, CameraOff, CircleCheck, CircleX, Keyboard, ScanLine } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import dynamic from "next/dynamic";
import { m } from "framer-motion";
import { pelicula, sala } from "@/data/catalogo";
import { fmtHora } from "@/utils/formato";
import { useApp } from "@/store/AppProvider";
import type { Validacion } from "@/types/cuenta";
import { Boton } from "@/components/ui/Boton";
import { card } from "@/components/panel/Elementos";

// El escáner (y su lector de QR) sólo se descarga al abrir la cámara.
const EscanerQR = dynamic(() => import("@/components/panel/EscanerQR").then((x) => x.EscanerQR), { ssr: false });

/** Con la cámara, el resultado se muestra este tiempo y luego se lee el siguiente boleto. */
const PAUSA_RESULTADO_MS = 2500;

/* ─────────── Validar acceso (RF-09, RF-27, RF-28) ─────────── */
// Tres formas de leer un boleto: la cámara del dispositivo, un lector QR en modo teclado o el código escrito a mano.
export function Validar() {
  const { validarBoleto, funciones } = useApp();
  const [camara, setCamara] = useState(false);
  const [codigo, setCodigo] = useState("");
  const [res, setRes] = useState<Validacion | null>(null);
  const [validando, setValidando] = useState(false);
  const [pausa, setPausa] = useState(false); // la cámara deja de leer mientras se muestra el resultado
  const pausaT = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(pausaT.current), []);

  // La validación es atómica en PostgreSQL (POST /api/boletos/{token}/validar) y exige el código dinámico vigente.
  const validar = useCallback(
    async (entrada: string) => {
      if (!entrada.trim()) return;
      setValidando(true);
      try {
        const r = await validarBoleto(entrada.trim());
        setRes(r);
        navigator.vibrate?.(r.ok ? 120 : [80, 60, 80, 60, 80]);
      } finally {
        setValidando(false);
      }
    },
    [validarBoleto],
  );

  const leerConCamara = useCallback(
    (texto: string) => {
      setPausa(true);
      validar(texto).finally(() => {
        clearTimeout(pausaT.current);
        pausaT.current = setTimeout(() => setPausa(false), PAUSA_RESULTADO_MS);
      });
    },
    [validar],
  );

  const f = res?.boleto && funciones.find((x) => x.id === res.boleto!.funcionId);

  return (
    <div className="grid gap-6 md:grid-cols-2">
      <div className={`${card} space-y-4`}>
        <div className="flex flex-wrap gap-2">
          <Boton icono={camara ? CameraOff : Camera} variante={camara ? "borde" : "vino"} onClick={() => setCamara((x) => !x)} className="flex-1">
            {camara ? "Cerrar cámara" : "Escanear con la cámara"}
          </Boton>
        </div>
        {camara && <EscanerQR onLeer={leerConCamara} pausado={pausa || validando} />}

        <div className="border-t border-borde pt-4">
          <label className="etiqueta block">
            <span className="flex items-center gap-2">
              <Keyboard aria-hidden className="h-3.5 w-3.5" /> Lector QR en modo teclado o código escrito
            </span>
            <input
              autoFocus={!camara}
              value={codigo}
              onChange={(e) => setCodigo(e.target.value)}
              onKeyDown={(e) => {
                if (e.key !== "Enter") return;
                validar(codigo);
                setCodigo("");
              }}
              placeholder="Escanea el QR o escribe: CE-… CÓDIGO"
              className="campo normal-case tracking-normal"
            />
          </label>
          <button
            onClick={() => {
              validar(codigo);
              setCodigo("");
            }}
            disabled={validando}
            className="btn-vino mt-3 w-full py-3.5 disabled:opacity-60"
          >
            <ScanLine aria-hidden className="h-4 w-4" />
            {validando ? "Validando…" : "Validar"}
          </button>
          <p className="mt-2 text-xs text-apagado">
            A mano: escribe el número del boleto y el código de 6 caracteres que aparece bajo su QR (cambia cada minuto). Cada boleto sólo puede
            validarse una vez.
          </p>
        </div>
      </div>
      {res && (
        <m.div
          key={JSON.stringify(res) + String(pausa)}
          initial={{ scale: 0.95, opacity: 0 }}
          animate={{ scale: 1, opacity: 1 }}
          role="status"
          aria-live="assertive"
          className={`grid place-items-center rounded-xl border p-8 text-center ${res.ok ? "border-oro/50 bg-oro/10" : "border-rojo/60 bg-vino/50"}`}
        >
          <div>
            <div className={`mx-auto w-fit ${res.ok ? "text-oro" : "text-salmon"}`}>{res.ok ? <CircleCheck aria-hidden className="h-20 w-20" /> : <CircleX aria-hidden className="h-20 w-20" />}</div>
            <p className="titulo mt-2 text-2xl">{res.ok ? "Acceso permitido" : "Acceso denegado"}</p>
            {!res.ok && <p className="mt-1 text-sm text-salmon">{res.motivo}</p>}
            {f && (
              <p className="mt-2 text-sm text-tenue">
                {pelicula(f.peliculaId).titulo} · {sala(f.salaId).nombre} · {fmtHora(f.inicio)} · Butaca <b className="text-tinta">{res.boleto!.asiento}</b>
              </p>
            )}
            {res.boleto?.titular && <p className="mt-1 text-xs text-apagado">Titular: {res.boleto.titular}</p>}
            {camara && <p className="mt-4 text-[11px] text-apagado">{pausa ? "Leyendo el siguiente boleto en un momento…" : "Listo para el siguiente boleto."}</p>}
          </div>
        </m.div>
      )}
    </div>
  );
}
