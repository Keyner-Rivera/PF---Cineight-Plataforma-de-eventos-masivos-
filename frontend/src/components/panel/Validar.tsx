"use client";

import { Camera, CameraOff, CircleCheck, CircleX, Keyboard, ScanLine } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import dynamic from "next/dynamic";
import { AnimatePresence, m } from "framer-motion";
import { pelicula, sala } from "@/data/catalogo";
import { fmtHora } from "@/utils/formato";
import { useApp } from "@/store/AppProvider";
import type { Validacion } from "@/types/cuenta";
import type { Funcion } from "@/types/catalogo";
import { Boton } from "@/components/ui/Boton";
import { card } from "@/components/panel/Elementos";

// El escáner (y su lector de QR) sólo se descarga al abrir la cámara.
const EscanerQR = dynamic(() => import("@/components/panel/EscanerQR").then((x) => x.EscanerQR), { ssr: false });

/** Tiempo que la ventana del resultado queda en pantalla; la cámara vuelve a leer cuando se cierra. */
const VENTANA_RESULTADO_MS = 3500;
/** La cámara ignora el mismo boleto este tiempo después de leerlo: al cerrarse la ventana el cliente suele seguir
 * mostrando el QR, y volver a validarlo mostraría "ya utilizado" a quien acaba de entrar. */
const MISMO_BOLETO_MS = 20000;

/** Token del boleto dentro del contenido del QR (JSON {t, …}); el texto completo cambia cada minuto. */
const tokenDelQR = (texto: string) => {
  try {
    return String(JSON.parse(texto).t ?? texto);
  } catch {
    return texto;
  }
};

type Lectura = Validacion & { hora: number };

