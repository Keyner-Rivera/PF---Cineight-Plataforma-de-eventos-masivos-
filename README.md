# Cineight — Prototipo

Prototipo de la plataforma de boletería y eventos masivos descrita en la ERS v1.4 (Análisis de Sistemas II, UMG).
Usa el stack de la Tabla 19 / Sección 3.4.3: **Next.js 16 + Tailwind CSS + Framer Motion**, **Go 1.26**, **PostgreSQL 18** y **Redis 8**.

## Novedades de esta versión

- **Butacas en tiempo real:** cada butaca elegida queda bloqueada a tu nombre 5 min (candado atómico en Redis) y los demás
  clientes la ven bloqueada al instante por **Server-Sent Events**. Si recargas la página recuperas tus propios bloqueos.
- **reCAPTCHA v3 invisible** en inicio de sesión, registro y cola virtual, verificado por el backend antes de procesar credenciales.
  Al aprobar el token, el backend entrega un **pase de un solo uso** (Redis, 10 min) que `POST /api/sesion` exige para crear
  una cuenta nueva: un bot ya no puede saltarse la verificación llamando directamente a la API para fabricar cuentas con bono.
- **Estadísticas de clientes** (Administrador → *Clientes*): historial de compras de cada cliente con películas vistas,
  géneros, directores y actores favoritos, gasto y última compra; búsqueda, orden y exportación a CSV.
- **Cartelera siempre vigente:** la API programa sola (al arrancar y cada hora) lo que falte de hoy y los 3 días
  siguientes en las 16 salas, con el mismo plan que el frontend, y mantiene un preestreno por delante. Con la API
  conectada, la cartelera del navegador es exactamente la de PostgreSQL (ya no se mezcla con la generada en el
  navegador), y la API rechaza bloquear o reservar funciones que ya comenzaron.
- **Resiliencia:** si Redis cae, las rutas de reservas responden `503` con `{"error": "Servicio de reservas
  temporalmente no disponible"}` en lugar de fallar; la cartelera sigue funcionando y todo se recupera solo cuando
  Redis vuelve. Un middleware `recover()` convierte cualquier panic en un error JSON. El contenedor de la API corre
  con un usuario sin privilegios (`appuser`). El canal SSE se reconecta con espera exponencial (1 s → 30 s).
- **Compra en un solo paso:** *Reservar* → *Pagar* (sin el paso intermedio de retener créditos), aviso de saldo
  insuficiente antes de pagar, indicador de pasos y, si un visitante toca una butaca sin sesión, se le aparta al iniciarla.
- **Catálogo editable:** el agente operativo y el administrador publican películas con póster, ficha técnica, tráiler y
  funciones desde el panel, con detección de traslapes en vivo.
- **Programación completa:** 24 películas (8 en cartelera, 8 estrenos, 8 próximamente) y 16 salas; cada película en
  exhibición tiene **5 funciones diarias de 10:00 a 22:00** (la primera a las 10:00 y la última entre 21:00 y 21:30),
  siempre para hoy y los 3 días siguientes.
- **Sin datos de prueba:** no hay modo demostración, accesos rápidos, clientes de prueba ni butacas vendidas inventadas.
  Todo sale de PostgreSQL y Redis; si la API no responde, el sitio lo dice y ofrece reintentar.
- **Preestreno con cola virtual real:** *Duna: Parte Dos* en IMAX se vende por una fila en Redis por orden de llegada;
  el servidor atiende a pocos clientes a la vez (turnos de 3 min) y rechaza elegir butacas del preestreno sin turno.
- **Interfaz:** logotipo en la barra superior, botones con íconos y animaciones, carruseles horizontales, horarios por día,
  navegación con íconos, menú de usuario con *Cerrar sesión* (lleva a la cartelera) y panel de administración con barra
  lateral fija.
- **Código reorganizado** por responsabilidades en el frontend y arquitectura por capas en el backend (ver estructura).

