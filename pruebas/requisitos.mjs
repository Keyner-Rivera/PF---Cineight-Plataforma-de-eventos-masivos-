// Verifica, contra PostgreSQL y Redis reales, los requisitos del prototipo funcional:
//  1. bloqueo transaccional en la base durante la selección y el pago (sin sobreventa con compras simultáneas)
//  2. el servidor recalcula precios y verifica la disponibilidad real sin confiar en el cliente
//  3. liberación automática de butacas al vencer la reserva
//  4. comprobantes y entradas con QR verificable
// Crea datos: córrelo contra el entorno de prueba con una retención corta (para ver el vencimiento en segundos):
//   RETENCION_SEGUNDOS=20 docker compose -f pruebas/compose-carga.yml up -d --build
//   node pruebas/requisitos.mjs pruebas/compose-carga.yml
import { execSync } from "node:child_process";

const COMPOSE = process.argv[2] ?? "pruebas/compose-carga.yml";
const BASE = process.argv[3] ?? "http://localhost:18080";
const RETENCION_S = Number(process.env.RETENCION_SEGUNDOS ?? 20);
let fallas = 0;
const ok = (c, t) => {
  if (!c) fallas++;
  console.log(`${c ? "✓" : "✗"} ${t}`);
};
const esperar = (ms) => new Promise((r) => setTimeout(r, ms));
let ipN = 1;
async function pedir(metodo, ruta, { cuerpo, token } = {}) {
  const h = { "X-Forwarded-For": `10.77.${Math.floor(ipN / 250)}.${ipN++ % 250}` };
  if (cuerpo !== undefined) h["Content-Type"] = "application/json";
  if (token) h.Authorization = `Bearer ${token}`;
  const r = await fetch(BASE + ruta, { method: metodo, headers: h, body: cuerpo === undefined ? undefined : JSON.stringify(cuerpo) });
  let datos = {};
  try {
    datos = await r.json();
  } catch {}
  return { status: r.status, datos };
}
const sql = (q) => execSync(`docker compose -f "${COMPOSE}" exec -T postgres psql -U cineight -d cineight -tAc "${q}"`).toString().trim();
const corrida = Date.now() % 1e6;
const letras = (n) => "abcdefghijklmnopqrstuvwxyz"[n % 26] + "abcdefghijklmnopqrstuvwxyz"[Math.floor(n / 26) % 26];
async function cliente(n) {
  const r = await pedir("POST", "/api/auth/registro", { cuerpo: { nombre: `Cliente ${letras(n).replace(/^./, (c) => c.toUpperCase())}prueba`, correo: `tx${corrida}-${n}@prueba.gt`, clave: "Palomitas#Dulces77" } });
  return r.datos.token;
}
const tokens = [];
for (let i = 0; i < 40; i += 4) tokens.push(...(await Promise.all([0, 1, 2, 3].map((j) => cliente(i + j)))));
console.log(`${tokens.filter(Boolean).length}/40 clientes registrados`);
const funciones = await (await fetch(`${BASE}/api/funciones`)).json();
const futuras = funciones.filter((f) => !f.evento && new Date(f.inicio) > new Date(Date.now() + 3600e3));
const [f1, f2, f3] = futuras;

