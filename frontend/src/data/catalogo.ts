// Catálogo de la cartelera: películas, salas, eventos de preestreno y trivia.
// La programación de funciones y la ocupación de las butacas vienen de la API (PostgreSQL y Redis).

import { PARAMETROS } from "./parametros";
import type { Categoria, EstrenoConCola, Funcion, Pelicula, Pregunta, Rol, Sala } from "@/types/catalogo";

// Cuentas del personal que existen desde la primera visita (los clientes se registran solos).
// El administrador crea, edita o da de baja las demás desde Panel → Usuarios & roles.
export const ROL_TXT: Record<Rol, string> = { cliente: "Cliente", agente: "Agente Operativo", admin: "Administrador del Sistema", auditor: "Auditor" };

export const SALAS: Sala[] = [
  // pasillos: columnas después de las cuales hay un pasillo
  { id: "s1", nombre: "Sala 1", tipo: "IMAX Láser", filas: 12, columnas: 16, aforo: 192, pasillos: [4, 12] },
  { id: "s2", nombre: "Sala 2", tipo: "Dolby Atmos", filas: 10, columnas: 14, aforo: 140, pasillos: [3, 11] },
  { id: "s3", nombre: "Sala 3", tipo: "Tradicional", filas: 10, columnas: 12, aforo: 120, pasillos: [3, 9] },
  { id: "s4", nombre: "Sala 4", tipo: "4DX 3D", filas: 8, columnas: 12, aforo: 96, pasillos: [3, 9] },
  { id: "s5", nombre: "Sala 5", tipo: "Dolby Atmos", filas: 10, columnas: 14, aforo: 140, pasillos: [3, 11] },
  { id: "s6", nombre: "Sala 6", tipo: "Tradicional", filas: 10, columnas: 12, aforo: 120, pasillos: [3, 9] },
  { id: "s7", nombre: "Sala 7", tipo: "VIP", filas: 6, columnas: 10, aforo: 60, pasillos: [5] },
  { id: "s8", nombre: "Sala 8", tipo: "Tradicional", filas: 10, columnas: 12, aforo: 120, pasillos: [3, 9] },
  { id: "s9", nombre: "Sala 9", tipo: "IMAX Láser", filas: 12, columnas: 16, aforo: 192, pasillos: [4, 12] },
  { id: "s10", nombre: "Sala 10", tipo: "Dolby Atmos", filas: 10, columnas: 14, aforo: 140, pasillos: [3, 11] },
  { id: "s11", nombre: "Sala 11", tipo: "Tradicional", filas: 10, columnas: 12, aforo: 120, pasillos: [3, 9] },
  { id: "s12", nombre: "Sala 12", tipo: "Dolby Atmos", filas: 10, columnas: 14, aforo: 140, pasillos: [3, 11] },
  { id: "s13", nombre: "Sala 13", tipo: "Tradicional", filas: 10, columnas: 12, aforo: 120, pasillos: [3, 9] },
  { id: "s14", nombre: "Sala 14", tipo: "Tradicional", filas: 10, columnas: 12, aforo: 120, pasillos: [3, 9] },
  { id: "s15", nombre: "Sala 15", tipo: "VIP", filas: 6, columnas: 10, aforo: 60, pasillos: [5] },
  { id: "s16", nombre: "Sala 16", tipo: "4DX 3D", filas: 8, columnas: 12, aforo: 96, pasillos: [3, 9] },
];

