// Pruebas de seguridad de la API: cada verificación intenta un ataque (acceso sin sesión, escalada de rol, IDOR,
// inyección SQL, fuerza bruta, tokens robados o falsos, cierre de sesión, cabeceras). Crea datos: úsalo contra el
// entorno de prueba, no contra producción.
//   docker compose -f pruebas/compose-carga.yml up -d --build
//   node pruebas/seguridad.mjs http://localhost:18080
const BASE = process.argv[2] || "http://localhost:18080";
let fallas = 0;
let total = 0;
const ok = (cond, texto) => {
  total++;
  if (!cond) fallas++;
  console.log(`${cond ? "✓" : "✗"} ${texto}`);
};
let ipN = 1;
const nuevaIP = () => `10.66.${Math.floor(ipN / 250)}.${ipN++ % 250}`;

async function pedir(metodo, ruta, { cuerpo, token, ip = nuevaIP(), headers = {} } = {}) {
  if (metodo === "GET") cuerpo = undefined;
  const h = { "X-Forwarded-For": ip, ...headers };
  if (cuerpo !== undefined) h["Content-Type"] = "application/json";
  if (token) h.Authorization = `Bearer ${token}`;
  const r = await fetch(BASE + ruta, { method: metodo, headers: h, body: cuerpo === undefined ? undefined : JSON.stringify(cuerpo) });
  let datos = {};
  try {
    datos = await r.json();
  } catch {}
  return { status: r.status, datos, h: r.headers };
}
const corrida = Date.now() % 1e6;
async function registrar(n) {
  const r = await pedir("POST", "/api/auth/registro", { cuerpo: { nombre: `Cliente ${"abcdefghij"[n]}rueba`, correo: `ataque${corrida}${n}@prueba.gt`, clave: "Palomitas#Dulces77" } });
  return r.datos;
}

console.log("── 1. Sin sesión, ninguna ruta privada responde datos");
const privadas = [
  ["GET", "/api/cuenta"], ["GET", "/api/usuarios"], ["GET", "/api/metricas"], ["GET", "/api/bitacora"], ["GET", "/api/clientes/historial"],
  ["POST", "/api/reservas"], ["POST", "/api/creditos/trivia"], ["GET", "/api/cola/e1"], ["POST", "/api/boletos/CE-X/validar"],
  ["GET", "/api/boletos/CE-X/codigos"], ["GET", "/api/salas/s1/fuera-servicio"], ["POST", "/api/peliculas"], ["POST", "/api/funciones"],
  ["POST", "/api/funciones/f1/asientos/A-01/bloqueo"], ["GET", "/api/funciones/f1/mis-bloqueos"], ["POST", "/api/auth/logout"],
];
const sinSesion = await Promise.all(privadas.map(([m, r]) => pedir(m, r, { cuerpo: {} })));
ok(sinSesion.every((r) => r.status === 401), `${privadas.length} rutas privadas sin token → 401 (${[...new Set(sinSesion.map((r) => r.status))].join(",")})`);
ok(sinSesion[0].h.get("www-authenticate")?.startsWith("Bearer"), "401 con WWW-Authenticate: Bearer");
const viejas = await Promise.all([pedir("POST", "/api/sesion", { cuerpo: { correo: "x@x.com", nombre: "Ana Perez" } }), pedir("GET", "/api/usuarios/1/cuenta")]);
ok(viejas.every((r) => r.status === 404 || r.status === 401), `rutas viejas que confiaban en el correo o el id ya no existen (${viejas.map((r) => r.status)})`);

console.log("── 2. Tokens falsos, robados o en lugares indebidos");
ok((await pedir("GET", "/api/cuenta", { token: "inventado" })).status === 401, "token inventado → 401");
ok((await pedir("GET", "/api/cuenta", { token: "x".repeat(5000) })).status === 401, "token gigante → 401 (sin error 500)");
const ana = await registrar(0);
const luis = await registrar(1);
ok(ana.token?.length >= 40 && luis.token, `registro entrega un token de ${ana.token?.length} caracteres (256 bits)`);
ok((await pedir("GET", `/api/cuenta?token=${ana.token}`)).status === 401, "el token en la URL no se acepta (sólo en la cabecera)");
ok((await pedir("GET", "/api/cuenta", { headers: { Cookie: `token=${ana.token}` } })).status === 401, "el token en una cookie no se acepta (sin CSRF posible)");
ok((await pedir("GET", "/api/cuenta", { token: ana.token })).datos.saldo === 800, "con su token, Ana ve su cuenta (800 créditos)");