console.log("\n── 1. Bloqueo transaccional en la base (selección y pago)");
const clics = await Promise.all(tokens.slice(0, 30).map((t) => pedir("POST", `/api/funciones/${f1.codigo}/asientos/C-05/bloqueo`, { token: t })));
const ganadores = clics.filter((r) => r.status === 201).length;
ok(ganadores === 1, `30 clientes eligen la misma butaca a la vez: ${ganadores} la obtiene, ${clics.filter((r) => r.status === 409).length} reciben 409`);
ok(sql(`SELECT count(*) FROM retenciones r JOIN funciones f ON f.id = r.funcion_id WHERE f.codigo = '${f1.codigo}' AND asiento = 'C-05'`) === "1", "la base registra una sola retención para esa butaca (clave primaria función + butaca)");
const dueno = tokens[clics.findIndex((r) => r.status === 201)];
const reservasMisma = await Promise.all(tokens.slice(0, 30).map((t) => pedir("POST", "/api/reservas", { token: t, cuerpo: { funcion: f1.codigo, asientos: ["C-05"] } })));
ok(reservasMisma.filter((r) => r.status === 201).length === 1, `30 reservas simultáneas de esa butaca: sólo la de quien la eligió prospera (${reservasMisma.filter((r) => r.status === 201).length})`);
const resDueno = reservasMisma[tokens.indexOf(dueno)];
const pagos = await Promise.all([dueno, dueno, dueno, ...tokens.slice(30, 33)].map((t) => pedir("POST", `/api/reservas/${resDueno.datos.reserva_id}/pagar`, { token: t })));
ok(pagos.filter((p) => p.status === 201).length === 1 && pagos.filter((p) => p.status === 200).length === 2, "3 pagos simultáneos del dueño: se cobra una vez (201) y los otros devuelven los mismos boletos (200)");
ok(pagos.slice(3).every((p) => p.status === 404), "otros clientes no pueden pagar esa reserva (404)");
ok(sql(`SELECT count(*) FROM boletos b JOIN funciones f ON f.id = b.funcion_id WHERE f.codigo = '${f1.codigo}' AND asiento = 'C-05'`) === "1", "un solo boleto emitido para C-05");
ok(sql(`SELECT count(*) FROM retenciones WHERE reserva_id = '${resDueno.datos.reserva_id}'`) === "0", "al pagar, la retención se borra en la misma transacción");
// Compra simultánea de 25 clientes en la misma función, cada uno 2 butacas al azar entre 12 posibles: nunca dos boletos por butaca.
const pool = ["D-01", "D-02", "D-03", "D-04", "D-05", "D-06", "D-07", "D-08", "D-09", "D-10", "E-01", "E-02"];
const intentos = await Promise.all(
  tokens.slice(5, 30).map(async (t, i) => {
    const a = pool[i % 12];
    const b = pool[(i * 5 + 3) % 12];
    const r = await pedir("POST", "/api/reservas", { token: t, cuerpo: { funcion: f2.codigo, asientos: a === b ? [a] : [a, b] } });
    return r.status === 201 ? pedir("POST", `/api/reservas/${r.datos.reserva_id}/pagar`, { token: t }) : r;
  }),
);
const duplicadas = sql(`SELECT count(*) FROM (SELECT asiento FROM boletos b JOIN funciones f ON f.id = b.funcion_id WHERE f.codigo = '${f2.codigo}' AND b.estado = 'valido' GROUP BY asiento HAVING count(*) > 1) x`);
const vendidas = sql(`SELECT count(*) FROM boletos b JOIN funciones f ON f.id = b.funcion_id WHERE f.codigo = '${f2.codigo}' AND b.reserva_id NOT LIKE 'taquilla:%'`);
ok(duplicadas === "0" && Number(vendidas) <= 12, `25 compras simultáneas sobre 12 butacas: ${vendidas} boletos, 0 butacas vendidas dos veces (${intentos.filter((r) => r.status === 201).length} compras, el resto 409)`);

console.log("\n── 2. El servidor recalcula el precio y verifica la disponibilidad real");
const tB = tokens[33];
const saldoAntes = (await pedir("GET", "/api/cuenta", { token: tB })).datos.saldo;
const trampa = await pedir("POST", "/api/reservas", { token: tB, cuerpo: { funcion: f3.codigo, asientos: ["F-01", "F-02"], precio: 1, total: 1, precio_unitario: 1 } });
ok(trampa.status === 201 && trampa.datos.precio_unitario === f3.precio && trampa.datos.total === f3.precio * 2, `mandar precio 1 no sirve: el servidor fija ${trampa.datos.precio_unitario} × 2 = ${trampa.datos.total} (tarifa de la función: ${f3.precio})`);
const pagoB = await pedir("POST", `/api/reservas/${trampa.datos.reserva_id}/pagar`, { token: tB, cuerpo: { total: 1 } });
const saldoDespues = (await pedir("GET", "/api/cuenta", { token: tB })).datos.saldo;
ok(pagoB.datos.total === f3.precio * 2 && saldoAntes - saldoDespues === f3.precio * 2, `se cobraron ${saldoAntes - saldoDespues} créditos, el total recalculado en la transacción`);
ok((await pedir("POST", "/api/reservas", { token: tB, cuerpo: { funcion: f3.codigo, asientos: ["Z-99"] } })).status === 400, "una butaca con formato inválido se rechaza (400)");
ok((await pedir("POST", "/api/reservas", { token: tB, cuerpo: { funcion: f3.codigo, asientos: ["P-16"] } })).status === 400, "una butaca que no existe en la sala se rechaza (400)");
ok((await pedir("POST", "/api/reservas", { token: tB, cuerpo: { funcion: f3.codigo, asientos: ["F-01"] } })).status === 409, "una butaca ya vendida se rechaza (409)");
ok((await pedir("POST", "/api/reservas", { token: tB, cuerpo: { funcion: f3.codigo, asientos: ["G-01", "G-01"] } })).status === 400, "butacas repetidas en la misma solicitud se rechazan (400)");
// La butaca pasa a fuera de servicio entre la reserva y el pago: el pago lo detecta dentro de la transacción.
const adm = await pedir("POST", "/api/auth/login", { cuerpo: { correo: "admin@cineight.gt", clave: "Cine#Temporal2026" } });
await pedir("POST", "/api/auth/clave", { token: adm.datos.token, cuerpo: { actual: "Cine#Temporal2026", nueva: "Taquilla#Segura2026" } });
const tC = tokens[34];
const rC = await pedir("POST", "/api/reservas", { token: tC, cuerpo: { funcion: f3.codigo, asientos: ["H-03"] } });
const salaF3 = f3.sala;
await pedir("POST", `/api/salas/${salaF3}/fuera-servicio/H-03`, { token: adm.datos.token, cuerpo: { motivo: "danada" } });
const saldoC = (await pedir("GET", "/api/cuenta", { token: tC })).datos.saldo;
const pagoC = await pedir("POST", `/api/reservas/${rC.datos.reserva_id}/pagar`, { token: tC });
ok(pagoC.status === 409 && (await pedir("GET", "/api/cuenta", { token: tC })).datos.saldo === saldoC, `si la butaca se daña entre la reserva y el pago, el pago la detecta: ${pagoC.status} y no se cobra nada`);