export const PELICULAS: Pelicula[] = [
  // ───────── EN CARTELERA ─────────
  {
    id: "the-batman",
    titulo: "The Batman",
    tituloOriginal: "The Batman",
    categoria: "cartelera",
    clasificacion: "PG-13",
    duracion: 176,
    genero: "Acción / Crimen / Drama",
    idioma: "Subtitulada",
    formatos: ["IMAX Láser", "Dolby Atmos", "2D"],
    director: "Matt Reeves",
    reparto: ["Robert Pattinson", "Zoë Kravitz", "Paul Dano", "Colin Farrell"],
    anio: "2022",
    sinopsis:
      "En su segundo año luchando contra el crimen, Batman explora la corrupción existente en Gotham City y el vínculo que esta tiene con su propia familia, mientras persigue al Acertijo, un asesino en serie que ataca a la élite de la ciudad.",
    trailer: "mqqft2x_Aa4",
    poster: "/posters/the-batman.jpg",
    acento: "#4a0b0b",
  },
  {
    id: "avatar-agua",
    titulo: "Avatar: El Camino del Agua",
    tituloOriginal: "Avatar: The Way of Water",
    categoria: "cartelera",
    clasificacion: "PG-13",
    duracion: 192,
    genero: "Ciencia Ficción / Aventura / Acción",
    idioma: "Español",
    formatos: ["IMAX Láser", "3D"],
    director: "James Cameron",
    reparto: ["Sam Worthington", "Zoe Saldaña", "Sigourney Weaver", "Stephen Lang"],
    anio: "2022",
    sinopsis:
      "Jake Sully vive con su nueva familia formada en el planeta Pandora. Una antigua amenaza regresa para terminar lo que comenzó, obligando a Jake a unirse a Neytiri y al ejército Na'vi para proteger su hogar en los océanos.",
    trailer: "d9MyW72ELq0",
    poster: "/posters/avatar-agua.jpg",
    acento: "#0b2e4a",
  },
  {
    id: "gladiador",
    titulo: "Gladiador",
    tituloOriginal: "Gladiator",
    categoria: "cartelera",
    clasificacion: "R",
    duracion: 155,
    genero: "Acción / Aventura / Drama Histórico",
    idioma: "Subtitulada",
    formatos: ["2D"],
    director: "Ridley Scott",
    reparto: ["Russell Crowe", "Joaquin Phoenix", "Connie Nielsen", "Oliver Reed"],
    anio: "2000",
    sinopsis:
      "Un leal general romano es traicionado cuando el ambicioso hijo del emperador asesina a su padre y usurpa el trono. Reducido a la esclavitud, el general se convierte en gladiador para ascender en la arena y vengar el asesinato de su familia.",
    trailer: "P5ieIbInFpg",
    poster: "/posters/gladiator.png",
    acento: "#4a330b",
  },
  {
    id: "dark-knight",
    titulo: "El Caballero de la Noche",
    tituloOriginal: "The Dark Knight",
    categoria: "cartelera",
    clasificacion: "PG-13",
    duracion: 152,
    genero: "Acción / Crimen / Drama",
    idioma: "Subtitulada",
    formatos: ["Dolby Atmos", "2D"],
    director: "Christopher Nolan",
    reparto: ["Christian Bale", "Heath Ledger", "Aaron Eckhart", "Maggie Gyllenhaal"],
    anio: "2008",
    sinopsis:
      "Con la ayuda del teniente Jim Gordon y el fiscal Harvey Dent, Batman combate al crimen organizado en Gotham. Sin embargo, la llegada del Guasón, un criminal anarquista, sume a la ciudad en el caos y pone a prueba los límites éticos del héroe.",
    trailer: "EXeTwQWrcwY",
    poster: "/posters/dark-knight.jpg",
    acento: "#0b1a2e",
  },
  {
    id: "top-gun-maverick",
    titulo: "Top Gun: Maverick",
    tituloOriginal: "Top Gun: Maverick",
    categoria: "cartelera",
    clasificacion: "PG-13",
    duracion: 130,
    genero: "Acción / Drama",
    idioma: "Subtitulada",
    formatos: ["IMAX Láser", "Dolby Atmos"],
    director: "Joseph Kosinski",
    reparto: ["Tom Cruise", "Miles Teller", "Jennifer Connelly", "Jon Hamm"],
    anio: "2022",
    sinopsis:
      "Tras más de 30 años de servicio como uno de los mejores aviadores de la Armada, Pete \"Maverick\" Mitchell entrena a un destacamento de graduados de Top Gun para una misión suicida especializada que requiere sacrificios extremos.",
    trailer: "giXco2jaZ_4",
    poster: "/posters/top-gun-maverick.jpg",
    acento: "#3a2a0b",
  },
  {
    id: "whiplash",
    titulo: "Whiplash: Música y Obsesión",
    tituloOriginal: "Whiplash",
    categoria: "cartelera",
    clasificacion: "R",
    duracion: 106,
    genero: "Drama / Música",
    idioma: "Subtitulada",
    formatos: ["2D"],
    director: "Damien Chazelle",
    reparto: ["Miles Teller", "J.K. Simmons", "Paul Reiser"],
    anio: "2014",
    sinopsis:
      "Andrew Neiman es un joven y ambicioso baterista de jazz decidido a triunfar en un elitista conservatorio. Terence Fletcher, un instructor conocido por sus despiadados métodos de enseñanza, lo empuja al límite absoluto de su salud física y mental.",
    trailer: "7d_jQycdQGo",
    poster: "/posters/whiplash.jpg",
    acento: "#4a2e0b",
  },
  {
    id: "todo-en-todas-partes",
    titulo: "Todo en todas partes al mismo tiempo",
    tituloOriginal: "Everything Everywhere All at Once",
    categoria: "cartelera",
    clasificacion: "R",
    duracion: 139,
    genero: "Ciencia Ficción / Comedia / Acción",
    idioma: "Subtitulada",
    formatos: ["2D"],
    director: "Daniel Kwan, Daniel Scheinert",
    reparto: ["Michelle Yeoh", "Ke Huy Quan", "Stephanie Hsu", "Jamie Lee Curtis"],
    anio: "2022",
    sinopsis:
      "Una inmigrante china de mediana edad envuelta en una auditoría fiscal descubre que debe conectar con sus versiones de universos paralelos para detener una amenaza multiversal que destruirá toda la existencia.",
    trailer: "wxN1T1uxQ2g",
    poster: "/posters/todo-en-todas-partes.jpg",
    acento: "#2e0b3a",
  },
  {
    id: "matrix",
    titulo: "Matrix",
    tituloOriginal: "The Matrix",
    categoria: "cartelera",
    clasificacion: "R",
    duracion: 136,
    genero: "Ciencia Ficción / Acción",
    idioma: "Subtitulada",
    formatos: ["2D"],
    director: "Lana Wachowski, Lilly Wachowski",
    reparto: ["Keanu Reeves", "Laurence Fishburne", "Carrie-Anne Moss", "Hugo Weaving"],
    anio: "1999",
    sinopsis:
      "Thomas Anderson, un programador de día y hacker de noche bajo el alias Neo, descubre que la realidad en la que vive es una simulación virtual creada por inteligencias artificiales para utilizar a los humanos como fuente de energía.",
    trailer: "vKQi3bBA1y8",
    poster: "/posters/matrix.png",
    acento: "#0b3a1a",
  },
  // ───────── ESTRENOS (preestrenos activos) ─────────
  {
    id: "deadpool-wolverine",
    titulo: "Deadpool & Wolverine",
    tituloOriginal: "Deadpool & Wolverine",
    categoria: "estreno",
    clasificacion: "R",
    duracion: 128,
    genero: "Acción / Comedia / Ciencia Ficción",
    idioma: "Subtitulada",
    formatos: ["Dolby Atmos", "2D"],
    director: "Shawn Levy",
    reparto: ["Ryan Reynolds", "Hugh Jackman", "Emma Corrin", "Morena Baccarin"],
    anio: "2024",
    sinopsis:
      "La Autoridad de Variación Temporal (TVA) recluta a un apático Deadpool para una misión urgente que alterará la historia del multiverso, obligándolo a hacer equipo con un reticente y desgastado Wolverine.",
    trailer: "73_1biulkYk",
    poster: "/posters/deadpool-wolverine.jpg",
    acento: "#5a0b0b",
  },
  {
    id: "dune-2",
    titulo: "Duna: Parte Dos",
    tituloOriginal: "Dune: Part Two",
    categoria: "estreno",
    clasificacion: "PG-13",
    duracion: 166,
    genero: "Ciencia Ficción / Aventura",
    idioma: "Subtitulada",
    formatos: ["IMAX Láser", "Dolby Atmos"],
    director: "Denis Villeneuve",
    reparto: ["Timothée Chalamet", "Zendaya", "Rebecca Ferguson", "Javier Bardem", "Austin Butler"],
    anio: "2024",
    sinopsis:
      "Paul Atreides se une a Chani y a los Fremen mientras busca venganza contra los conspiradores que destruyeron a su familia. Ante una elección entre el amor de su vida y el destino del universo, debe prevenir un futuro catastrófico.",
    trailer: "Way9Dexny3w",
    poster: "/posters/dune-2.jpg",
    acento: "#5a3a0b",
  },
  {
    id: "intensa-mente-2",
    titulo: "Intensa-Mente 2",
    tituloOriginal: "Inside Out 2",
    categoria: "estreno",
    clasificacion: "PG",
    duracion: 96,
    genero: "Animación / Aventura / Comedia",
    idioma: "Español",
    formatos: ["2D", "3D"],
    director: "Kelsey Mann",
    reparto: ["Amy Poehler", "Maya Hawke", "Kensington Tallman", "Liza Lapira"],
    repartoVoces: true,
    anio: "2024",
    sinopsis:
      "La mente de Riley, ahora una adolescente, sufre una demolición repentina para hacer espacio a nuevas emociones. Alegría, Tristeza, Furia, Temor y Desagrado no saben cómo reaccionar cuando Ansiedad y Envidia irrumpen en el cuartel general.",
    trailer: "LEjhY15eCx0",
    poster: "/posters/inside-out-2.jpg",
    acento: "#3a0b5a",
  },
  {
    id: "oppenheimer",
    titulo: "Oppenheimer",
    tituloOriginal: "Oppenheimer",
    categoria: "estreno",
    clasificacion: "R",
    duracion: 180,
    genero: "Biografía / Drama / Historia",
    idioma: "Subtitulada",
    formatos: ["IMAX Láser", "2D"],
    director: "Christopher Nolan",
    reparto: ["Cillian Murphy", "Emily Blunt", "Matt Damon", "Robert Downey Jr."],
    anio: "2023",
    sinopsis:
      "Historia del físico teórico J. Robert Oppenheimer, su papel al frente del Proyecto Manhattan durante la Segunda Guerra Mundial y las consecuencias políticas y éticas del desarrollo de la bomba atómica.",
    trailer: "uYPbbksJxIg",
    poster: "/posters/oppenheimer.jpg",
    acento: "#5a2a0b",
  },
  {
    id: "alien-romulus",
    titulo: "Alien: Romulus",
    tituloOriginal: "Alien: Romulus",
    categoria: "estreno",
    clasificacion: "R",
    duracion: 119,
    genero: "Terror / Ciencia Ficción",
    idioma: "Subtitulada",
    formatos: ["Dolby Atmos", "2D"],
    director: "Fede Álvarez",
    reparto: ["Cailee Spaeny", "David Jonsson", "Archie Renaux", "Isabela Merced"],
    anio: "2024",
    sinopsis:
      "Mientras hurgaban en las profundidades de una estación espacial abandonada con la esperanza de conseguir una vida mejor, un grupo de jóvenes colonos espaciales se encuentra cara a cara con la forma de vida más aterradora del universo.",
    trailer: "x0XDEhP4MQs",
    poster: "/posters/alien-romulus.jpg",
    acento: "#0b2a1f",
  },
  {
    id: "furiosa",
    titulo: "Furiosa: De la saga Mad Max",
    tituloOriginal: "Furiosa: A Mad Max Saga",
    categoria: "estreno",
    clasificacion: "R",
    duracion: 148,
    genero: "Acción / Ciencia Ficción",
    idioma: "Subtitulada",
    formatos: ["Dolby Atmos", "2D"],
    director: "George Miller",
    reparto: ["Anya Taylor-Joy", "Chris Hemsworth", "Tom Burke", "Alyla Browne"],
    anio: "2024",
    sinopsis:
      "Al caer el mundo, la joven Furiosa es arrebatada del Lugar Verde de Muchas Madres y cae en manos de una horda de motoristas liderada por el Señor de la Guerra Dementus. Para regresar a casa, deberá sobrevivir a incontables pruebas en el páramo.",
    trailer: "XJMuhwVlca4",
    poster: "/posters/furiosa.jpg",
    acento: "#4a2a0b",
  },
  {
    id: "civil-war",
    titulo: "Civil War",
    tituloOriginal: "Civil War",
    categoria: "estreno",
    clasificacion: "R",
    duracion: 109,
    genero: "Acción / Suspenso / Drama",
    idioma: "Subtitulada",
    formatos: ["2D VIP", "2D"],
    director: "Alex Garland",
    reparto: ["Kirsten Dunst", "Wagner Moura", "Cailee Spaeny", "Jesse Plemons"],
    anio: "2024",
    sinopsis:
      "En un futuro cercano, un equipo de periodistas de guerra recorre unos Estados Unidos colapsados y divididos por un conflicto armado interno para llegar a Washington D.C. antes de que las fuerzas rebeldes tomen la Casa Blanca.",
    trailer: "aDyQxtg0V2w",
    poster: "/posters/civil-war.jpg",
    acento: "#2a1a0b",
  },
  {
    id: "planeta-simios",
    titulo: "El Planeta de los Simios: Nuevo Reino",
    tituloOriginal: "Kingdom of the Planet of the Apes",
    categoria: "estreno",
    clasificacion: "PG-13",
    duracion: 145,
    genero: "Ciencia Ficción / Acción / Aventura",
    idioma: "Subtitulada",
    formatos: ["4DX 3D", "2D"],
    director: "Wes Ball",
    reparto: ["Owen Teague", "Freya Allan", "Kevin Durand", "Peter Macon"],
    anio: "2024",
    sinopsis:
      "Varios siglos después del reinado de César, los simios son la especie dominante y los humanos viven reducidos a un estado salvaje. Un joven simio emprende un viaje que lo llevará a cuestionar todo lo que sabe sobre el pasado de ambas especies.",
    trailer: "XtFI7SNtVpY",
    poster: "/posters/planeta-simios.jpg",
    acento: "#3a2e0b",
  },
  // ───────── PRÓXIMAMENTE (apertura de taquilla programada) ─────────
  {
    id: "spider-verse",
    titulo: "Spider-Man: Beyond the Spider-Verse",
    tituloOriginal: "Spider-Man: Beyond the Spider-Verse",
    categoria: "proximamente",
    clasificacion: "PG",
    duracion: 145,
    duracionEstimada: true,
    genero: "Animación / Acción / Aventura",
    idioma: "Español",
    formatos: ["IMAX Láser", "3D"],
    director: "Joaquim Dos Santos, Kemp Powers, Justin K. Thompson",
    reparto: ["Shameik Moore", "Hailee Steinfeld", "Oscar Isaac", "Daniel Kaluuya"],
    repartoVoces: true,
    anio: "Próximo lanzamiento",
    sinopsis:
      "Tras los desgarradores acontecimientos del multiverso, Miles Morales debe escapar de su contraparte alternativa en la Tierra-42 mientras Gwen Stacy reúne a un equipo de Spider-Personas para rescatarlo y salvar la red del destino.",
    poster: "/posters/spider-verse.jpg",
    posterLogo: true,
    estrenoEnDias: 45,
    acento: "#3a0b2e",
  },
  {
    id: "avatar-fuego",
    titulo: "Avatar: Fuego y Ceniza",
    tituloOriginal: "Avatar: Fire and Ash",
    categoria: "proximamente",
    clasificacion: "PG-13",
    duracion: 190,
    duracionEstimada: true,
    genero: "Ciencia Ficción / Aventura",
    idioma: "Español",
    formatos: ["IMAX Láser", "3D"],
    director: "James Cameron",
    reparto: ["Sam Worthington", "Zoe Saldaña", "Sigourney Weaver", "Oona Chaplin"],
    anio: "Próximo lanzamiento",
    sinopsis:
      'La familia Sully explora nuevas regiones de Pandora y entra en contacto con el "Pueblo de la Ceniza", un clan Na\'vi con una cultura agresiva y oscura asociada directamente con el elemento fuego.',
    trailer: "nb_fFj_0rq8",
    poster: "/posters/avatar-fuego.jpg",
    estrenoEnDias: 20,
    acento: "#5a1a0b",
  },
  {
    id: "gladiator-2",
    titulo: "Gladiator II",
    tituloOriginal: "Gladiator II",
    categoria: "proximamente",
    clasificacion: "R",
    duracion: 148,
    genero: "Acción / Aventura / Drama Histórico",
    idioma: "Subtitulada",
    formatos: ["IMAX Láser", "Dolby Atmos"],
    director: "Ridley Scott",
    reparto: ["Paul Mescal", "Pedro Pascal", "Denzel Washington", "Connie Nielsen"],
    anio: "Próximo lanzamiento",
    sinopsis:
      "Años después de presenciar el sacrificio de Máximo, Lucius, el nieto del antiguo emperador Marco Aurelio, se ve obligado a ingresar al Coliseo tras la conquista de su hogar a manos de los tiránicos emperadores de Roma.",
    trailer: "4rgYUipGJNo",
    poster: "/posters/gladiator-2.jpg",
    estrenoEnDias: 12,
    acento: "#4a2a0b",
  },
  {
    id: "batman-2",
    titulo: "The Batman: Parte II",
    tituloOriginal: "The Batman: Part II",
    categoria: "proximamente",
    clasificacion: "PG-13",
    duracion: 170,
    duracionEstimada: true,
    genero: "Acción / Crimen / Drama",
    idioma: "Subtitulada",
    formatos: ["IMAX Láser", "Dolby Atmos"],
    director: "Matt Reeves",
    reparto: ["Robert Pattinson", "Zoë Kravitz", "Colin Farrell", "Andy Serkis"],
    anio: "Próximo lanzamiento",
    sinopsis:
      "Tras la gran inundación de Gotham, Batman se enfrenta al colapso del control territorial entre las mafias de la ciudad mientras un nuevo antagonista emerge del caos para desmantelar la estructura social.",
    poster: "/posters/batman-2.png",
    posterLogo: true,
    estrenoEnDias: 60,
    acento: "#2e0b0b",
  },
  {
    id: "mickey-17",
    titulo: "Mickey 17",
    tituloOriginal: "Mickey 17",
    categoria: "proximamente",
    clasificacion: "R",
    duracion: 135,
    genero: "Ciencia Ficción / Comedia Negra / Drama",
    idioma: "Subtitulada",
    formatos: ["IMAX Láser", "2D"],
    director: "Bong Joon-ho",
    reparto: ["Robert Pattinson", "Steven Yeun", "Toni Collette", "Mark Ruffalo"],
    anio: "2025",
    sinopsis:
      "Mickey 17 es un empleado \"prescindible\" enviado a una expedición para colonizar el planeta helado Niflheim. Cada vez que muere en misión, un nuevo cuerpo es regenerado conservando la mayoría de sus recuerdos.",
    trailer: "osYpGSz_0i4",
    poster: "/posters/mickey-17.png",
    estrenoEnDias: 8,
    acento: "#0b2e3a",
  },
  {
    id: "superman",
    titulo: "Superman",
    tituloOriginal: "Superman",
    categoria: "proximamente",
    clasificacion: "PG-13",
    duracion: 140,
    duracionEstimada: true,
    genero: "Acción / Ciencia Ficción / Superhéroes",
    idioma: "Subtitulada",
    formatos: ["IMAX Láser", "Dolby Atmos", "3D"],
    director: "James Gunn",
    reparto: ["David Corenswet", "Rachel Brosnahan", "Nicholas Hoult", "Edi Gathegi"],
    anio: "2025",
    sinopsis:
      "La historia sigue a Superman mientras intenta reconciliar su herencia kryptoniana con su crianza humana como Clark Kent en Smallville, encarnando la bondad y la justicia en un mundo que considera esos valores obsoletos.",
    trailer: "Ox8ZLF6cGM0",
    poster: "/posters/superman.jpg",
    estrenoEnDias: 15,
    acento: "#0b1a4a",
  },
  {
    id: "tron-ares",
    titulo: "TRON: Ares",
    tituloOriginal: "TRON: Ares",
    categoria: "proximamente",
    clasificacion: "PG-13",
    duracion: 125,
    duracionEstimada: true,
    genero: "Ciencia Ficción / Acción",
    idioma: "Subtitulada",
    formatos: ["IMAX Láser", "3D"],
    director: "Joachim Rønning",
    reparto: ["Jared Leto", "Evan Peters", "Greta Lee", "Jodie Turner-Smith", "Jeff Bridges"],
    anio: "2025",
    sinopsis:
      "Un programa altamente sofisticado llamado Ares es enviado desde el mundo digital al mundo real en una misión crítica, marcando el primer encuentro directo de la humanidad con un ser inteligente de la Red.",
    trailer: "YShVEXb7-ic",
    poster: "/posters/tron-ares.jpg",
    estrenoEnDias: 28,
    acento: "#3a0b0b",
  },
  {
    id: "avengers-secret-wars",
    titulo: "Avengers: Secret Wars",
    tituloOriginal: "Avengers: Secret Wars",
    categoria: "proximamente",
    clasificacion: "PG-13",
    duracion: 180,
    duracionEstimada: true,
    genero: "Acción / Ciencia Ficción / Fantasía",
    idioma: "Subtitulada",
    formatos: ["IMAX Láser", "Dolby Atmos", "3D"],
    director: "Anthony Russo, Joe Russo",
    reparto: ["Robert Downey Jr. (Doctor Doom)", "Pedro Pascal", "Vanessa Kirby", "Anthony Mackie"],
    anio: "Próximo lanzamiento",
    sinopsis:
      "El colapso del multiverso provoca la colisión de realidades paralelas en un único mundo de batalla (Battleworld), donde héroes de múltiples dimensiones deben unir fuerzas para evitar la aniquilación total del cosmos.",
    poster: "/posters/avengers-secret-wars.png",
    posterLogo: true,
    estrenoEnDias: 90,
    acento: "#2e0b4a",
  },
];