console.log("── 3. Escalada de privilegios (cliente → personal)");
const comoCliente = await Promise.all(
  [["GET", "/api/usuarios"], ["GET", "/api/metricas"], ["GET", "/api/bitacora"], ["GET", "/api/clientes/historial"], ["POST", "/api/peliculas"], ["POST", "/api/boletos/CE-X/validar"], ["POST", "/api/salas/s1/fuera-servicio/A-01"], ["POST", "/api/usuarios"], ["POST", "/api/auth/reconfirmar"]].map(([m, r]) =>
    pedir(m, r, { token: ana.token, cuerpo: { rol: "admin", nombre: "Ana Admin", correo: "a@a.com", clave: "Hackeo#Total99", motivo: "danada" } }),
  ),
);
ok(comoCliente.every((r) => r.status === 403), `un cliente no entra a 9 rutas del personal → 403 (${[...new Set(comoCliente.map((r) => r.status))]})`);
ok((await pedir("POST", "/api/auth/registro", { cuerpo: { nombre: "Ana Admin", correo: `admin${corrida}@prueba.gt`, clave: "Palomitas#Dulces77", rol: "admin" } })).datos.usuario?.rol === "cliente", "mandar rol=admin al registrarse no sirve: queda como cliente");

console.log("── 4. Datos ajenos (IDOR)");
const f = (await (await fetch(`${BASE}/api/funciones`)).json()).find((x) => !x.evento && new Date(x.inicio) > new Date(Date.now() + 3600e3));
const res = await pedir("POST", "/api/reservas", { token: ana.token, cuerpo: { funcion: f.codigo, asientos: ["C-03"], usuario_id: 999 } });
ok(res.status === 201, "Ana reserva C-03 (el usuario_id del cuerpo se ignora)");
ok((await pedir("POST", `/api/reservas/${res.datos.reserva_id}/pagar`, { token: luis.token })).status === 404, "Luis no puede pagar la reserva de Ana con sus créditos (404)");
ok((await pedir("DELETE", `/api/reservas/${res.datos.reserva_id}`, { token: luis.token })).status === 404, "Luis no puede cancelar la reserva de Ana (404)");
const pago = await pedir("POST", `/api/reservas/${res.datos.reserva_id}/pagar`, { token: ana.token });
const tokAna = pago.datos.boletos?.[0]?.token;
ok(pago.status === 201 && tokAna, "Ana paga su reserva");
const repetido = await pedir("POST", `/api/reservas/${res.datos.reserva_id}/pagar`, { token: luis.token });
ok(!(repetido.datos.boletos?.length > 0), "repetir el pago de Ana como Luis no revela sus boletos");
ok((await pedir("GET", `/api/boletos/${tokAna}/codigos`, { token: luis.token })).status === 404, "Luis no obtiene los códigos del QR del boleto de Ana (404)");
const codigos = await pedir("GET", `/api/boletos/${tokAna}/codigos`, { token: ana.token });
ok(codigos.datos.codigos?.length === 10, "Ana obtiene los códigos de su QR para 10 minutos");
ok((await pedir("GET", `/api/funciones/${f.codigo}/asientos?usuario=1`)).datos.propios === undefined, "la ocupación pública no revela qué butacas tiene cada usuario");