```
cineight/
├── frontend/                 Next.js (App Router) — interfaz del cliente y panel administrativo
│   └── src/
│       ├── app/              Rutas. (sitio)/ = público con pie de página · panel/ = personal, a pantalla completa
│       ├── components/
│       │   ├── ui/           Piezas reutilizables: Boton, BotonIcono, BotonVolver, Modal, Carrusel, Poster
│       │   ├── layout/       Header, Logo, MenuUsuario, PiePagina, GuardiaRol, Ventanas
│       │   ├── auth/         LoginModal (reCAPTCHA v3), AvisoRecaptcha
│       │   ├── cartelera/    Destacado, TarjetaPelicula, BannerTrivia
│       │   ├── pelicula/     HorariosPorDia, Trailer
│       │   ├── funcion/      MapaButacas, piezas del checkout
│       │   ├── boletos/      Boleto, VisorQR
│       │   ├── trivia/       TriviaJuego, TriviaModal, AvisoTrivia
│       │   └── panel/        MarcoPanel (barra lateral fija) + una sección por archivo
│       │                     (Películas + FormPelicula, Programar, Usuarios, Reportes…)
│       ├── hooks/            useAsientosEnVivo (SSE), useRecaptcha, useCerrarSesion, usePanelDatos…
│       ├── services/         api.ts (cliente HTTP), recaptcha.ts, mapeadores.ts (Go → interfaz)
│       ├── store/            AppProvider: estado global (sesión, compra, créditos)
│       ├── data/             catálogo de películas y salas, cuentas iniciales del personal y parámetros globales
│       ├── types/            tipos del dominio y de la API
│       └── utils/            formato, programación (traslapes y huecos libres), imagen del póster, CSV, QR, cola
├── backend/                  API REST en Go (arquitectura por capas)
│   ├── cmd/api/              main.go: configuración y cableado de dependencias
│   ├── internal/
│   │   ├── config/           variables de entorno
│   │   ├── domain/           entidades, reglas fijas y errores de negocio
│   │   ├── repository/       PostgreSQL (catálogo, ventas, cuentas) y Redis (candados, reservas, cola, Pub/Sub)
│   │   ├── services/         lógica de negocio (depende de interfaces, no de implementaciones)
│   │   └── handlers/         rutas HTTP, JSON y SSE
│   └── pkg/                  utilidades sin dependencias del dominio: httpx, recaptcha, token
├── db/init.sql               Esquema PostgreSQL (snake_case en español) + catálogo, personal y programación inicial
└── docker-compose.yml        PostgreSQL + Redis + API
```

## Tecnologías

| Capa | Tecnología | Estado en el prototipo |
|---|---|---|
| Frontend | Next.js 16 (React 19, App Router), TypeScript, Tailwind CSS 4, Framer Motion, lucide-react, qrcode.react | Implementado. Todos los datos vienen de la API; sólo las cuentas para iniciar sesión viven aún en el navegador |
| Backend | Go 1.26 (`net/http`, pgx, go-redis) | Implementado: bloqueo de butacas en tiempo real (SSE), reservas, pago con créditos, cola virtual, reCAPTCHA v3, rate limiting y métricas |
| Base de datos | PostgreSQL 18 | Implementado en Docker (`db/init.sql`) |
| Caché / candados | Redis 8 | Implementado en Docker (retención de 5 min con TTL, cola, rate limiting) |
| Infraestructura local | Docker Compose | PostgreSQL + Redis + API |

**Pendiente:** autenticación en el servidor (JWT + bcrypt). Hoy las cuentas y contraseñas se validan en el navegador de
cada visitante, así que un cliente registrado en un equipo no existe en otro, y no se puede cambiar la contraseña.

## Cómo abrirlo en tu computadora

### 1. Requisitos