/* ─────────── Validar acceso (RF-09, RF-27, RF-28) ─────────── */
// Tres formas de leer un boleto: la cámara del dispositivo, un lector QR en modo teclado o el código escrito a mano.
// El resultado aparece en una ventana emergente grande (pensada para el teléfono en la puerta de la sala) que se
// cierra sola; debajo queda, discreto, el último resultado.
export function Validar() {
  const { validarBoleto, funciones } = useApp();
  const [camara, setCamara] = useState(false);
  const [codigo, setCodigo] = useState("");
  const [validando, setValidando] = useState(false);
  const [ventana, setVentana] = useState<Lectura | null>(null); // resultado en pantalla
  const [ultimo, setUltimo] = useState<Lectura | null>(null);
  const cierre = useRef<ReturnType<typeof setTimeout>>(undefined);
  const leidoConCamara = useRef({ token: "", en: 0 });
  useEffect(() => () => clearTimeout(cierre.current), []);

  const cerrarVentana = useCallback(() => {
    clearTimeout(cierre.current);
    setVentana(null);
  }, []);

  // La validación es atómica en PostgreSQL (POST /api/boletos/{token}/validar) y exige el código dinámico vigente.
  const validar = useCallback(
    async (entrada: string) => {
      if (!entrada.trim()) return;
      setValidando(true);
      try {
        const r: Lectura = { ...(await validarBoleto(entrada.trim())), hora: Date.now() };
        setVentana(r);
        setUltimo(r);
        navigator.vibrate?.(r.ok ? 120 : [80, 60, 80, 60, 80]);
        clearTimeout(cierre.current);
        cierre.current = setTimeout(() => setVentana(null), VENTANA_RESULTADO_MS);
      } finally {
        setValidando(false);
      }
    },
    [validarBoleto],
  );

  const leerConCamara = useCallback(
    (texto: string) => {
      const token = tokenDelQR(texto);
      const previo = leidoConCamara.current;
      if (token === previo.token && Date.now() - previo.en < MISMO_BOLETO_MS) return;
      leidoConCamara.current = { token, en: Date.now() };
      validar(texto);
    },
    [validar],
  );

  const enviarEscrito = () => {
    validar(codigo);
    setCodigo("");
  };

  const funcionDe = (l: Lectura | null) => (l?.boleto ? funciones.find((x) => x.id === l.boleto!.funcionId) : undefined);
  const fu = funcionDe(ultimo);

  return (
    <div className="mx-auto max-w-xl">
      <div className={`${card} space-y-4`}>
        <Boton icono={camara ? CameraOff : Camera} variante={camara ? "borde" : "vino"} onClick={() => setCamara((x) => !x)} className="w-full">
          {camara ? "Cerrar cámara" : "Escanear con la cámara"}
        </Boton>
        {camara && <EscanerQR onLeer={leerConCamara} pausado={!!ventana || validando} />}

        {/* Último resultado, discreto: la ventana emergente ya lo mostró en grande. */}
        {ultimo && (
          <p role="status" className={`flex items-center gap-2 rounded-md border px-3 py-2 text-xs ${ultimo.ok ? "border-oro/30 text-oro" : "border-rojo/40 text-salmon"}`}>
            {ultimo.ok ? <CircleCheck aria-hidden className="h-3.5 w-3.5 shrink-0" /> : <CircleX aria-hidden className="h-3.5 w-3.5 shrink-0" />}
            <span className="min-w-0" title={fu ? pelicula(fu.peliculaId).titulo : undefined}>
              <b>{ultimo.ok ? "Permitido" : "Denegado"}</b>
              {ultimo.boleto ? ` · ${ultimo.boleto.asiento}` : ""}
              {!ultimo.ok ? <span className="text-salmon/80"> · {ultimo.motivo}</span> : ""}
            </span>
            <span className="ml-auto shrink-0 tabular-nums text-apagado">{new Date(ultimo.hora).toLocaleTimeString("es-GT")}</span>
          </p>
        )}

        <div className="border-t border-borde pt-4">
          <label className="etiqueta block">
            <span className="flex items-center gap-2">
              <Keyboard aria-hidden className="h-3.5 w-3.5" /> Lector QR en modo teclado o código escrito
            </span>
            <input
              autoFocus={!camara}
              value={codigo}
              onChange={(e) => setCodigo(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && enviarEscrito()}
              placeholder="Escanea el QR o escribe: CE-… CÓDIGO"
              className="campo normal-case tracking-normal"
            />
          </label>
          <button onClick={enviarEscrito} disabled={validando} className="btn-vino mt-3 w-full py-3.5 disabled:opacity-60">
            <ScanLine aria-hidden className="h-4 w-4" />
            {validando ? "Validando…" : "Validar"}
          </button>
          <p className="mt-2 text-xs text-apagado">
            A mano: escribe el número del boleto y el código de 6 caracteres que aparece bajo su QR (cambia cada minuto). Cada boleto sólo puede
            validarse una vez.
          </p>
        </div>
      </div>

      <VentanaResultado lectura={ventana} funcion={funcionDe(ventana)} onCerrar={cerrarVentana} />
    </div>
  );
}

/** Resultado a pantalla completa: verde si entra, rojo con el motivo si no. Se cierra solo o al tocarlo. */
function VentanaResultado({ lectura, funcion: f, onCerrar }: { lectura: Lectura | null; funcion?: Funcion; onCerrar: () => void }) {
  return (
    <AnimatePresence>
      {lectura && (
        <m.div
          key={lectura.hora}
          role="alertdialog"
          aria-live="assertive"
          aria-label={lectura.ok ? "Acceso permitido" : "Acceso denegado"}
          onClick={onCerrar}
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{ duration: 0.15 }}
          className="fixed inset-0 z-[70] grid place-items-center bg-black/70 p-5 backdrop-blur-sm"
        >
          <m.div
            initial={{ scale: 0.85, y: 12 }}
            animate={{ scale: 1, y: 0 }}
            exit={{ scale: 0.95 }}
            transition={{ type: "spring", stiffness: 420, damping: 28 }}
            className={`relative w-full max-w-sm overflow-hidden rounded-2xl border-2 p-8 text-center shadow-2xl bg-panel bg-gradient-to-b to-transparent ${lectura.ok ? "border-oro from-oro/20" : "border-rojo from-vino"}`}
          >
            <div className={`mx-auto w-fit ${lectura.ok ? "text-oro" : "text-salmon"}`}>
              {lectura.ok ? <CircleCheck aria-hidden className="h-24 w-24" /> : <CircleX aria-hidden className="h-24 w-24" />}
            </div>
            <p className="titulo mt-3 text-3xl">{lectura.ok ? "Acceso permitido" : "Acceso denegado"}</p>
            {!lectura.ok && <p className="mt-2 text-sm text-salmon">{lectura.motivo}</p>}
            {lectura.boleto && (
              <p className="mt-4 text-4xl font-black tracking-wide text-tinta">
                <span className="mr-2 align-middle text-xs font-bold uppercase tracking-[0.2em] text-tenue">Butaca</span>
                {lectura.boleto.asiento}
              </p>
            )}
            {f && (
              <p className="mt-2 text-sm text-tenue">
                {pelicula(f.peliculaId).titulo} · {sala(f.salaId).nombre} · {fmtHora(f.inicio)}
              </p>
            )}
            {lectura.boleto?.titular && <p className="mt-1 text-xs text-apagado">Titular: {lectura.boleto.titular}</p>}
            {/* Barra que se vacía: cuánto falta para que la ventana se cierre sola */}
            <m.span
              aria-hidden
              initial={{ scaleX: 1 }}
              animate={{ scaleX: 0 }}
              transition={{ duration: VENTANA_RESULTADO_MS / 1000, ease: "linear" }}
              className={`absolute inset-x-0 bottom-0 h-1 origin-left ${lectura.ok ? "bg-oro" : "bg-rojo"}`}
            />
          </m.div>
        </m.div>
      )}
    </AnimatePresence>
  );
}
