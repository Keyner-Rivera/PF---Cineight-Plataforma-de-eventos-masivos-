import type { NextConfig } from "next";

// Cabeceras de seguridad (OWASP Secure Headers). La CSP sólo permite scripts, marcos y conexiones de los orígenes que
// la aplicación usa de verdad: el propio sitio, la API, reCAPTCHA (google.com / gstatic.com) y los tráileres
// (youtube-nocookie.com). Así, si se lograra inyectar HTML, no podría cargar scripts de otro sitio ni enviar datos
// (p. ej. el token de sesión) a un servidor ajeno.
// - 'unsafe-inline' en scripts: Next.js inserta pequeños scripts en línea al hidratar páginas estáticas; un nonce
//   obligaría a renderizar cada página en el servidor. React escapa todo el contenido dinámico, y no hay innerHTML.
// - 'wasm-unsafe-eval': el lector de QR zxing es WebAssembly (sólo permite compilar wasm, no eval de JavaScript).
const desarrollo = process.env.NODE_ENV !== "production";
const api = (() => {
  try {
    return new URL(process.env.NEXT_PUBLIC_API_URL ?? "").origin;
  } catch {
    return "";
  }
})();
const csp = [
  "default-src 'self'",
  `script-src 'self' 'unsafe-inline' 'wasm-unsafe-eval'${desarrollo ? " 'unsafe-eval'" : ""} https://www.google.com https://www.gstatic.com`,
  "style-src 'self' 'unsafe-inline'",
  `img-src 'self' data: blob: ${api}`,
  "font-src 'self' data:",
  `connect-src 'self' ${api} https://www.google.com${desarrollo ? " ws:" : ""}`,
  "frame-src https://www.google.com https://recaptcha.google.com https://www.youtube-nocookie.com",
  "media-src 'self' blob:",
  "worker-src 'self' blob:",
  "object-src 'none'",
  "base-uri 'self'",
  "form-action 'self'",
  "frame-ancestors 'none'",
  ...(api.startsWith("https://") ? ["upgrade-insecure-requests"] : []),
].join("; ");

const cabecerasSeguridad = [
  { key: "Content-Security-Policy", value: csp },
  { key: "X-Content-Type-Options", value: "nosniff" },
  { key: "X-Frame-Options", value: "DENY" },
  { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
  // La cámara sólo la usa el propio sitio (validación de boletos); nada más puede pedir micrófono, ubicación, etc.
  { key: "Permissions-Policy", value: "camera=(self), microphone=(), geolocation=(), payment=(), usb=(), interest-cohort=()" },
  { key: "Cross-Origin-Opener-Policy", value: "same-origin" },
];

const nextConfig: NextConfig = {
  poweredByHeader: false,
  async headers() {
    return [{ source: "/:ruta*", headers: cabecerasSeguridad }];
  },
  cacheComponents: true,
  // El prototipo renderiza en el cliente (estado en localStorage), así que solo se validan
  // los segmentos que declaren `instant` explícitamente.
  experimental: {
    instantInsights: { validationLevel: "manual-warning" },
  },
  partialPrefetching: true,
  // next/image: los pósters (JPG/PNG de hasta 180 KB) se sirven en AVIF o WebP y al tamaño en que se muestran.
  images: {
    formats: ["image/avif", "image/webp"],
    qualities: [60, 75], // obligatorio desde Next 16: sólo estas calidades se pueden pedir
    localPatterns: [
      { pathname: "/posters/**", search: "" },
      { pathname: "/logo-cineight.png", search: "" },
    ],
    minimumCacheTTL: 2678400, // 31 días: los pósters no cambian
  },
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
};

export default nextConfig;
