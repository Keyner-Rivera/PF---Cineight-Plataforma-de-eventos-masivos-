"use client";

import { useEffect, useRef, useState } from "react";
import { Camera, Flashlight, FlashlightOff, Loader2, SwitchCamera, ZoomIn } from "lucide-react";

// Escáner de QR con la cámara del dispositivo (taquilla o puerta de sala).
// - Lector: el BarcodeDetector nativo del navegador cuando existe (Chrome en Android y macOS); si no (Safari en iPhone,
//   Chrome en Windows), zxing-cpp compilado a WebAssembly, que se descarga sólo al abrir el escáner y se sirve desde
//   este mismo sitio (public/zxing), no desde un CDN.
// - Enfoque: cámara trasera en alta resolución con enfoque continuo; tocar la imagen enfoca ese punto (si el
//   dispositivo lo permite). El zoom 2× deja leer desde más lejos, a una distancia que el lente enfoca sin problema.
// - Se analiza el cuadro central (donde está la guía) en una imagen reducida, que es más rápido y nítido que el
//   cuadro completo; cada pocos intentos se revisa la imagen entera por si el QR no está centrado.

type Detector = { detect: (img: CanvasImageSource) => Promise<{ rawValue: string }[]> };

// Versión fija: el archivo .wasm de public/zxing corresponde a zxing-wasm 3.1.3 (dependencia de barcode-detector 3.2.2).
const WASM_ZXING = "/zxing/zxing_reader-3.1.3.wasm";
const INTERVALO_MS = 90;
const REPETIDO_MS = 3000; // el mismo QR frente a la cámara no se envía dos veces seguidas

async function crearDetector(): Promise<Detector> {
  const Nativo = (globalThis as { BarcodeDetector?: { new (o: { formats: string[] }): Detector; getSupportedFormats(): Promise<string[]> } }).BarcodeDetector;
  if (Nativo) {
    try {
      if ((await Nativo.getSupportedFormats()).includes("qr_code")) return new Nativo({ formats: ["qr_code"] });
    } catch {}
  }
  const { BarcodeDetector, prepareZXingModule } = await import("barcode-detector/ponyfill");
  prepareZXingModule({ overrides: { locateFile: (ruta: string, prefijo: string) => (ruta.endsWith(".wasm") ? WASM_ZXING : prefijo + ruta) } });
  return new BarcodeDetector({ formats: ["qr_code"] });
}

// Capacidades de cámara que aún no trae la librería de tipos de TypeScript (enfoque, linterna, zoom).
type Capacidades = MediaTrackCapabilities & { focusMode?: string[]; torch?: boolean; zoom?: { min: number; max: number }; pointsOfInterest?: unknown };
const aplicar = (pista: MediaStreamTrack, ajustes: Record<string, unknown>) => pista.applyConstraints({ advanced: [ajustes as MediaTrackConstraintSet] }).catch(() => {});

type Estado = "iniciando" | "buscando" | "error";

/**
 * Muestra la cámara y llama a onLeer con el texto de cada QR detectado. Mientras `pausado` es true no analiza
 * (p. ej. mientras se valida o se muestra el resultado).
 */
