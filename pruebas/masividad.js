// Pruebas de masividad de la API de Cineight con k6. ESCENARIO = registro | compra | contencion | cola | sse | lectura
//   registro    N clientes registrándose a la vez (3 registros cada uno); cada cuenta debe recibir 800 créditos.
//               La contraseña se cifra con bcrypt (lento a propósito): mide cuántos registros por segundo aguanta.
//   compra      N clientes con sesión ya iniciada compran 2 butacas (bloqueo → reserva → pago) al mismo tiempo.
//   contencion  N clientes intentan pagar la MISMA butaca en el mismo instante: sólo uno debe lograrlo.
//   cola        N clientes entran a la cola del preestreno a la vez: sólo COLA_CAPACIDAD reciben turno.
//   sse         N mapas de butacas abiertos en vivo (15 s cada uno).
//   lectura     sube a N visitantes navegando cartelera y ocupación.
// Crea datos (clientes, boletos): córrelo contra el entorno de prueba, no contra producción.
//   docker compose -f pruebas/compose-carga.yml up -d --build
//   docker run --rm -i -e ESCENARIO=compra -e N=300 grafana/k6 run - < pruebas/masividad.js
// Cada usuario virtual simula un cliente distinto (IP propia en X-Forwarded-For), así el límite por IP sigue activo.
import http from "k6/http";
import { check, sleep } from "k6";
import { Counter, Trend } from "k6/metrics";

const BASE = __ENV.BASE || "http://host.docker.internal:18080";
const ESC = __ENV.ESCENARIO || "registro";
const N = Number(__ENV.N || 0);
const RUN = __ENV.RUN || String(Date.now() % 1e7);

const pagados = new Counter("compras_pagadas");
const conflictos = new Counter("compras_conflicto_esperado");
const fallasServidor = new Counter("respuestas_5xx");
const limitados = new Counter("respuestas_429");
const duracionCompra = new Trend("duracion_compra_completa", true);

const ESCENARIOS = {
  registro: { executor: "shared-iterations", vus: N || 100, iterations: (N || 100) * 3, maxDuration: "8m" },
  compra: { executor: "per-vu-iterations", vus: N || 300, iterations: 1, maxDuration: "5m" },
  contencion: { executor: "per-vu-iterations", vus: N || 300, iterations: 1, maxDuration: "2m" },
  cola: { executor: "per-vu-iterations", vus: N || 300, iterations: 1, maxDuration: "2m" },
  sse: { executor: "per-vu-iterations", vus: N || 150, iterations: 1, maxDuration: "2m" },
  lectura: { executor: "ramping-vus", stages: [{ duration: "20s", target: N || 500 }, { duration: "40s", target: N || 500 }, { duration: "10s", target: 0 }] },
};

export const options = {
  scenarios: { [ESC]: ESCENARIOS[ESC] },
  setupTimeout: "10m",
  summaryTrendStats: ["avg", "med", "p(95)", "p(99)", "max"],
  thresholds: { respuestas_5xx: ["count<1"] },
};

const ip = (n) => `10.${(n >> 16) & 255}.${(n >> 8) & 255}.${n & 255}`;
// Cada solicitud de un cliente lleva su sesión en la cabecera Authorization (como el navegador).
const cab = (n, token) => ({ headers: { "Content-Type": "application/json", "X-Forwarded-For": ip(n), ...(token ? { Authorization: `Bearer ${token}` } : {}) } });
function anotar(r) {
  if (r.status >= 500 || r.status === 0) fallasServidor.add(1, { estado: String(r.status) });
  if (r.status === 429) limitados.add(1);
  return r;
}
const post = (ruta, cuerpo, n, extra = {}, token) => anotar(http.post(BASE + ruta, cuerpo === undefined ? null : JSON.stringify(cuerpo), { ...cab(n, token), ...extra }));
const get = (ruta, n, extra = {}, token) => anotar(http.get(BASE + ruta, { ...cab(n, token), ...extra }));
const CLAVE = "Palomitas#Dulces77";