console.log("── 5. Inyección SQL (debe dar 4xx, nunca 500, y la base intacta)");
const payloads = ["' OR '1'='1", "'; DROP TABLE usuarios; --", "\" OR 1=1 --", "1; SELECT pg_sleep(5)--", "admin@cineight.gt'--", "%27%20OR%201%3D1"];
const inyecciones = [];
for (const p of payloads) {
  inyecciones.push(await pedir("POST", "/api/auth/login", { cuerpo: { correo: p, clave: p } }));
  inyecciones.push(await pedir("POST", "/api/auth/login", { cuerpo: { correo: "auditor@cineight.gt", clave: p } }));
  inyecciones.push(await pedir("GET", `/api/funciones/${encodeURIComponent(p)}/asientos`));
  inyecciones.push(await pedir("GET", `/api/peliculas/${encodeURIComponent(p)}/poster`));
  inyecciones.push(await pedir("POST", "/api/reservas", { token: ana.token, cuerpo: { funcion: p, asientos: [p] } }));
  inyecciones.push(await pedir("GET", `/api/boletos/${encodeURIComponent(p)}/codigos`, { token: ana.token }));
  inyecciones.push(await pedir("POST", `/api/cola/${encodeURIComponent(p)}`, { token: ana.token, cuerpo: {} }));
}
const t0 = Date.now();
await pedir("POST", "/api/auth/login", { cuerpo: { correo: "x' OR pg_sleep(3)::text='", clave: "x" } });
ok(inyecciones.every((r) => r.status >= 400 && r.status < 500), `${inyecciones.length} intentos de inyección → todos 4xx (${[...new Set(inyecciones.map((r) => r.status))].join(",")}), ninguno 500`);
ok(Date.now() - t0 < 2500, "pg_sleep inyectado no se ejecuta (la respuesta no se demora)");
ok(!inyecciones.some((r) => r.datos.token), "ninguna inyección inicia sesión");
ok((await pedir("POST", "/api/auth/login", { cuerpo: { correo: `ataque${corrida}0@prueba.gt`, clave: "Palomitas#Dulces77" } })).status === 200, "la tabla usuarios sigue intacta (Ana inicia sesión)");

console.log("── 6. Fuerza bruta y enumeración de cuentas");
const correoV = `ataque${corrida}1@prueba.gt`;
const a1 = await pedir("POST", "/api/auth/login", { cuerpo: { correo: "no-existe@prueba.gt", clave: "Cualquiera#123" } });
const a2 = await pedir("POST", "/api/auth/login", { cuerpo: { correo: correoV, clave: "Cualquiera#123" } });
ok(a1.status === 401 && a2.status === 401 && a1.datos.error === a2.datos.error, "correo inexistente y contraseña incorrecta dan el mismo mensaje (no se enumeran cuentas)");
const intentos = [];
for (let i = 0; i < 5; i++) intentos.push((await pedir("POST", "/api/auth/login", { cuerpo: { correo: correoV, clave: `Mala#${i}abc1` } })).status);
ok(intentos.at(-1) === 429, `al 5.º intento fallido la cuenta se bloquea (${intentos.join(",")})`);
ok((await pedir("POST", "/api/auth/login", { cuerpo: { correo: correoV, clave: "Palomitas#Dulces77" } })).status === 429, "bloqueada no entra ni con la contraseña correcta");
const ipFija = "10.99.99.99";
const rafaga = (await Promise.all(Array.from({ length: 15 }, (_, i) => pedir("POST", "/api/auth/login", { ip: ipFija, cuerpo: { correo: `x${i}@prueba.gt`, clave: "Cualquiera#123" } })))).map((r) => r.status);
ok(rafaga.filter((s) => s === 429).length >= 5, `una IP no prueba más de 10 contraseñas por minuto: 15 a la vez → ${rafaga.filter((s) => s === 429).length} rechazadas con 429`);

console.log("── 7. Cierre de sesión y cambio de contraseña");
const telefono = (await pedir("POST", "/api/auth/login", { cuerpo: { correo: `ataque${corrida}0@prueba.gt`, clave: "Palomitas#Dulces77" } })).datos.token;
ok((await pedir("POST", "/api/auth/logout", { token: telefono })).status === 200, "cerrar sesión responde 200");
ok((await pedir("GET", "/api/cuenta", { token: telefono })).status === 401, "el token cerrado (aunque alguien lo copiara) ya no sirve");
const otro = (await pedir("POST", "/api/auth/login", { cuerpo: { correo: `ataque${corrida}0@prueba.gt`, clave: "Palomitas#Dulces77" } })).datos.token;
ok((await pedir("POST", "/api/auth/clave", { token: ana.token, cuerpo: { actual: "Palomitas#Dulces77", nueva: "123" } })).status === 400, "contraseña nueva débil → 400");
ok((await pedir("POST", "/api/auth/clave", { token: ana.token, cuerpo: { actual: "Palomitas#Dulces77", nueva: "Cineight#Segura2026" } })).status === 200, "Ana cambia su contraseña");
ok((await pedir("GET", "/api/cuenta", { token: otro })).status === 401, "cambiarla cierra las sesiones de los otros dispositivos");
ok((await pedir("GET", "/api/cuenta", { token: ana.token })).status === 200, "la sesión desde la que la cambió sigue abierta");
ok((await pedir("POST", "/api/auth/logout-todas", { token: ana.token })).datos.sesiones_cerradas >= 1, "«cerrar en todos los dispositivos» revoca todas");
ok((await pedir("GET", "/api/cuenta", { token: ana.token })).status === 401, "incluida la actual");