| Programa | Versión | Para qué | Descarga |
|---|---|---|---|
| Git | cualquiera | Descargar el proyecto | https://git-scm.com/downloads |
| Node.js | 20 o superior (LTS) | Frontend | https://nodejs.org |
| Docker Desktop | reciente | API en Go, PostgreSQL y Redis | https://www.docker.com/products/docker-desktop |

La API es necesaria: sin ella el sitio muestra *Servicio no disponible*.
No hace falta instalar Go, PostgreSQL ni Redis: Docker los ejecuta.

### 2. Descargar el proyecto

```bash
git clone https://github.com/Gione02/Cineprototipo.git
cd Cineprototipo
```

¿Ya lo tenías descargado? Actualízalo con `git pull`.

### 3. Levantar el backend (API + base de datos + Redis)

Abre Docker Desktop y espera a que diga *Engine running*. Luego, en la carpeta del proyecto:

```bash
docker compose up -d --build
```

La primera vez tarda unos minutos. Comprueba que funciona abriendo http://localhost:8080/api/salud (debe responder).

> **Si ya lo habías levantado con una versión anterior**, reinicia la base de datos: esta versión agrega salas, películas,
> funciones y columnas nuevas. `docker compose down -v` y luego `docker compose up -d --build`.

### 4. Levantar el frontend

```bash
cd frontend
npm install
```

Crea el archivo de configuración copiando el de ejemplo:

```bash
cp .env.example .env.local          # macOS / Linux / Git Bash
copy .env.example .env.local        # Windows (CMD o PowerShell)
```

Y arranca la aplicación:

```bash
npm run dev
```

Abre **http://localhost:3000**. Si la API no está levantada verás *Servicio no disponible* con un botón para reintentar.

### 5. Detener todo

- Frontend: `Ctrl + C` en la terminal donde corre `npm run dev`.
- Backend: `docker compose down` (añade `-v` para borrar también la base de datos).

### Problemas comunes

| Síntoma | Solución |
|---|---|
| *Servicio no disponible* | Verifica que Docker Desktop esté abierto y que `docker compose ps` muestre `api`, `postgres` y `redis` en ejecución. Revisa que exista `frontend/.env.local` y reinicia `npm run dev`. |
| `port is already allocated` (5432, 6379 u 8080) | Otro programa usa ese puerto (por ejemplo un PostgreSQL instalado). Detenlo o cambia el puerto izquierdo en `docker-compose.yml`. |
| `port 3000 is in use` | Next.js usará el 3001; en ese caso agrega `http://localhost:3001` a `CORS_ORIGINS` en `docker-compose.yml` y vuelve a ejecutar `docker compose up -d`. |
| `npm` o `node` no se reconoce | Instala Node.js y abre una terminal nueva. |
| Faltan datos o hay errores tras actualizar | `docker compose down -v` y `docker compose up -d --build`. |

### Cuentas iniciales del personal

Los clientes se registran desde *Iniciar sesión → Registrarse* (reciben 800 créditos de bienvenida). Para entrar al panel
de operaciones existen estas cuentas, con la contraseña inicial `cineight123`:

| Rol | Correo |
|---|---|
| Administrador del Sistema | `admin@cineight.gt` |
| Agente Operativo | `agente@cineight.gt` |
| Auditor | `auditor@cineight.gt` |

El administrador crea, edita o da de baja más cuentas en *Panel → Usuarios & roles*. Mientras la autenticación siga en el
navegador, la contraseña inicial es pública (está en el código del sitio): no la uses para nada fuera de este proyecto.

## Publicar en internet

La arquitectura se reparte en cuatro servicios; todos tienen plan gratuito:

| Pieza | Dónde | Qué se configura |
|---|---|---|
| Frontend (Next.js) | **Vercel** | carpeta raíz `frontend`, variables `NEXT_PUBLIC_*` |
| API (Go, Docker) | **Render** (Web Service) | carpeta raíz `backend`, variables de `backend/.env.example` |
| PostgreSQL | Render Postgres, Neon o Supabase | cargar `db/init.sql` una vez |
| Redis | Render Key Value o Upstash | su URL `redis://` o `rediss://` |