console.log("\n── 3. Liberación automática al vencer la reserva (retención de 20 s en este entorno)");
const tD = tokens[35];
const rD = await pedir("POST", "/api/reservas", { token: tD, cuerpo: { funcion: f3.codigo, asientos: ["J-07", "J-08"] } });
ok(sql(`SELECT count(*) FROM retenciones WHERE reserva_id = '${rD.datos.reserva_id}'`) === "2", "la reserva queda retenida en la base (2 filas con vencimiento)");
const tE = tokens[36];
ok((await pedir("POST", "/api/reservas", { token: tE, cuerpo: { funcion: f3.codigo, asientos: ["J-07"] } })).status === 409, "mientras está vigente, nadie más la puede reservar");
console.log(`   esperando a que venza (≈${RETENCION_S + 32} s)…`);
await esperar((RETENCION_S + 2) * 1000);
const tardio = await pedir("POST", `/api/reservas/${rD.datos.reserva_id}/pagar`, { token: tD });
ok(tardio.status === 410, `pagar después del vencimiento: ${tardio.status} (la reserva expiró)`);
await esperar(30000);
ok(sql(`SELECT count(*) FROM retenciones WHERE reserva_id = '${rD.datos.reserva_id}'`) === "0", "la limpieza periódica borró sus retenciones de la base");
const ocup = await (await fetch(`${BASE}/api/funciones/${f3.codigo}/asientos`)).json();
ok(!ocup.retenidos.includes("J-07") && !ocup.vendidos.includes("J-07"), "el mapa vuelve a mostrar J-07 libre");
const rE = await pedir("POST", "/api/reservas", { token: tE, cuerpo: { funcion: f3.codigo, asientos: ["J-07"] } });
ok(rE.status === 201, "y otro cliente ya puede reservarla");

console.log("\n── 4. Comprobante y entradas con QR verificable");
const folio = pagoB.datos.reserva_id;
const comp = await pedir("GET", `/api/compras/${folio}`, { token: tB });
ok(comp.status === 200 && comp.datos.total === f3.precio * 2 && comp.datos.asientos.join() === "F-01,F-02" && comp.datos.firma?.length === 16, `el comprobante ${folio} trae el total del servidor, las butacas y su firma`);
ok((await pedir("GET", `/api/compras/${folio}`, { token: tC })).status === 404, "otro cliente no ve el comprobante (404)");
const ver = await pedir("GET", `/api/comprobantes/${folio}/verificar?firma=${comp.datos.firma}`);
ok(ver.datos.valido && !ver.datos.comprobante.correo && !JSON.stringify(ver.datos).includes("CE-"), "su QR verifica públicamente: auténtico, sin correo ni códigos de entrada");
ok(!(await pedir("GET", `/api/comprobantes/${folio}/verificar?firma=AAAAAAAAAAAAAAAA`)).datos.valido, "una firma alterada no verifica");
const tok = comp.datos.boletos?.[0]?.token;
const cod = await pedir("GET", `/api/boletos/${tok}/codigos`, { token: tB });
ok(cod.datos.codigos?.length === 10, "la entrada recibe sus códigos QR firmados por el servidor");
const val = await pedir("POST", `/api/boletos/${tok}/validar`, { token: adm.datos.token, cuerpo: { codigo: cod.datos.codigos[0], ventana: cod.datos.desde } });
ok(val.datos.ok === true, "en la sala, el QR de la entrada se valida (acceso permitido)");
const ver2 = await pedir("GET", `/api/comprobantes/${folio}/verificar?firma=${comp.datos.firma}`);
ok(ver2.datos.vigentes === 1 && ver2.datos.utilizados === 1, "el comprobante refleja 1 entrada usada y 1 vigente");

console.log(`\n${fallas ? `${fallas} verificaciones fallaron` : "todas las verificaciones pasaron"}`);
process.exit(fallas ? 1 : 0);