const letras = (n) => {
  let t = "";
  do (t = "abcdefghijklmnopqrstuvwxyz"[n % 26] + t), (n = Math.floor(n / 26));
  while (n > 0);
  return t.charAt(0).toUpperCase() + t.slice(1);
};
const datosRegistro = (n, etiqueta) => ({ nombre: "Cliente " + letras(n), correo: `carga-${RUN}-${etiqueta}-${n}@prueba.gt`, clave: CLAVE });

// Registra un cliente y devuelve su token de sesión ("" si falló).
function registrar(n, etiqueta) {
  const r = post("/api/auth/registro", datosRegistro(n, etiqueta), n, { tags: { paso: "registro" } });
  check(r, { "registro 201": (x) => x.status === 201 });
  return r.status === 201 ? r.json("token") : "";
}

// Registra `cuantos` clientes en lotes paralelos (antes de la prueba) y devuelve sus tokens.
function registrarVarios(cuantos, etiqueta) {
  const tokens = [];
  for (let i = 0; i < cuantos; i += 8) {
    const lote = [];
    for (let j = i; j < Math.min(i + 8, cuantos); j++)
      lote.push(["POST", `${BASE}/api/auth/registro`, JSON.stringify(datosRegistro(100000 + j, etiqueta)), cab(100000 + j)]);
    for (const r of http.batch(lote)) tokens.push(r.status === 201 ? r.json("token") : "");
  }
  console.log(`${tokens.filter(Boolean).length}/${cuantos} clientes registrados para la prueba`);
  return tokens;
}

export function setup() {
  const funciones = http.get(`${BASE}/api/funciones`).json();
  // Funciones normales que empiezan en más de 30 min (las que ya empezaron no venden).
  const limite = Date.now() + 30 * 60 * 1000;
  const normales = funciones.filter((f) => !f.evento && new Date(f.inicio).getTime() > limite);
  const datos = { normales: normales.map((f) => f.codigo), evento: (funciones.find((f) => f.evento) || {}).evento, objetivo: normales[0] && normales[0].codigo };
  if (ESC === "compra" || ESC === "contencion" || ESC === "cola") {
    // Los clientes se registran antes: la prueba mide sólo el choque simultáneo de compras o de la cola.
    datos.tokens = registrarVarios(N || 300, ESC);
  }
  if (ESC === "contencion" || ESC === "cola") {
    // Una butaca que está libre justo antes del choque.
    for (const codigo of datos.normales.slice(20)) {
      const o = http.get(`${BASE}/api/funciones/${codigo}/asientos`).json();
      if (![...o.vendidos, ...o.retenidos, ...(o.fuera_servicio || [])].includes("E-06")) {
        datos.objetivo = codigo;
        break;
      }
    }
    console.log(`objetivo: ${datos.objetivo} E-06`);
  }
  if (ESC === "sse") console.log(`objetivo: ${datos.normales[0]}`);
  return datos;
}