`render.yaml` crea la API, PostgreSQL y Redis en Render de una vez (**New → Blueprint**). Paso a paso:

1. **Sube el proyecto a GitHub** (`git push`). Las claves no se suben: `.env` y `.env.local` están en `.gitignore`.
2. **Render → New → Blueprint** → elige el repositorio. Crea `cineight-api`, `cineight-db` y `cineight-redis`.
3. **Carga el esquema** una sola vez. Copia la *External Database URL* de `cineight-db` y, con Docker (no hace falta
   instalar PostgreSQL), ejecuta en la carpeta del proyecto:
   ```bash
   docker run --rm -v "${PWD}/db:/db" postgres:18-alpine psql "URL_EXTERNA_DE_LA_BASE" -f /db/init.sql
   ```
   (Funciona igual en PowerShell y en Git Bash. Debe terminar sin `ERROR`; si la base ya tenía tablas, bórralas antes.)
4. **Vercel → Add New → Project** → el mismo repositorio, *Root Directory* = `frontend`, y estas variables:
   - `NEXT_PUBLIC_API_URL` = URL pública de la API (p. ej. `https://cineight-api.onrender.com`, sin barra final).
   - `NEXT_PUBLIC_RECAPTCHA_SITE_KEY` = clave pública de reCAPTCHA (opcional).
5. En Render, en `cineight-api` → *Environment*: `CORS_ORIGINS` = URL de Vercel (p. ej. `https://cineight.vercel.app`)
   y, si usas reCAPTCHA, `RECAPTCHA_SECRET`. Agrega el dominio de Vercel a los dominios del sitio en reCAPTCHA.
6. Comprueba: `https://…onrender.com/api/salud` responde `{"estado":"ok"}` y el pie del sitio dice **Datos en vivo**.

Notas:

- **El plan gratuito de Render duerme la API** tras 15 min sin uso; la primera visita tarda hasta ~1 min. El frontend
  lo sabe: muestra *Conectando con el servidor…* y reintenta durante un minuto antes de mostrar *Servicio no disponible*.
- **`TRUST_PROXY=true`** (ya puesto en `render.yaml`): detrás del proxy del hosting, el límite anti-bots se cuenta por
  cliente y no para todos juntos. No lo actives si la API no está detrás de un proxy.
- **Redis administrado** (Upstash, Render Key Value) no permite activar las notificaciones de expiración; no pasa
  nada: el canal en vivo reenvía la foto completa del mapa cada 20 s, así los bloqueos vencidos se liberan igual.
- Con varios usuarios de la misma red (p. ej. un aula), todos comparten IP: si ves `429`, sube `RATE_LIMIT`.
- Otro hosting con Docker (Railway, Fly.io…) sirve igual: usa `backend/Dockerfile` y las variables de
  `backend/.env.example`; la API toma el puerto de `PORT`.

### Versión de producción (opcional)

Para medir rendimiento con Lighthouse usa la compilación de producción en una ventana de incógnito:

```bash
cd frontend
npm run build
npm start          # http://localhost:3000
```

### Configuración

- `NEXT_PUBLIC_API_URL` (en `frontend/.env.local`): URL de la API (obligatoria).
- **Base de datos (API, obligatoria):** `DATABASE_URL`, o bien `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER`, `DB_PASSWORD`
  y `DB_SSLMODE`. No hay credenciales por defecto en el código: si faltan, la API no arranca. En local las pone
  `docker-compose.yml` (puedes cambiarlas con `POSTGRES_USER` / `POSTGRES_PASSWORD` en un archivo `.env`).
- `DB_MAX_CONNS` / `DB_MIN_CONNS` (API): tamaño del pool de PostgreSQL, 25 y 5 por defecto. Con el plan gratuito de
  Supabase usa un máximo menor al límite de conexiones de tu plan (p. ej. 10).
