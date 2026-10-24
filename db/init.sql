-- Esquema de Cineight (PostgreSQL 18). Nomenclatura snake_case en español (RNF-14).
-- Los campos `codigo` / `slug` coinciden con los identificadores del frontend (f1…f21, s1…s3, the-batman…)
-- para que la interfaz y la API hablen de las mismas funciones, salas y películas.

CREATE TABLE roles (
  id      SMALLINT PRIMARY KEY,
  nombre  TEXT NOT NULL UNIQUE
);
INSERT INTO roles VALUES (1, 'cliente'), (2, 'agente'), (3, 'admin'), (4, 'auditor');

CREATE TABLE usuarios (
  id               BIGSERIAL PRIMARY KEY,
  nombre           TEXT NOT NULL,
  correo           TEXT NOT NULL UNIQUE,
  hash_contrasena  TEXT NOT NULL,           -- bcrypt (RNF-09)
  rol_id           SMALLINT NOT NULL REFERENCES roles(id),
  activo           BOOLEAN NOT NULL DEFAULT TRUE,
  intentos_fallidos SMALLINT NOT NULL DEFAULT 0,
  bloqueado_hasta  TIMESTAMPTZ,
  creado           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE cuentas_credito (
  usuario_id  BIGINT PRIMARY KEY REFERENCES usuarios(id),
  saldo       INTEGER NOT NULL DEFAULT 0 CHECK (saldo >= 0)
);

CREATE TABLE movimientos_credito (
  id          BIGSERIAL PRIMARY KEY,
  usuario_id  BIGINT NOT NULL REFERENCES usuarios(id),
  tipo        TEXT NOT NULL CHECK (tipo IN ('recarga','retencion','debito','liberacion','reembolso')),
  monto       INTEGER NOT NULL,
  referencia  TEXT NOT NULL,
  fecha       TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tipo, referencia)                 -- idempotencia: una partida de trivia o un pago se acredita/debita una sola vez
);