export default function (d) {
  const n = __VU * 1000 + __ITER;
  if (ESC === "registro") {
    const token = registrar(n, "r");
    if (token) check(get("/api/cuenta", n, {}, token), { "bono de 800": (x) => x.status === 200 && x.json("saldo") === 800 });
    return;
  }

  if (ESC === "compra") {
    const inicio = Date.now();
    const token = d.tokens[__VU - 1];
    if (!token) return;
    // Cada cliente intenta 2 butacas de una función al azar; si otra persona se le adelanta, prueba con otras.
    for (let intento = 0; intento < 3; intento++) {
      const f = d.normales[Math.floor(Math.random() * Math.min(d.normales.length, 40))];
      const occ = get(`/api/funciones/${f}/asientos`, n);
      if (occ.status !== 200) continue;
      const tomadas = new Set([...occ.json("vendidos"), ...occ.json("retenidos"), ...(occ.json("fuera_servicio") || [])]);
      const libres = [];
      // A–F × 1–10 existe en todas las salas (la más chica es de 6 × 10).
      for (const fila of "ABCDEF") for (let c = 1; c <= 10; c++) if (!tomadas.has(`${fila}-${String(c).padStart(2, "0")}`)) libres.push(`${fila}-${String(c).padStart(2, "0")}`);
      if (libres.length < 2) continue;
      const k = Math.floor(Math.random() * (libres.length - 1));
      const asientos = [libres[k], libres[k + 1]];
      let ok = true;
      for (const a of asientos) {
        const b = post(`/api/funciones/${f}/asientos/${a}/bloqueo`, undefined, n, { tags: { paso: "bloqueo" } }, token);
        if (b.status !== 201) ok = false;
      }
      if (!ok) {
        anotar(http.del(`${BASE}/api/funciones/${f}/bloqueos`, JSON.stringify({ asientos }), cab(n, token)));
        conflictos.add(1);
        continue;
      }
      const r = post("/api/reservas", { funcion: f, asientos }, n, { tags: { paso: "reserva" } }, token);
      if (r.status !== 201) {
        conflictos.add(1);
        continue;
      }
      const p = post(`/api/reservas/${r.json("reserva_id")}/pagar`, undefined, n, { tags: { paso: "pago" } }, token);
      check(p, { "pago 201": (x) => x.status === 201, "2 boletos": (x) => x.status === 201 && x.json("boletos").length === 2, "saldo 800-2×precio": (x) => x.status === 201 && x.json("saldo") < 800 });
      if (p.status === 201) {
        pagados.add(1);
        duracionCompra.add(Date.now() - inicio);
        return;
      }
      conflictos.add(1);
    }
    return;
  }

  if (ESC === "contencion") {
    // Todos a la vez por la MISMA butaca: sólo uno debe pagarla.
    const token = d.tokens[__VU - 1];
    const r = post("/api/reservas", { funcion: d.objetivo, asientos: ["E-06"] }, 100000 + __VU, { tags: { paso: "reserva" } }, token);
    if (r.status === 201) {
      const p = post(`/api/reservas/${r.json("reserva_id")}/pagar`, undefined, 100000 + __VU, { tags: { paso: "pago" } }, token);
      if (p.status === 201) pagados.add(1);
    } else if (r.status === 409) conflictos.add(1);
    return;
  }

  if (ESC === "cola") {
    const token = d.tokens[__VU - 1];
    const r = post(`/api/cola/${d.evento}`, {}, 100000 + __VU, { tags: { paso: "cola" } }, token);
    check(r, { "entra a la cola": (x) => x.status === 200 || x.status === 201 });
    sleep(2);
    const pos = get(`/api/cola/${d.evento}`, 100000 + __VU, {}, token);
    if (pos.status === 200 && pos.json("turno_habilitado")) pagados.add(1, { tipo: "turno" });
    return;
  }

  if (ESC === "sse") {
    // Cada cliente deja abierto el mapa en vivo 15 s (como alguien eligiendo butacas).
    const r = get(`/api/funciones/${d.normales[0]}/stream`, n, { timeout: "15s", tags: { paso: "sse" } });
    check(r, {
      "stream entrega la ocupación": (x) => (x.body || "").includes("event: estado"),
      "recibe en vivo el bloqueo de otro cliente": (x) => (x.body || "").includes('"tipo":"bloqueado"'),
    });
    return;
  }

  // lectura: lo que hace un visitante al navegar la cartelera.
  check(get("/api/funciones", n, { tags: { paso: "cartelera" } }), { "cartelera 200": (x) => x.status === 200 });
  const f = d.normales[Math.floor(Math.random() * d.normales.length)];
  check(get(`/api/funciones/${f}/asientos`, n, { tags: { paso: "asientos" } }), { "ocupación 200": (x) => x.status === 200 });
  sleep(1);
}