- `CACHE_CARTELERA_SEGUNDOS` (API): segundos que `GET /api/funciones` se sirve desde Redis. 15 por defecto; 0 la desactiva.
- `CORS_ORIGINS` (en `docker-compose.yml`): orígenes que pueden llamar a la API. Por defecto `http://localhost:3000,http://127.0.0.1:3000`.
- `RETENCION_SEGUNDOS` (API, opcional): vida del bloqueo de una butaca y de una reserva. Por defecto 300.

### Pruebas

```bash
# Pruebas unitarias del backend (sin base de datos: usan dobles en memoria)
docker run --rm -v "${PWD}/backend:/app" -w /app golang:1.26-alpine go test ./...

# Vulnerabilidades conocidas en las dependencias de Go
docker run --rm -v "${PWD}/backend:/app" -w /app golang:1.26-alpine go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Carga: 500 usuarios simultáneos contra la API local (no compra ni deja datos)
docker run --rm -i -e BASE=http://host.docker.internal:8080 grafana/k6 run - < pruebas/script_carga.js
```

En PowerShell, la prueba de carga se ejecuta con
`Get-Content pruebas\script_carga.js -Raw | docker run --rm -i -e BASE=http://host.docker.internal:8080 grafana/k6 run -`.

#### reCAPTCHA v3 (opcional)

El inicio de sesión, el registro y la cola virtual usan reCAPTCHA v3 invisible: no hay casillas ni imágenes.
El navegador obtiene un token con un puntaje y el backend en Go lo verifica con Google **antes** de procesar las credenciales.

1. Crea un sitio tipo **reCAPTCHA v3** en https://www.google.com/recaptcha/admin y agrega el dominio `localhost`.
2. Clave del sitio (pública) → `NEXT_PUBLIC_RECAPTCHA_SITE_KEY` en `frontend/.env.local`.
3. Clave secreta → archivo `.env` en la raíz del proyecto (Docker Compose lo lee solo; no se sube a git):

   ```
   RECAPTCHA_SECRET=tu_clave_secreta
   RECAPTCHA_MIN_SCORE=0.5
   ```

4. Reinicia: `docker compose up -d` y `npm run dev`.

Sin claves, el formulario sigue funcionando con las otras dos capas anti-bots (campo trampa y tiempo mínimo de envío).

### Programación de funciones

`db/init.sql` y la API (`backend/internal/domain/programacion.go`) usan el **mismo plan**: las 16 películas en exhibición
forman 8 pares (una larga con una corta) y cada par ocupa dos salas del mismo formato con el orden invertido. Todas las
salas abren a las 10:00 y la siguiente función empieza al terminar la anterior más 20 min de limpieza (RF-31), sin inicios
después de las 22:00. Así, **cada película tiene función a las 10:00 todos los días y 5 funciones repartidas hasta la noche**
(la última entre 21:00 y 21:30); lo comprueban pruebas automáticas en Go. Antes de las 13:00 hay 10 créditos de descuento.

| Formato | Salas | Películas |
|---|---|---|
| IMAX Láser | 1 y 9 | *Duna: Parte Dos* · *Top Gun: Maverick* |
| Dolby Atmos | 2 y 12 | *The Batman* · *Whiplash* |
| Dolby Atmos | 5 y 10 | *Furiosa* · *Deadpool & Wolverine* |
| 4DX 3D | 4 y 16 | *Avatar: El Camino del Agua* · *Intensa-Mente 2* |
| 2D VIP | 7 y 15 | *Oppenheimer* · *Civil War* |
| 2D | 3 y 13 | *Gladiador* · *Alien: Romulus* |
| 2D | 6 y 8 | *El Caballero de la Noche* · *Matrix* |
| 2D | 11 y 14 | *El Planeta de los Simios: Nuevo Reino* · *Todo en todas partes al mismo tiempo* |