CREATE TABLE salas (
  id        SERIAL PRIMARY KEY,
  codigo    TEXT NOT NULL UNIQUE,
  nombre    TEXT NOT NULL,
  tipo      TEXT NOT NULL,
  filas     SMALLINT NOT NULL,
  columnas  SMALLINT NOT NULL,
  aforo     SMALLINT NOT NULL,
  activa    BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE peliculas (
  id             SERIAL PRIMARY KEY,
  slug           TEXT NOT NULL UNIQUE,
  titulo         TEXT NOT NULL,
  clasificacion  TEXT NOT NULL,
  duracion_min   SMALLINT NOT NULL,
  estado         TEXT NOT NULL DEFAULT 'cartelera' CHECK (estado IN ('cartelera','estreno','proximamente')),
  -- Ficha completa: la llenan las películas creadas desde el panel (las de demostración viven en el frontend).
  titulo_original TEXT,
  genero          TEXT NOT NULL DEFAULT '',
  idioma          TEXT NOT NULL DEFAULT 'Subtitulada',
  formatos        TEXT[] NOT NULL DEFAULT '{}',
  director        TEXT NOT NULL DEFAULT '',
  reparto         TEXT[] NOT NULL DEFAULT '{}',
  anio            TEXT NOT NULL DEFAULT '',
  sinopsis        TEXT NOT NULL DEFAULT '',
  trailer         TEXT,                       -- id de YouTube
  estreno_en_dias SMALLINT,                   -- sólo "proximamente"
  poster          BYTEA,                      -- imagen (JPEG/PNG/WebP, máx. 2 MB)
  poster_tipo     TEXT,
  personalizada   BOOLEAN NOT NULL DEFAULT FALSE,  -- creada por un agente o administrador
  creada_por      TEXT,
  creada          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE funciones (
  id           SERIAL PRIMARY KEY,
  codigo       TEXT NOT NULL UNIQUE,
  pelicula_id  INT NOT NULL REFERENCES peliculas(id),
  sala_id      INT NOT NULL REFERENCES salas(id),
  inicio       TIMESTAMPTZ NOT NULL,
  fin          TIMESTAMPTZ NOT NULL,        -- inicio + duración + 20 min de limpieza (RF-31)
  formato      TEXT NOT NULL DEFAULT '2D',
  idioma       TEXT NOT NULL DEFAULT 'Subtitulada',
  precio       INTEGER NOT NULL,            -- créditos
  evento       TEXT,                        -- preestreno con cola virtual (RF-36)
  estado       TEXT NOT NULL DEFAULT 'programada'
);
CREATE INDEX ix_funciones_sala_inicio ON funciones (sala_id, inicio);

-- RF-54 / RNF-36: la restricción UNIQUE hace imposible vender dos veces la misma butaca de una función.
CREATE TABLE boletos (
  id           BIGSERIAL PRIMARY KEY,
  token        TEXT NOT NULL UNIQUE,
  funcion_id   INT NOT NULL REFERENCES funciones(id),
  asiento      TEXT NOT NULL,
  usuario_id   BIGINT NOT NULL REFERENCES usuarios(id),
  precio       INTEGER NOT NULL,
  estado       TEXT NOT NULL DEFAULT 'valido' CHECK (estado IN ('valido','utilizado','anulado','reembolsado')),
  reserva_id   TEXT NOT NULL,
  fecha_venta  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ux_boletos_asiento_activo ON boletos (funcion_id, asiento) WHERE estado IN ('valido','utilizado');
CREATE INDEX ix_boletos_usuario ON boletos (usuario_id);

CREATE TABLE bitacora_auditoria (
  id           BIGSERIAL PRIMARY KEY,
  usuario_id   BIGINT,
  accion       TEXT NOT NULL,
  entidad      TEXT,
  valor_anterior JSONB,
  valor_nuevo  JSONB,
  fecha        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Butacas que el personal retira de la venta (dañadas o en mantenimiento) en todas las funciones de su sala.
-- La API también la crea al arrancar si falta (bases creadas antes de este cambio).
CREATE TABLE butacas_fuera_servicio (
  sala_id     INT NOT NULL REFERENCES salas(id),
  asiento     TEXT NOT NULL,
  motivo      TEXT NOT NULL CHECK (motivo IN ('danada','fuera-servicio')),
  marcada_por TEXT NOT NULL DEFAULT '',
  desde       TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (sala_id, asiento)
);

-- ───────── Datos iniciales ─────────
-- Cuentas del personal. Los clientes se crean solos al registrarse (POST /api/sesion, con bono de bienvenida).
-- El hash es un marcador: el inicio de sesión todavía se valida en el frontend (pendiente: JWT + bcrypt).
INSERT INTO usuarios (nombre, correo, hash_contrasena, rol_id) VALUES
  ('Carlos Agente',   'agente@cineight.gt',    '$2a$10$Q9r1m0h0pZ3x1x7nqv8b0eW8m0n2Zq4S2Wc6l2y2a3o7xJ5h1m6yG', 2),
  ('Admin Sistema',   'admin@cineight.gt',     '$2a$10$Q9r1m0h0pZ3x1x7nqv8b0eW8m0n2Zq4S2Wc6l2y2a3o7xJ5h1m6yG', 3),
  ('Laura Auditora',  'auditor@cineight.gt',   '$2a$10$Q9r1m0h0pZ3x1x7nqv8b0eW8m0n2Zq4S2Wc6l2y2a3o7xJ5h1m6yG', 4);
-- Usuario de sistema dueño de las ventas en taquilla del preestreno (no puede iniciar sesión).
INSERT INTO usuarios (nombre, correo, hash_contrasena, rol_id, activo) VALUES
  ('Venta en taquilla', 'taquilla@cineight.gt', 'sin-acceso', 2, FALSE);

INSERT INTO salas (codigo, nombre, tipo, filas, columnas, aforo) VALUES
  ('s1', 'Sala 1', 'IMAX Láser',  12, 16, 192),
  ('s2', 'Sala 2', 'Dolby Atmos', 10, 14, 140),
  ('s3', 'Sala 3', 'Tradicional', 10, 12, 120),
  ('s4', 'Sala 4', '4DX 3D',       8, 12,  96),
  ('s5', 'Sala 5', 'Dolby Atmos', 10, 14, 140),
  ('s6', 'Sala 6', 'Tradicional', 10, 12, 120),
  ('s7', 'Sala 7', 'VIP',          6, 10,  60),
  ('s8', 'Sala 8', 'Tradicional', 10, 12, 120),
  ('s9',  'Sala 9',  'IMAX Láser',  12, 16, 192),
  ('s10', 'Sala 10', 'Dolby Atmos', 10, 14, 140),
  ('s11', 'Sala 11', 'Tradicional', 10, 12, 120),
  ('s12', 'Sala 12', 'Dolby Atmos', 10, 14, 140),
  ('s13', 'Sala 13', 'Tradicional', 10, 12, 120),
  ('s14', 'Sala 14', 'Tradicional', 10, 12, 120),
  ('s15', 'Sala 15', 'VIP',          6, 10,  60),
  ('s16', 'Sala 16', '4DX 3D',       8, 12,  96);

INSERT INTO peliculas (slug, titulo, clasificacion, duracion_min, estado) VALUES
  ('the-batman',         'The Batman',                          'PG-13', 176, 'cartelera'),
  ('avatar-agua',        'Avatar: El Camino del Agua',          'PG-13', 192, 'cartelera'),
  ('gladiador',          'Gladiador',                           'R',     155, 'cartelera'),
  ('dark-knight',        'El Caballero de la Noche',            'PG-13', 152, 'cartelera'),
  ('deadpool-wolverine', 'Deadpool & Wolverine',                'R',     128, 'estreno'),
  ('dune-2',             'Duna: Parte Dos',                     'PG-13', 166, 'estreno'),
  ('intensa-mente-2',    'Intensa-Mente 2',                     'PG',     96, 'estreno'),
  ('oppenheimer',        'Oppenheimer',                         'R',     180, 'estreno'),
  ('spider-verse',       'Spider-Man: Beyond the Spider-Verse', 'PG',    145, 'proximamente'),
  ('avatar-fuego',       'Avatar: Fuego y Ceniza',              'PG-13', 190, 'proximamente'),
  ('gladiator-2',        'Gladiator II',                        'R',     148, 'proximamente'),
  ('batman-2',           'The Batman: Parte II',                'PG-13', 170, 'proximamente'),
  ('top-gun-maverick',     'Top Gun: Maverick',                     'PG-13', 130, 'cartelera'),
  ('whiplash',             'Whiplash: Música y Obsesión',           'R',     106, 'cartelera'),
  ('todo-en-todas-partes', 'Todo en todas partes al mismo tiempo',  'R',     139, 'cartelera'),
  ('matrix',               'Matrix',                                'R',     136, 'cartelera'),
  ('alien-romulus',        'Alien: Romulus',                        'R',     119, 'estreno'),
  ('furiosa',              'Furiosa: De la saga Mad Max',           'R',     148, 'estreno'),
  ('civil-war',            'Civil War',                             'R',     109, 'estreno'),
  ('planeta-simios',       'El Planeta de los Simios: Nuevo Reino', 'PG-13', 145, 'estreno'),
  ('mickey-17',            'Mickey 17',                             'R',     135, 'proximamente'),
  ('superman',             'Superman',                              'PG-13', 140, 'proximamente'),
  ('tron-ares',            'TRON: Ares',                            'PG-13', 125, 'proximamente'),
  ('avengers-secret-wars', 'Avengers: Secret Wars',                 'PG-13', 180, 'proximamente');

-- Programación de hoy y los 3 días siguientes, en la hora de Guatemala. Es el mismo plan que la API usa para
-- completar los días siguientes (backend/internal/domain/programacion.go):
--   * 8 pares de películas (una larga con una corta); cada par ocupa dos salas del mismo formato con el orden
--     invertido, así cada película tiene función a las 10:00 todos los días;
--   * todas las salas abren a las 10:00; la siguiente función inicia al terminar la anterior + 20 min de limpieza,
--     redondeado al cuarto de hora, sin inicios después de las 22:00 (5 funciones por sala y por película al día);
--   * los días impares cada sala empieza por la segunda película; antes de las 13:00 hay 10 créditos de descuento.
INSERT INTO funciones (codigo, pelicula_id, sala_id, inicio, fin, formato, idioma, precio, evento)
WITH RECURSIVE
plan(orden, sala, a, b, formato, precio, desde) AS (VALUES
  ( 1, 's1',  'dune-2',               'top-gun-maverick',     'IMAX Láser',  70, 600),
  ( 2, 's9',  'top-gun-maverick',     'dune-2',               'IMAX Láser',  70, 600),
  ( 3, 's2',  'the-batman',           'whiplash',             'Dolby Atmos', 60, 600),
  ( 4, 's12', 'whiplash',             'the-batman',           'Dolby Atmos', 60, 600),
  ( 5, 's5',  'furiosa',              'deadpool-wolverine',   'Dolby Atmos', 60, 600),
  ( 6, 's10', 'deadpool-wolverine',   'furiosa',              'Dolby Atmos', 60, 600),
  ( 7, 's4',  'avatar-agua',          'intensa-mente-2',      '4DX 3D',      75, 600),
  ( 8, 's16', 'intensa-mente-2',      'avatar-agua',          '4DX 3D',      75, 600),
  ( 9, 's7',  'oppenheimer',          'civil-war',            '2D VIP',      80, 600),
  (10, 's15', 'civil-war',            'oppenheimer',          '2D VIP',      80, 600),
  (11, 's3',  'gladiador',            'alien-romulus',        '2D',          50, 600),
  (12, 's13', 'alien-romulus',        'gladiador',            '2D',          50, 600),
  (13, 's6',  'dark-knight',          'matrix',               '2D',          50, 600),
  (14, 's8',  'matrix',               'dark-knight',          '2D',          50, 600),
  (15, 's11', 'planeta-simios',       'todo-en-todas-partes', '2D',          50, 600),
  (16, 's14', 'todo-en-todas-partes', 'planeta-simios',       '2D',          50, 600)
),
prog(orden, sala, dia, n, minuto, peli, a, b, formato, precio) AS (
  SELECT orden, sala, dia, 0, desde, CASE WHEN dia % 2 = 0 THEN a ELSE b END, a, b, formato, precio
  FROM plan CROSS JOIN generate_series(0, 3) AS dia
  UNION ALL
  SELECT pr.orden, pr.sala, pr.dia, pr.n + 1, (ceil((pr.minuto + pe.duracion_min + 20) / 15.0) * 15)::int,
         CASE WHEN (pr.dia + pr.n + 1) % 2 = 0 THEN pr.a ELSE pr.b END, pr.a, pr.b, pr.formato, pr.precio
  FROM prog pr JOIN peliculas pe ON pe.slug = pr.peli
  WHERE ceil((pr.minuto + pe.duracion_min + 20) / 15.0) * 15 <= 1320
),
numeradas AS (
  SELECT prog.*, 'f' || row_number() OVER (ORDER BY dia, orden, n) AS codigo,
         -- Preestreno con cola virtual: la última función de Duna: Parte Dos del día 1 en la Sala 1.
         (peli = 'dune-2' AND sala = 's1' AND dia = 1
          AND minuto = max(minuto) FILTER (WHERE peli = 'dune-2' AND sala = 's1') OVER (PARTITION BY dia)) AS es_evento
  FROM prog
)
SELECT x.codigo, p.id, s.id, i.inicio, i.inicio + make_interval(mins => p.duracion_min + 20), x.formato,
       CASE WHEN x.peli IN ('intensa-mente-2', 'avatar-agua') THEN 'Español' ELSE 'Subtitulada' END,
       CASE WHEN x.es_evento THEN 80 ELSE x.precio - CASE WHEN x.minuto < 780 THEN 10 ELSE 0 END END,
       CASE WHEN x.es_evento THEN 'e1' END
FROM numeradas x
JOIN peliculas p ON p.slug = x.peli
JOIN salas s ON s.codigo = x.sala
CROSS JOIN LATERAL (
  SELECT ((date_trunc('day', now() AT TIME ZONE 'America/Guatemala') + make_interval(days => x.dia, mins => x.minuto))
          AT TIME ZONE 'America/Guatemala') AS inicio
) i;