export function EscanerQR({ onLeer, pausado }: { onLeer: (texto: string) => void; pausado: boolean }) {
  const video = useRef<HTMLVideoElement>(null);
  const pista = useRef<MediaStreamTrack | null>(null);
  const [estado, setEstado] = useState<Estado>("iniciando");
  const [error, setError] = useState("");
  const [camaras, setCamaras] = useState<MediaDeviceInfo[]>([]);
  const [camara, setCamara] = useState<string | null>(null); // deviceId elegido (null = la trasera por defecto)
  const [caps, setCaps] = useState<Capacidades>({});
  const [linterna, setLinterna] = useState(false);
  const [zoom2x, setZoom2x] = useState(false);
  const [enfoque, setEnfoque] = useState<{ x: number; y: number } | null>(null);

  const onLeerRef = useRef(onLeer);
  const pausadoRef = useRef(pausado);
  useEffect(() => {
    onLeerRef.current = onLeer;
    pausadoRef.current = pausado;
  });

  // Abre la cámara elegida con la mayor resolución práctica y enfoque continuo.
  useEffect(() => {
    let vivo = true;
    let flujo: MediaStream | null = null;
    (async () => {
      if (!navigator.mediaDevices?.getUserMedia) {
        setEstado("error");
        return setError(window.isSecureContext ? "Este navegador no permite usar la cámara." : "La cámara sólo funciona en páginas seguras (https).");
      }
      try {
        flujo = await navigator.mediaDevices.getUserMedia({
          audio: false,
          video: {
            ...(camara ? { deviceId: { exact: camara } } : { facingMode: { ideal: "environment" } }),
            width: { ideal: 1920 },
            height: { ideal: 1080 },
            frameRate: { ideal: 30 },
          },
        });
      } catch (e) {
        if (!vivo) return;
        const nombre = (e as DOMException).name;
        setEstado("error");
        return setError(
          nombre === "NotAllowedError"
            ? "Permiso de cámara denegado. Actívalo en el candado de la barra de direcciones y vuelve a intentarlo."
            : nombre === "NotFoundError" || nombre === "OverconstrainedError"
              ? "No se encontró una cámara disponible."
              : nombre === "NotReadableError"
                ? "La cámara está en uso por otra aplicación."
                : "No se pudo abrir la cámara.",
        );
      }
      if (!vivo) return flujo.getTracks().forEach((t) => t.stop());
      const p = flujo.getVideoTracks()[0];
      pista.current = p;
      const c = (p.getCapabilities?.() ?? {}) as Capacidades;
      setCaps(c);
      setLinterna(false);
      setZoom2x(false);
      if (c.focusMode?.includes("continuous")) await aplicar(p, { focusMode: "continuous" });
      const v = video.current!;
      v.srcObject = flujo;
      await v.play().catch(() => {});
      setEstado("buscando");
      // La lista de cámaras (con nombres) sólo está disponible después de conceder el permiso.
      const dispositivos = await navigator.mediaDevices.enumerateDevices().catch(() => []);
      if (vivo) setCamaras(dispositivos.filter((d) => d.kind === "videoinput"));
    })();
    return () => {
      vivo = false;
      flujo?.getTracks().forEach((t) => t.stop());
      pista.current = null;
    };
  }, [camara]);

  // Ciclo de lectura.
  useEffect(() => {
    if (estado !== "buscando") return;
    let vivo = true;
    let t: ReturnType<typeof setTimeout>;
    let intento = 0;
    let ultimo = { texto: "", en: 0 };
    const lienzo = document.createElement("canvas");
    const ctx = lienzo.getContext("2d", { willReadFrequently: true })!;
    (async () => {
      let detector: Detector;
      try {
        detector = await crearDetector();
      } catch {
        if (!vivo) return;
        setEstado("error");
        return setError("No se pudo cargar el lector de códigos QR. Revisa la conexión y vuelve a abrir el escáner.");
      }
      const ciclo = async () => {
        if (!vivo) return;
        const v = video.current;
        if (v && !pausadoRef.current && v.readyState >= 2 && v.videoWidth > 0) {
          intento++;
          const completo = intento % 5 === 0;
          const lado = Math.min(v.videoWidth, v.videoHeight) * (completo ? 1 : 0.75);
          const sx = completo ? 0 : (v.videoWidth - lado) / 2;
          const sy = completo ? 0 : (v.videoHeight - lado) / 2;
          const ancho = completo ? v.videoWidth : lado;
          const alto = completo ? v.videoHeight : lado;
          const escala = Math.min(1, 900 / Math.max(ancho, alto));
          lienzo.width = Math.round(ancho * escala);
          lienzo.height = Math.round(alto * escala);
          ctx.drawImage(v, sx, sy, ancho, alto, 0, 0, lienzo.width, lienzo.height);
          try {
            const [r] = await detector.detect(lienzo);
            const ahora = Date.now();
            if (r?.rawValue && vivo && !pausadoRef.current && (r.rawValue !== ultimo.texto || ahora - ultimo.en > REPETIDO_MS)) {
              ultimo = { texto: r.rawValue, en: ahora };
              navigator.vibrate?.(60);
              onLeerRef.current(r.rawValue);
            }
          } catch {}
        }
        t = setTimeout(ciclo, INTERVALO_MS);
      };
      ciclo();
    })();
    return () => {
      vivo = false;
      clearTimeout(t);
    };
  }, [estado]);

  const alternarLinterna = async () => {
    if (!pista.current) return;
    await aplicar(pista.current, { torch: !linterna });
    setLinterna((x) => !x);
  };
  const alternarZoom = async () => {
    if (!pista.current || !caps.zoom) return;
    await aplicar(pista.current, { zoom: zoom2x ? Math.max(caps.zoom.min, 1) : Math.min(caps.zoom.max, 2) });
    setZoom2x((x) => !x);
  };
  const siguienteCamara = () => {
    if (camaras.length < 2) return;
    const actual = pista.current?.getSettings().deviceId;
    const i = camaras.findIndex((d) => d.deviceId === actual);
    setEstado("iniciando");
    setCamara(camaras[(i + 1) % camaras.length].deviceId);
  };
  // Tocar la imagen enfoca ese punto (cámaras que admiten puntos de interés); luego vuelve al enfoque continuo.
  const enfocar = async (e: React.PointerEvent<HTMLDivElement>) => {
    const p = pista.current;
    if (!p) return;
    const caja = e.currentTarget.getBoundingClientRect();
    const x = (e.clientX - caja.left) / caja.width;
    const y = (e.clientY - caja.top) / caja.height;
    setEnfoque({ x, y });
    setTimeout(() => setEnfoque(null), 900);
    if (caps.pointsOfInterest !== undefined) await aplicar(p, { pointsOfInterest: [{ x, y }] });
    if (caps.focusMode?.includes("single-shot")) {
      await aplicar(p, { focusMode: "single-shot" });
      setTimeout(() => caps.focusMode?.includes("continuous") && aplicar(p, { focusMode: "continuous" }), 1500);
    }
  };

  return (
    <div className="space-y-3">
      <div onPointerDown={enfocar} className="relative aspect-square w-full overflow-hidden rounded-xl border border-borde bg-black sm:aspect-[4/3]">
        <video ref={video} playsInline muted autoPlay className="h-full w-full object-cover" />
        {/* Guía: el QR se lee mejor dentro del cuadro (es la zona que se analiza con más detalle). */}
        {estado === "buscando" && (
          <div className="pointer-events-none absolute inset-0 grid place-items-center">
            <div className={`relative aspect-square w-[62%] max-w-[300px] transition-opacity ${pausado ? "opacity-40" : ""}`}>
              {["left-0 top-0 border-l-4 border-t-4 rounded-tl-xl", "right-0 top-0 border-r-4 border-t-4 rounded-tr-xl", "bottom-0 left-0 border-b-4 border-l-4 rounded-bl-xl", "bottom-0 right-0 border-b-4 border-r-4 rounded-br-xl"].map((c) => (
                <span key={c} className={`absolute h-9 w-9 border-oro ${c}`} />
              ))}
              {!pausado && <span className="escaner-linea absolute inset-x-3 h-0.5 rounded-full bg-oro/80 shadow-[0_0_12px_2px] shadow-oro/50" />}
            </div>
          </div>
        )}
        {enfoque && (
          <span className="pointer-events-none absolute h-14 w-14 -translate-x-1/2 -translate-y-1/2 animate-ping rounded-full border-2 border-white/80" style={{ left: `${enfoque.x * 100}%`, top: `${enfoque.y * 100}%` }} />
        )}
        {estado !== "buscando" && (
          <div className="absolute inset-0 grid place-items-center p-6 text-center">
            {estado === "iniciando" ? (
              <p className="flex items-center gap-2 text-sm text-tenue">
                <Loader2 aria-hidden className="h-4 w-4 animate-spin" /> Abriendo la cámara…
              </p>
            ) : (
              <div>
                <Camera aria-hidden className="mx-auto h-8 w-8 text-salmon" />
                <p className="mt-2 text-sm text-salmon">{error}</p>
              </div>
            )}
          </div>
        )}
      </div>
      {estado === "buscando" && (
        <div className="flex flex-wrap items-center justify-between gap-2">
          <p className="text-xs text-tenue">Centra el QR en el cuadro a unos 15–25 cm. Toca la imagen para enfocar.</p>
          <div className="flex gap-2">
            {caps.torch && (
              <BotonCamara etiqueta={linterna ? "Apagar linterna" : "Encender linterna"} activo={linterna} onClick={alternarLinterna}>
                {linterna ? <FlashlightOff aria-hidden className="h-4 w-4" /> : <Flashlight aria-hidden className="h-4 w-4" />}
              </BotonCamara>
            )}
            {caps.zoom && caps.zoom.max >= 2 && (
              <BotonCamara etiqueta={zoom2x ? "Zoom normal" : "Zoom 2×"} activo={zoom2x} onClick={alternarZoom}>
                <ZoomIn aria-hidden className="h-4 w-4" />
              </BotonCamara>
            )}
            {camaras.length > 1 && (
              <BotonCamara etiqueta="Cambiar de cámara" onClick={siguienteCamara}>
                <SwitchCamera aria-hidden className="h-4 w-4" />
              </BotonCamara>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function BotonCamara({ etiqueta, activo, onClick, children }: { etiqueta: string; activo?: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={etiqueta}
      aria-label={etiqueta}
      aria-pressed={activo}
      className={`grid h-9 w-9 place-items-center rounded-lg border transition-colors ${activo ? "border-oro bg-oro/15 text-oro" : "border-borde bg-panel-2 text-tenue hover:text-tinta"}`}
    >
      {children}
    </button>
  );
}