`init.sql` programa 4 días a partir de su creación; después, la API completa sola (al arrancar y cada hora) lo que falte
de hoy y los 3 días siguientes, así la cartelera nunca se vacía. El **preestreno** con cola virtual es la última función
de *Duna: Parte Dos* de mañana en la Sala 1 IMAX (80 créditos); cuando empieza, la API elige el del día siguiente.

### Cola virtual del preestreno

1. El cliente pasa la verificación anti-bots (reCAPTCHA v3) y entra a la fila (`POST /api/cola/e1`).
2. La API admite por orden de llegada a `COLA_CAPACIDAD` clientes a la vez (5 por defecto), pero nunca más de los que
   alcanzan las butacas sin tomar (cada cliente puede llevar hasta 2). Cada turno empieza con **un minuto de gracia**:
   si el cliente no bloquea ninguna butaca, pasa al siguiente; si bloquea, el turno dura `COLA_TURNO_MINUTOS` (3)
   desde que lo recibió. Al pagar o vencer, el cupo pasa al siguiente.
3. La cola conoce el inventario: muestra a quien espera cuántas butacas quedan y si su lugar **alcanza**, es
   **incierto** o **no alcanza**; cuando no queda ninguna se cierra con *Boletos agotados* y no admite a nadie más.
   Tampoco admite a quien ya compró sus 2 boletos del preestreno.
4. Quien espera consulta su lugar cada 2,5 s (y de inmediato al volver a la pestaña); si deja de hacerlo durante 2 min
   (cerró la página) pierde su lugar.
5. Bloquear o reservar butacas del preestreno sin turno vigente responde `403`: la cola no se puede saltar.

Todo ocurre en un script Lua atómico sobre tres ZSET de Redis (`cola:`, `turnos:`, `latidos:`), así dos clientes nunca
ocupan el mismo cupo. Para probarla: abre el preestreno en tres navegadores con clientes distintos; el tercero espera.

## Recorrido sugerido

1. **Iniciar sesión** → *Registrarse* para crear una cuenta de cliente, o entra con una cuenta del personal (ver arriba).
2. **Cliente:** cartelera en carruseles horizontales (*En cartelera*, *Estrenos*, *Próximamente*) → ficha de la película
   (director, reparto, sinopsis, tráiler en ventana emergente) → horarios por día → mapa de butacas → *Comprar boletos*.
   Abre la misma función en dos navegadores con clientes distintos: lo que bloquea uno aparece al instante en el otro.
   La compra continúa en una ventana emergente con cuenta regresiva de 5 min: *Pagar* → ventana de compra exitosa.
   Si el saldo no alcanza, se avisa antes de pagar y se ofrece la trivia sin perder las butacas.
3. **Mis Entradas (seguridad):** los QR aparecen borrosos; al tocarlos se abre un visor con código dinámico que cambia cada minuto, marca de agua con el correo del titular,
   cierre automático a los 2 min o al cambiar de pestaña. Cada usuario tiene sus propios créditos, boletos y movimientos. El validador del acceso rechaza códigos vencidos (capturas).
   La sesión se cierra sola tras 15 min sin actividad.
4. **Créditos y trivia:** la trivia sólo se abre tocando **tus créditos** en el encabezado (un aviso emergente lo recuerda).
   3 preguntas: 10 créditos por acierto y 50 si se aciertan las 3.
5. **Noche de estreno (alta demanda):** la función de estreno de *Duna: Parte Dos* se vende con verificación anti-bots →
   cola virtual con posición y tiempo estimado → turno de 3 min para elegir asiento (límite 2 boletos por cliente).
6. **Agente Operativo / Administrador → Películas:** *Nueva película* con póster (arrastrar o elegir imagen), ficha técnica,
   tráiler y sus funciones. Cada función se revisa en vivo contra la programación (duración + 20 min de limpieza) y se
   propone el primer horario libre. Se guarda en PostgreSQL (póster incluido) y si alguna función choca no se guarda nada.
