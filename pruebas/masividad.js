// Pruebas de masividad de la API de Cineight con k6. ESCENARIO = registro | compra | contencion | cola | sse | lectura
//   registro    N clientes registrándose a la vez (10 registros cada uno); cada cuenta debe recibir 800 créditos.
//   compra      N clientes que se registran y compran 2 butacas (bloqueo → reserva → pago) al mismo tiempo.
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
  registro: { executor: "shared-iterations", vus: N || 200, iterations: (N || 200) * 10, maxDuration: "5m" },
  compra: { executor: "per-vu-iterations", vus: N || 300, iterations: 1, maxDuration: "5m" },
  contencion: { executor: "per-vu-iterations", vus: N || 300, iterations: 1, maxDuration: "2m" },
  cola: { executor: "per-vu-iterations", vus: N || 300, iterations: 1, maxDuration: "2m" },
  sse: { executor: "per-vu-iterations", vus: N || 150, iterations: 1, maxDuration: "2m" },
  lectura: { executor: "ramping-vus", stages: [{ duration: "20s", target: N || 500 }, { duration: "40s", target: N || 500 }, { duration: "10s", target: 0 }] },
};

export const options = {
  scenarios: { [ESC]: ESCENARIOS[ESC] },
  setupTimeout: "5m",
  summaryTrendStats: ["avg", "med", "p(95)", "p(99)", "max"],
  thresholds: { respuestas_5xx: ["count<1"] },
};

const ip = (n) => `10.${(n >> 16) & 255}.${(n >> 8) & 255}.${n & 255}`;
const cab = (n) => ({ headers: { "Content-Type": "application/json", "X-Forwarded-For": ip(n) } });
function anotar(r) {
  if (r.status >= 500 || r.status === 0) fallasServidor.add(1, { estado: String(r.status) });
  if (r.status === 429) limitados.add(1);
  return r;
}
const post = (ruta, cuerpo, n, extra = {}) => anotar(http.post(BASE + ruta, cuerpo === undefined ? null : JSON.stringify(cuerpo), { ...cab(n), ...extra }));
const get = (ruta, n, extra = {}) => anotar(http.get(BASE + ruta, { ...cab(n), ...extra }));

const letras = (n) => {
  let t = "";
  do (t = "abcdefghijklmnopqrstuvwxyz"[n % 26] + t), (n = Math.floor(n / 26));
  while (n > 0);
  return t.charAt(0).toUpperCase() + t.slice(1);
};
function registrar(n, etiqueta) {
  const r = post("/api/sesion", { correo: `carga-${RUN}-${etiqueta}-${n}@prueba.gt`, nombre: "Cliente " + letras(n) }, n, { tags: { paso: "registro" } });
  check(r, { "registro 200": (x) => x.status === 200, "bono de 800": (x) => x.status === 200 && x.json("saldo") === 800 });
  return r.status === 200 ? r.json("usuario_id") : 0;
}

export function setup() {
  const funciones = http.get(`${BASE}/api/funciones`).json();
  // Funciones normales que empiezan en más de 30 min (las que ya empezaron no venden).
  const limite = Date.now() + 30 * 60 * 1000;
  const normales = funciones.filter((f) => !f.evento && new Date(f.inicio).getTime() > limite);
  const datos = { normales: normales.map((f) => f.codigo), evento: (funciones.find((f) => f.evento) || {}).evento, objetivo: normales[0] && normales[0].codigo };
  if (ESC === "contencion" || ESC === "cola") {
    // Los usuarios se crean antes: la prueba mide sólo el choque simultáneo.
    datos.usuarios = [];
    for (let i = 1; i <= (N || 300); i++) datos.usuarios.push(registrar(100000 + i, ESC));
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
    const id = registrar(n, "r");
    if (id) check(get(`/api/usuarios/${id}/cuenta`, n), { "cuenta 200": (x) => x.status === 200 });
    return;
  }

  if (ESC === "compra") {
    const inicio = Date.now();
    const id = registrar(n, "c");
    if (!id) return;
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
        const b = post(`/api/funciones/${f}/asientos/${a}/bloqueo`, { usuario_id: id }, n, { tags: { paso: "bloqueo" } });
        if (b.status !== 201) ok = false;
      }
      if (!ok) {
        post(`/api/funciones/${f}/bloqueos`, undefined, n);
        conflictos.add(1);
        continue;
      }
      const r = post("/api/reservas", { funcion: f, usuario_id: id, asientos }, n, { tags: { paso: "reserva" } });
      if (r.status !== 201) {
        conflictos.add(1);
        continue;
      }
      const p = post(`/api/reservas/${r.json("reserva_id")}/pagar`, undefined, n, { tags: { paso: "pago" } });
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
    const id = d.usuarios[__VU - 1];
    const r = post("/api/reservas", { funcion: d.objetivo, usuario_id: id, asientos: ["E-06"] }, 100000 + __VU, { tags: { paso: "reserva" } });
    if (r.status === 201) {
      const p = post(`/api/reservas/${r.json("reserva_id")}/pagar`, undefined, 100000 + __VU, { tags: { paso: "pago" } });
      if (p.status === 201) pagados.add(1);
    } else if (r.status === 409) conflictos.add(1);
    return;
  }

  if (ESC === "cola") {
    const id = d.usuarios[__VU - 1];
    const r = post(`/api/cola/${d.evento}`, { usuario_id: id }, 100000 + __VU, { tags: { paso: "cola" } });
    check(r, { "entra a la cola": (x) => x.status === 200 || x.status === 201 });
    sleep(2);
    const pos = get(`/api/cola/${d.evento}/${id}`, 100000 + __VU);
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