export const CATEGORIAS: { id: Categoria; titulo: string; sub: string }[] = [
  { id: "cartelera", titulo: "En cartelera", sub: "Funciones disponibles hoy y los próximos días" },
  { id: "estreno", titulo: "Estrenos", sub: "Preestrenos activos" },
  { id: "proximamente", titulo: "Próximamente", sub: "Apertura de taquilla programada" },
];

// Último inicio permitido de una función. La programación la genera la API (backend/internal/domain/programacion.go).
export const ULTIMA_FUNCION_MIN = 22 * 60; // 22:00

export const EVENTOS: EstrenoConCola[] = [{ id: "e1", nombre: "Duna: Parte Dos — Preestreno IMAX", limitePorCliente: 2 }];

/**
 * Próxima función de un preestreno dentro de la cartelera vigente (la de la API o la generada en el navegador).
 * undefined si no queda ninguna por delante.
 */
export function funcionDelEvento(funciones: Funcion[], eventoId: string, ahora = new Date()): Funcion | undefined {
  return funciones.filter((f) => f.eventoId === eventoId && new Date(f.inicio) > ahora).sort((a, b) => a.inicio.localeCompare(b.inicio))[0];
}

// Preguntas basadas en los datos curiosos de las películas de la cartelera.
export const PREGUNTAS: Pregunta[] = [
  { texto: "En «The Batman», ¿qué se puso Paul Dano bajo la máscara del Acertijo para no dejar rastros de ADN?", opciones: ["Una media de nailon", "Plástico transparente envuelto en la cabeza", "Pintura corporal", "Un casco de cuero"], correcta: 1 },
  { texto: "¿Cuánto tiempo aguantó Kate Winslet la respiración bajo el agua en el rodaje de «Avatar: El Camino del Agua»?", opciones: ["4 minutos", "5 minutos 30 segundos", "7 minutos 15 segundos", "9 minutos"], correcta: 2 },
  { texto: "En «Gladiador», ¿qué personaje grita «¡¿No están entretenidos?!» en la arena?", opciones: ["Cómodo", "Máximo", "Próximo", "Lucila"], correcta: 1 },
  { texto: "¿Cuánto tiempo se aisló Heath Ledger en un hotel para crear al Joker de «El Caballero de la Noche»?", opciones: ["Casi un mes", "Una semana", "Tres meses", "Un año"], correcta: 0 },
  { texto: "¿Qué hito marcó «Deadpool & Wolverine» dentro del Universo Cinematográfico de Marvel?", opciones: ["La primera en blanco y negro", "La primera animada", "La primera con clasificación R", "La primera sin héroes"], correcta: 2 },
  { texto: "¿Cómo se crearon los sonidos de los gusanos de arena de «Duna: Parte Dos»?", opciones: ["Con sintetizadores", "Grabando ballenas", "Con motores de avión", "Enterrando micrófonos en la arena del desierto de Jordania"], correcta: 3 },
  { texto: "Para retratar un ataque de ansiedad en «Intensa-Mente 2», ¿con quién trabajó el equipo de animación?", opciones: ["Psicólogos del desarrollo", "Neurocirujanos", "Actores de teatro", "Atletas olímpicos"], correcta: 0 },
  { texto: "¿Cómo se recreó la prueba de la bomba Trinity en «Oppenheimer»?", opciones: ["Totalmente con CGI", "Con efectos físicos reales, sin CGI", "Con material de archivo", "Con dibujos animados"], correcta: 1 },
  { texto: "En «Spider-Man: Beyond the Spider-Verse», ¿de qué Tierra debe escapar Miles Morales?", opciones: ["Tierra-616", "Tierra-1610", "Tierra-42", "Tierra-65"], correcta: 2 },
  { texto: "¿Por qué James Cameron filmó «Avatar: Fuego y Ceniza» junto con «El Camino del Agua»?", opciones: ["Para ahorrar en vestuario", "Para evitar el envejecimiento visible de los actores jóvenes", "Por el clima de Nueva Zelanda", "Por un contrato con el estudio"], correcta: 1 },
  { texto: "Para las batallas navales de «Gladiator II», ¿qué hizo la producción?", opciones: ["Usó sólo CGI", "Filmó en el mar Mediterráneo", "Construyó una réplica del Coliseo y llenó la fosa de agua", "Usó maquetas en una piscina"], correcta: 2 },
  { texto: "¿A qué sello de DC pertenece la saga de «The Batman» de Matt Reeves?", opciones: ["Elseworlds", "DCEU", "Vertigo", "Black Label"], correcta: 0 },
  { texto: "¿Qué tuvieron que hacer los actores jóvenes de «Top Gun: Maverick» antes del rodaje?", opciones: ["Tomar clases de actuación con Tom Cruise", "Un entrenamiento de aviación de tres meses para soportar fuerzas G reales", "Obtener licencia de piloto comercial", "Vivir un mes en un portaaviones"], correcta: 1 },
  { texto: "En «Whiplash», ¿qué le pasó a Miles Teller durante el rodaje de la escena final?", opciones: ["Se fracturó una muñeca", "Perdió la audición temporalmente", "Terminó con ampollas sangrantes reales en las manos", "Rompió la batería original"], correcta: 2 },
  { texto: "¿Cuántas personas formaron el equipo principal de efectos visuales de «Todo en todas partes al mismo tiempo»?", opciones: ["Cinco", "Cincuenta", "Doscientas", "Ninguna: no tiene efectos"], correcta: 0 },
  { texto: "¿Qué es en realidad la famosa lluvia de código verde de «Matrix»?", opciones: ["Fórmulas de física cuántica", "Letras de canciones japonesas", "Código fuente de un videojuego", "Recetas de sushi escaneadas de un libro de cocina"], correcta: 3 },
  { texto: "¿Qué priorizó Fede Álvarez para la criatura de «Alien: Romulus»?", opciones: ["CGI de última generación", "Efectos prácticos y animatrónicos en tiempo real", "Captura de movimiento con IA", "Animación stop-motion"], correcta: 1 },
  { texto: "¿Cuántos días de rodaje requirió la persecución central de 15 minutos de «Furiosa»?", opciones: ["7 días", "21 días", "78 días", "150 días"], correcta: 2 },
  { texto: "¿Cómo se logró el realismo sonoro de los combates en «Civil War»?", opciones: ["Con armas de fogueo con decibelios idénticos a la munición real", "Grabando guerras reales", "Con sintetizadores", "Sólo en postproducción"], correcta: 0 },
  { texto: "¿Qué hizo el elenco de «El Planeta de los Simios: Nuevo Reino» antes de filmar?", opciones: ["Vivir en un zoológico", "Un «campo de entrenamiento de simios» de seis semanas", "Aprender lenguaje de señas", "Nada: todo fue animado"], correcta: 1 },
  { texto: "¿Qué hito marca «Mickey 17» para Bong Joon-ho?", opciones: ["Su primera película en inglés", "Su primera animación", "Su primer largometraje tras el triunfo de Parasite en los Óscar", "Su debut como actor"], correcta: 2 },
  { texto: "¿Qué ordenó James Gunn sobre el traje de «Superman»?", opciones: ["Que fuera totalmente digital", "Que fuera de tela física sin retoques por computadora", "Que no tuviera capa", "Que fuera negro"], correcta: 1 },
  { texto: "¿Quién compuso la música original de «TRON: Ares»?", opciones: ["Daft Punk", "Hans Zimmer", "Nine Inch Nails (Trent Reznor y Atticus Ross)", "Ludwig Göransson"], correcta: 2 },
  { texto: "¿Qué marca «Avengers: Secret Wars» para los hermanos Russo?", opciones: ["Su debut en el cine", "Su regreso a la dirección en Marvel Studios tras Infinity War y Endgame", "Su primera película de DC", "Su retiro"], correcta: 1 },
];