7. **Agente Operativo / Administrador → Programación:** programar una función para cualquier película (detecta traslapes
   y sugiere horarios libres). El agente además tiene inventario por función y validación de boletos en el acceso.
8. **Administrador → Usuarios & roles:** crear, editar y eliminar (dar de baja) cuentas de cualquier rol. Cada acción pide
   reconfirmar la contraseña del administrador (3 intentos, vale 5 min). No puede cambiar su propio rol
   ni darse de baja, y siempre queda al menos un administrador activo. Una cuenta dada de baja no puede iniciar sesión.
9. **Mis Entradas:** los boletos de la misma función se agrupan en una tarjeta («2 entradas»); el visor muestra el QR de cada
   butaca y se cambia con flechas, teclado, deslizando o tocando la butaca.
10. **Administrador:** métricas en vivo, parámetros vigentes (sólo lectura), reportes (exporta CSV) y bitácora.
    En *Clientes*, las preferencias de cada cliente se ponderan por **visitas** (funciones distintas a las que asistió),
    no por boletos: quien compra 4 butacas para su familia vio la película una vez.
11. **Auditor:** reportes y bitácora en solo lectura.
12. **Cerrar sesión:** desde el menú del usuario (esquina superior derecha); limpia la sesión y vuelve a la cartelera.

## Cómo funciona por dentro

| Pieza | Implementación |
|---|---|
| Bloqueo al elegir una butaca | candado atómico por butaca en Redis (script Lua equivalente a `SET NX EX 300`), máx. 6 por cliente |
| Mapa en tiempo real | SSE (`/api/funciones/{id}/stream`) alimentado por Redis Pub/Sub, con foto completa cada 20 s |
| Retención de 5 min | `SET NX` + TTL de 300 s en Redis |
| Sobreventa (RF-54, RNF-36) | candados atómicos + índice único parcial en `boletos` |
| reCAPTCHA v3 (RF-53) | `POST /api/captcha/verificar` contra Google (puntaje y acción) y pase de un solo uso para crear cuentas |
| Pago con créditos | transacción con `SELECT … FOR UPDATE`, todo o nada; repetirlo no vuelve a cobrar (RF-23) |
| Cola virtual | fila FIFO en Redis (script Lua atómico), turnos con vencimiento exigidos al bloquear o reservar |
| Anti-bots (RF-53) | campo trampa + tiempo mínimo de envío + límite de 5 solicitudes/min por IP en reservas y cola |
| Alta de películas | transacción en PostgreSQL con póster (`bytea`), validación del tipo real de imagen y de traslapes |

Todavía en el navegador (pendiente: JWT + bcrypt): el inicio de sesión, la gestión de usuarios y la bitácora.