console.log("── 8. Personal con contraseña temporal y gestión de usuarios");
const adm = await pedir("POST", "/api/auth/login", { cuerpo: { correo: "admin@cineight.gt", clave: "Cine#Temporal2026" } });
ok(adm.status === 200 && adm.datos.usuario.debe_cambiar_clave, "el admin entra con la contraseña inicial y se le exige cambiarla");
ok((await pedir("GET", "/api/usuarios", { token: adm.datos.token })).status === 403, "con la contraseña temporal no puede usar el panel (403)");
ok((await pedir("POST", "/api/auth/clave", { token: adm.datos.token, cuerpo: { actual: "Cine#Temporal2026", nueva: "Admin#Nueva2026" } })).status === 400, "la contraseña nueva no puede contener el usuario del correo (admin@…)");
ok((await pedir("POST", "/api/auth/clave", { token: adm.datos.token, cuerpo: { actual: "Cine#Temporal2026", nueva: "Taquilla#Segura2026" } })).status === 200, "cambia la contraseña temporal");
const tAdm = adm.datos.token;
ok((await pedir("GET", "/api/usuarios", { token: tAdm })).status === 200, "tras cambiarla ya gestiona usuarios");
ok((await pedir("POST", "/api/usuarios", { token: tAdm, cuerpo: { nombre: "Carlos Nuevo", correo: `c${corrida}@cineight.gt`, rol: "agente", clave: "Temporal#Agente1" } })).status === 403, "crear usuarios exige reconfirmar la contraseña (403)");
ok((await pedir("POST", "/api/auth/reconfirmar", { token: tAdm, cuerpo: { clave: "Taquilla#Segura2026" } })).status === 200, "reconfirma su contraseña");
const nuevo = await pedir("POST", "/api/usuarios", { token: tAdm, cuerpo: { nombre: "Carlos Nuevo", correo: `c${corrida}@cineight.gt`, rol: "agente", clave: "Temporal#Agente1" } });
ok(nuevo.status === 201 && nuevo.datos.debe_cambiar_clave, "crea un agente con contraseña temporal");
const tAg = (await pedir("POST", "/api/auth/login", { cuerpo: { correo: `c${corrida}@cineight.gt`, clave: "Temporal#Agente1" } })).datos.token;
ok((await pedir("POST", `/api/usuarios/${nuevo.datos.id}/estado`, { token: tAdm, cuerpo: { activo: false } })).status === 200, "lo da de baja");
ok((await pedir("GET", "/api/auth/sesion", { token: tAg })).status === 401, "la sesión abierta del agente dado de baja se cierra al instante");
const yo = (await pedir("GET", "/api/auth/sesion", { token: tAdm })).datos.usuario;
ok((await pedir("POST", `/api/usuarios/${yo.id}`, { token: tAdm, cuerpo: { nombre: "Admin Sistema", rol: "cliente" } })).status === 400, "el admin no puede quitarse su propio rol");

console.log("── 9. Cabeceras de seguridad de la API");
const h = (await pedir("GET", "/api/funciones")).h;
ok(h.get("x-content-type-options") === "nosniff" && h.get("x-frame-options") === "DENY" && /frame-ancestors 'none'/.test(h.get("content-security-policy") ?? ""), "nosniff, X-Frame-Options DENY y CSP frame-ancestors 'none'");
ok(h.get("referrer-policy") === "no-referrer" && /max-age=63072000/.test(h.get("strict-transport-security") ?? ""), "Referrer-Policy y HSTS (detrás del proxy)");
ok((await pedir("GET", "/api/auth/sesion", { token: tAdm })).h.get("cache-control") === "no-store", "respuestas privadas con Cache-Control: no-store");

console.log(`\n${total - fallas}/${total} verificaciones correctas${fallas ? ` · ${fallas} FALLARON` : ""}`);
process.exit(fallas ? 1 : 0);