// Películas creadas desde el panel (API o navegador). El store las registra antes de pintar,
// así pelicula(id) y finFuncion() funcionan igual para las del catálogo base y las nuevas.
let extras: Pelicula[] = [];
export function registrarPeliculas(lista: Pelicula[]) {
  extras = lista;
}

// Respaldo por si una función llega antes que su película (no debería ocurrir): evita romper la página.
const DESCONOCIDA: Pelicula = {
  id: "desconocida", titulo: "Película no disponible", tituloOriginal: "", categoria: "cartelera", clasificacion: "—", duracion: 120,
  genero: "", idioma: "", formatos: [], director: "", reparto: [], anio: "", sinopsis: "", poster: "/posters/batman-2.png", posterLogo: true, acento: "#1b1b1e",
};

export const pelicula = (id: string): Pelicula => PELICULAS.find((p) => p.id === id) ?? extras.find((p) => p.id === id) ?? DESCONOCIDA;
export const sala = (id: string) => SALAS.find((s) => s.id === id)!;

export function fechaEstreno(p: Pelicula) {
  const d = new Date();
  d.setDate(d.getDate() + (p.estrenoEnDias ?? 0));
  return d.toLocaleDateString("es-GT", { day: "numeric", month: "long" });
}

export function finFuncion(f: Funcion) {
  const p = pelicula(f.peliculaId);
  return new Date(new Date(f.inicio).getTime() + (p.duracion + PARAMETROS.limpiezaMinutos) * 60000);
}


export const FILAS = "ABCDEFGHIJKLMNOP";
export const etiqueta = (fila: number, col: number) => `${FILAS[fila]}-${String(col + 1).padStart(2, "0")}`;