| Método | Ruta | Uso |
|---|---|---|
| GET | `/api/funciones` | Cartelera vigente con butacas disponibles |
| GET | `/api/funciones/{codigo\|id}/asientos[?usuario=ID]` | Butacas vendidas y retenidas (y con `usuario`, cuáles son suyas) |
| GET | `/api/funciones/{codigo\|id}/stream` | **SSE**: evento `estado` (foto inicial) y `asientos` (`bloqueado`, `liberado`, `vendido`) |
| POST | `/api/funciones/{codigo\|id}/asientos/{butaca}/bloqueo` | `{usuario_id}` → candado de la butaca por 5 min (409 si la tiene otro) |
| DELETE | `/api/funciones/{codigo\|id}/bloqueos` | `{usuario_id, asientos}` → suelta butacas propias |
| GET | `/api/salas/{sala}/fuera-servicio` | Butacas dañadas o fuera de servicio de la sala (no se venden en ninguna de sus funciones) |
| POST | `/api/boletos/{token}/validar` | `{codigo, ventana}` → marca el boleto como utilizado (una sola vez) si el código dinámico de su QR es vigente; si no, lo rechaza sin consumirlo |
| POST | `/api/salas/{sala}/fuera-servicio/{asiento}` | `{motivo: "danada"\|"fuera-servicio", por}` → retira la butaca de la venta |
| DELETE | `/api/salas/{sala}/fuera-servicio/{asiento}?por=` | Devuelve la butaca a la venta |
| POST | `/api/reservas` | `{funcion, usuario_id, asientos}` → retención de 5 min |
| POST | `/api/reservas/{id}/pagar` | Pago idempotente → boletos y saldo |
| DELETE | `/api/reservas/{id}` | Libera la retención |
| POST | `/api/sesion` | `{correo, nombre, pase?}` → usuario y cuenta (bono de 800). Con reCAPTCHA activo, crear una cuenta nueva exige el `pase` (403 sin él) |
| GET | `/api/usuarios/{id}/cuenta` | Saldo, movimientos y boletos |
| POST | `/api/creditos/trivia` | `{usuario_id, partida, aciertos}` → acreditación única por partida |
| GET | `/api/metricas` | KPIs del panel de administración |
| GET | `/api/clientes/historial` | Compras de cada cliente agrupadas por película (boletos, visitas, gasto, última compra) |
| POST | `/api/cola/{evento}` | `{usuario_id, pase?}` → entra a la cola virtual: posición en la fila o turno con vencimiento |
| GET | `/api/cola/{evento}/{usuario}` | Posición o turno (consultarlo mantiene el lugar en la fila) |
| DELETE | `/api/cola/{evento}/{usuario}` | Sale de la fila o libera el turno (p. ej. tras comprar) |
| GET | `/api/ocupacion` | Ocupación de todas las funciones vigentes en una respuesta (panel) |
| GET | `/api/peliculas` | Películas creadas desde el panel (ficha completa) |
| POST | `/api/peliculas` | Agente/administrador: ficha + póster (data URL, máx. 2 MB) + funciones, en una transacción. 409 si alguna función se traslapa |
| GET | `/api/peliculas/{slug}/poster` | Imagen del póster guardada en PostgreSQL |
| POST | `/api/funciones` | `{pelicula, sala, inicio, formato, idioma, precio}` → programa una función (409 si se traslapa) |
| POST | `/api/captcha/verificar` | `{token, accion}` → verificación de reCAPTCHA v3 (403 si no supera el puntaje) y `pase` de un solo uso |

```bash
curl localhost:8080/api/funciones
curl -X POST localhost:8080/api/reservas -d '{"funcion":"f5","usuario_id":1,"asientos":["C-04","C-05"]}'
curl -X POST localhost:8080/api/reservas/<reserva_id>/pagar
curl localhost:8080/api/funciones/f5/asientos
curl localhost:8080/api/metricas
curl -N localhost:8080/api/funciones/f5/stream     # deja la terminal abierta y bloquea butacas desde el navegador
```

### Cómo se evita la doble venta

1. Al tocar una butaca libre, el frontend pide `POST …/bloqueo`. Un script Lua en Redis comprueba y escribe en un solo paso:
   si la clave `asiento:{función}:{butaca}` no existe (o ya es del mismo cliente) la crea con TTL de 300 s; si es de otro, responde 409.
2. Cada cambio se publica en el canal `asientos:{función}`; todas las instancias de la API lo reenvían por SSE a los navegadores
   que tienen abierto ese mapa. Cuando un TTL vence, Redis lo notifica (`notify-keyspace-events Ex`) y se difunde como `liberado`.
3. Al reservar, las butacas bloqueadas por el cliente pasan a la reserva de forma atómica (todas o ninguna).
4. Al pagar, la transacción en PostgreSQL y el índice único parcial de `boletos` son la última barrera.

Pendiente para la siguiente iteración: autenticación con JWT y bcrypt (hoy el `usuario_id` viaja en el cuerpo de la
solicitud y los permisos de rol, como crear películas, se aplican sólo en el frontend), usuarios y bitácora en la API,
edición y baja de películas, envío de correo SMTP y pruebas de carga (k6) para RNF-01 y RNF-43.
