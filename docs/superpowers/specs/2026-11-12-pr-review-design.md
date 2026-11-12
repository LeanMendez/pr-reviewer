# pr-review: diseño

Fecha: 2026-11-12. Estado: diseño, sin código.

## 1. Para qué existe

El equipo escribe cada vez más código con IA y el volumen superó el tiempo
disponible para revisarlo a conciencia. `pr-review` es un **primer filtro**
para los PRs de Bitbucket de los compañeros: no reemplaza al revisor humano,
hace que llegue al PR sabiendo dónde mirar.

Hoy existe un script de bash (`review-pr`) que baja contexto y diff con
prismatic-rainbow y lanza **un solo** Claude con unas reglas personales.
Funciona, pero se le escapan cosas y reporta ruido. Las causas que vemos:
un solo agente con todo el diff, solo ve el diff (no el repo), nadie intenta
refutar los hallazgos, chequeos mecánicos hechos por IA, el mismo modelo para
todo.

Primera etapa: **uso personal**. El autor la usa desde su máquina y, recién
cuando confíe en los resultados, la lleva al equipo.

## 2. Objetivos, en orden de prioridad

1. **Que no se escape nada grave.** Ante la duda en algo grave, se reporta.
2. **Poco ruido.** El review no puede ser un tribunal ni hacer que todo
   parezca un error. Ante la duda en algo menor, se calla.
3. **Separar lo que introduce el PR de lo que ya existía.** La deuda heredada
   no cuenta en contra del autor del PR.
4. **Empujar PRs más chicos**, con una propuesta concreta de cómo partirlos.
5. **Cada hallazgo ayuda a resolverlo:** qué pasa, por qué pasa (muy breve) y
   una solución posible que cumple las normas del repo.

## 3. Fuera de la v1

- Publicar comentarios, aprobar o pedir cambios en Bitbucket. La herramienta
  no escribe nada en Bitbucket. (El reporte se guarda estructurado para que
  publicar sea posible más adelante.)
- Correr en Bitbucket Pipelines o como servicio del equipo.
- Ejecutar el código del PR (tests, linters, builds) en la máquina local.
- Aprender de los hallazgos descartados por el usuario. (Se guardan los datos
  para poder hacerlo después.)
- Re-revisar solo lo que cambió desde la revisión anterior.
- Comparar el PR contra los criterios de aceptación de la tarea de ClickUp.
- Revisar el impacto en **otros repos** del workspace (por ejemplo, quién
  consume una API que cambió). Cada revisión mira un solo repo. Cuando el PR
  cambia un contrato público (API, evento, esquema de base compartido), el
  agente de tests y contratos lo deja como **duda** en el reporte: "puede
  afectar a otros repos, revisalo".
- Un banco de pruebas con PRs históricos. El autor va a probar la herramienta
  con PRs reales una vez construida.

## 4. Anti-objetivos (aunque funcione, sería un fracaso si…)

- El reporte hace que todo parezca un error o es tortuoso de leer para quien
  abrió el PR.
- Culpa al autor por problemas que ya estaban antes de su PR.
- Un hallazgo grave queda tapado por una lista de detalles menores.
- Dice "sin problemas" cuando en realidad un agente falló o una parte del PR
  quedó sin revisar.
- Sigue instrucciones escritas dentro del PR (título, descripción, código,
  comentarios).

## 5. Restricciones

- **Lenguaje:** Go. CLI con **Cobra** e interfaz con **Bubble Tea** (y el
  resto del stack de Charm: Lip Gloss, Bubbles). Tiene que ser agradable de
  usar, no un CLI feo.
- **Datos de Bitbucket:** solo a través de prismatic-rainbow
  (`prb`, repo `github.com/LeanMendez/prismatic-rainbow`), llamado **como
  comando externo** con `-o json`. No se importan sus paquetes: los dos
  proyectos quedan desacoplados. Su JSON es un contrato estable
  (`internal/cmd/pr/model.go` en ese repo).
- **Agentes:** corren con la **suscripción de Claude Code** del usuario, como
  procesos `claude -p` (Claude Code 2.1.322 instalado). No se usa una clave de
  API.
- **Normas del repo:** están en el `CLAUDE.md` de cada repo y en los archivos
  que referencia.
- **Idioma:** la interfaz y los reportes en español.

## 6. Arquitectura

### 6.1 Quién conduce

**El programa en Go conduce todas las etapas** (decisión del usuario, opción A).
Go decide el orden, lanza los agentes en paralelo, aplica los modelos de cada
rol, junta resultados y arma el reporte. El **agente superior** (modelo
fuerte) solo hace la parte de criterio: decide el plan de revisión y escribe la
síntesis. Se descartó la alternativa de que una sesión de Claude orqueste todo
con subagentes, porque podría saltear pasos y es más difícil de testear.

El modelo de cada rol se fija por código al lanzar el proceso (`--model`,
`--effort`), nunca por instrucciones en un prompt. Es el mismo principio que
usa gentle-ai: el contrato de modelo por rol se aplica en la capa del agente,
no en la prosa del orquestador.

### 6.2 Cómo se ejecuta cada agente

Cada agente es un proceso `claude -p` aislado. Base de flags (verificados en la
ayuda de Claude Code 2.1.322; la combinación viene del revisor aislado de
gentle-ai, `internal/reviewerprovider/claude_adapter.go`):

- `--model <alias>` y `--effort <nivel>` según la configuración del rol.
- `--json-schema <schema>` para que devuelva hallazgos con formato fijo.
- `--setting-sources ""`: sin hooks, plugins, permisos ni MCP del usuario.
- `--system-prompt <prompt del rol>`: reemplaza el contexto por defecto
  (incluido el `CLAUDE.md` global del usuario).
- `--tools` / `--allowedTools`: solo lectura. `Read`, `Grep`, `Glob` y, como
  mucho, `git` de solo lectura (`git show`, `git blame`, `git log`,
  `git diff`). Nada que escriba archivos ni acceda a la red.
- `--permission-mode dontAsk` y `--no-session-persistence`.
- Directorio de trabajo: el worktree del PR (ver 6.4).

gentle-ai documenta que `--bare` rompe la autenticación por suscripción
(OAuth); por eso se usa esta combinación y no `--bare`.

El contenido del PR (título, descripción, diff, comentarios, código) se pasa
siempre como **dato no confiable**, delimitado, con la instrucción explícita de
no seguir instrucciones que aparezcan ahí.

Límite de procesos `claude` en paralelo: configurable, para no agotar el límite
de uso de la suscripción. Timeout por agente: configurable. Si un agente falla
o vence, la revisión sigue con los demás y el reporte dice qué agente falló y
qué parte quedó sin revisar.

### 6.3 Datos de Bitbucket (prb)

- **Un repo por vez** (decisión del usuario): la herramienta se abre con el
  nombre del repo como argumento (`pr-review stock`, `pr-review ventas`,
  `pr-review logistica`) y la lista muestra solo los PRs de ese repo. El
  workspace sale de la configuración; también se acepta `workspace/repo`.
- `prb pr list -R <workspace>/<repo> --state open -o json` para el listado
  base.
- `prb pr context <n> -o json` por cada PR del listado, en paralelo: trae draft,
  revisores y su estado, checks de CI, mergeability, archivos, commits, hilos,
  tareas y referencias de ClickUp.
- `prb pr diff <n> -o json` (con `--exclude` para generados y lockfiles) y
  `prb pr file` cuando haga falta.
- Errores: prb los devuelve en JSON con `code` y `retryable`. Exit 4 significa
  credenciales faltantes; la configuración inicial lo detecta.

**Un PR aparece en la lista solo si** (decisión del usuario):

- no está en borrador,
- el CI pasó,
- no tiene tareas del PR sin resolver,
- ningún revisor pidió cambios.

Además (decisión del asistente): no aparecen los PRs que abrió el propio
usuario ni los que ya aprobó. Un repo sin pipelines cuenta como CI aprobado,
porque no hay nada fallando; el detalle del PR muestra "sin CI".

Los que no cumplen se cuentan en una línea ("4 PRs no aparecen: 2 en borrador,
1 con CI fallando, 1 con cambios pedidos") y se pueden mostrar con una tecla.

### 6.4 Acceso al código

Los agentes necesitan ver el repo, no solo el diff (quién llama a la función,
qué tests existen, qué convenciones hay). La herramienta mantiene **su propia
copia de cada repo** en una carpeta de caché y, por revisión, crea un
`git worktree` en el commit de origen del PR. Nunca toca las copias de trabajo
del usuario. El commit de destino (base) queda disponible para comparar y para
decidir si un problema ya existía.

Las **reglas del repo se leen de la rama destino**, no del PR: así un PR no
puede cambiar las reglas con las que se lo juzga. Cargador de reglas: el
`CLAUDE.md` de la raíz, los `CLAUDE.md` de las carpetas de los archivos
cambiados, y los archivos que referencian (siguiendo referencias `@ruta` y
links, con límite de profundidad y de tamaño). El reporte lista qué archivos de
reglas se cargaron.

### 6.5 Las etapas

Son siempre las mismas y en este orden. Según el modo, algunas no corren.

| # | Etapa | Qué hace | Ligero | Exhaustivo | Con judgment |
|---|---|---|---|---|---|
| 1 | Chequeos sin IA | Ver 6.6 | sí | sí | sí |
| 2 | Reglas del repo | Carga las reglas desde la rama destino | sí | sí | sí |
| 3 | Plan | Resumen inicial y plan del agente superior | solo resumen | sí | sí |
| 4 | Especialistas | Agentes en paralelo, cada uno con un foco | bugs y reglas | los cuatro | los cuatro |
| 5 | Validación | Confirma o descarta cada candidato grave | sí | sí | sí |
| 6 | Jueces | Dos jueces independientes | no | no | sí |
| 7 | Reporte | Síntesis, separación nuevo/preexistente, cómo partir | sí | sí | sí |

En la etapa 3, el resumen inicial (modelo barato) resume el PR y marca qué
archivos son triviales o generados. El agente superior decide qué
especialistas hacen falta y qué grupos de archivos mira cada uno. En PRs
grandes, Go puede lanzar más de una instancia del mismo especialista, una por
grupo de archivos.

El modo **sugerido** para cada PR lo propone el CLI según tamaño y qué toca
(por ejemplo, migraciones o autenticación sugieren exhaustivo). El usuario
decide.

### 6.6 Chequeos sin IA (etapa 1)

Reglas fijas, rápidas y deterministas. Sus resultados se les pasan a los
agentes para que no los repitan. Cada chequeo tiene un destino distinto:

- **Secretos** en líneas agregadas (tokens, claves, passwords, URLs con
  credenciales, `.env`, claves privadas): cada coincidencia es un **candidato
  Crítico** y pasa por el validador de bugs y seguridad como cualquier otro,
  porque las expresiones regulares dan falsos positivos.
- **Proceso**: el título no empieza con el ID de la tarea (prefijo
  configurable), o el PR pasa de 400 líneas o 15 archivos (configurable) con la
  descripción vacía o mínima. Son hallazgos **Bajo** de proceso: se muestran en
  una línea compacta y como mucho llevan el veredicto a "aprobar con
  comentarios".
- **Tamaño** sobre el umbral: dispara la propuesta de cómo partir el PR (6.10).
- **Cambios sensibles** (config, CI, Docker, dependencias, lockfiles,
  migraciones, permisos): no son hallazgos. Se marcan para que los agentes
  verifiquen si la descripción del PR los justifica.
- **Archivos generados y lockfiles** se excluyen de la revisión de los agentes y
  quedan listados como no revisados (esto no hace incompleta la revisión).

### 6.7 Roles y modelos

| Rol | Qué mira | Modelo (Balanceado) |
|---|---|---|
| Agente superior | Plan y síntesis | opus, esfuerzo alto |
| Resumen inicial | Resume el PR, marca lo trivial | haiku |
| Bugs y lógica | Errores que rompen el comportamiento | opus, alto |
| Seguridad | Validación de datos, autorización, SQL, XSS, secretos | opus, medio |
| Reglas del repo | Cumplimiento de `CLAUDE.md` y sus archivos | sonnet, medio |
| Tests y contratos | Lógica nueva sin tests, quién llama a lo que cambió, APIs o migraciones que rompen a otros, N+1 | sonnet, medio |
| Validador de bugs | Intenta descartar candidatos de bugs y seguridad | opus, alto |
| Validador de reglas | Confirma que la regla existe, aplica a ese archivo y se viola | sonnet, medio |
| Juez A | Las tres preguntas de 6.9 | opus, alto |
| Juez B | Lo mismo, sin ver al juez A | sonnet, alto |

Combinaciones armadas: **Balanceado** (la tabla), **Económico** (sonnet casi
en todo), **Potente** (opus en todo con más esfuerzo) y **Personalizado**. Los
jueces usan modelos distintos a propósito, para tener dos miradas. Modelos:
`fable`, `opus`, `sonnet`, `haiku`. Esfuerzo: `low`, `medium`, `high`, `xhigh`,
`max` (en la interfaz: bajo, medio, alto, muy alto, máximo). La interfaz solo
ofrece combinaciones de modelo y esfuerzo que Claude Code admite.

### 6.8 El hallazgo

Todos los agentes devuelven hallazgos con el mismo formato (forzado con
`--json-schema`):

- `ubicacion`: `archivo:línea` o rango.
- `severidad`: Crítico, Alto, Medio o Bajo.
- `origen`: introducido por el PR, activado por el PR, empeorado por el PR, ya
  existía, o no se puede saber. (Tomado del contrato de jueces de gentle-ai.)
  Introducido, activado y empeorado cuentan como **del PR**. "Ya existía" va a
  la sección gris y nunca cuenta para el veredicto. "No se puede saber" se
  muestra en la sección principal con la marca "origen incierto" (ver
  veredicto en 6.9).
- `grupo`: A o B, según la política de revisión (ver abajo y el anexo A).
- `funcion`: la función o método afectado, cuando aplica. Go lo usa para la
  regla del Grupo B.
- `que_pasa`: una frase.
- `por_que`: como máximo dos líneas.
- `solucion`: como máximo 10 líneas, mostradas como código actual → código
  corregido. Si aplica una norma del repo, cita la regla y su archivo.
- `evidencia`: referencias `archivo:línea` que respaldan el hallazgo. Una
  opinión sin evidencia no es un hallazgo.
- `regla`: si el hallazgo es de normas, la cita exacta y el archivo.

Lista fija de cosas que **no se reportan** (tomada del plugin `code-review` de
Anthropic y de PR-Agent): lo que detecta un linter, problemas que ya existían
reportados como si fueran del PR, nombres que pueden estar definidos en otro
lado del repo, reglas que el código silenció explícitamente, calidad general
sin una regla que la pida.

**Política de revisión de la herramienta.** Las reglas personales que el autor
usa hoy con su script pasan a ser la política por defecto. El texto está en el
**anexo A**; se embebe en el binario y se puede sobrescribir con un archivo en
la configuración. Cómo se aplica:

- **Grupo A** (tests, secretos, config sin justificación, tamaño, título,
  seguridad, riesgo): se reporta siempre, con severidad Crítico, Alto, Medio o
  Bajo. Solo el Grupo A define el veredicto.
- **Grupo B** (calidad menor y sobreingeniería con umbrales): va a "detalles
  menores y preguntas de diseño", nunca cambia el veredicto, y **Go lo filtra**
  después de la validación: un hallazgo B solo se muestra si hay un hallazgo A
  confirmado en el mismo archivo o en la misma función.
- Si la política y las normas de un repo se contradicen, **ganan las normas del
  repo** sobre cómo se escribe el código. La política decide qué vale la pena
  reportar y el formato del reporte.

### 6.9 Validación y jueces

**Validación (todos los modos).** Cada candidato Crítico o Alto pasa por un
validador propio, en paralelo, que lee el repo. El validador tiene tres
respuestas posibles:

- **Confirmado**, con evidencia `archivo:línea`: pasa al reporte.
- **Refutado**, con evidencia de por qué no es un problema: se descarta y queda
  registrado con el motivo (se ve con `l` en el reporte).
- **Sin resolver**: no pudo confirmarlo ni refutarlo. Por el objetivo 1, se
  mantiene en la sección principal marcado como **"sin confirmar"**. Si el
  validador falla o vence, el candidato queda igual: sin confirmar.

Los Medio y Bajo no se validan uno por uno: los agentes tienen instrucción de
reportarlos solo si están seguros, y hay un tope.

El validador también decide el **origen** comparando con el commit de destino
(si el mismo problema ya está en la rama destino, es preexistente).

**Jueces (modo con judgment).** Dos jueces, cada uno sin ver al otro, reciben
la lista de hallazgos confirmados, el diff, acceso al repo y las reglas. Para
cada hallazgo responden, siempre con evidencia:

1. ¿Es real y es de este PR?
2. ¿Vale la pena reportarlo, o es ruido?
3. ¿La solución propuesta cumple las normas del repo?

Reglas de decisión:

- Un Crítico o Alto se descarta solo si **los dos** jueces lo refutan con
  evidencia. Los hallazgos de seguridad y de lógica nunca se silencian por
  parecer ruido.
- Un Medio o Bajo se descarta si **cualquiera** de los dos lo refuta, y pasa a
  "detalles menores" si los dos dicen que no vale la pena.
- Si un juez dice que la solución viola una norma, tiene que citar la regla.
  La solución se reemplaza por la alternativa del juez o se marca como
  "solución a revisar", con el motivo.
- Que un juez opine sin evidencia no cuenta. Greptile documenta que un juez de
  IA puntuando de 1 a 10 daba resultados casi aleatorios: acá se le pide
  verificación, no puntaje.

**Veredicto.** Lo calcula Go con reglas fijas, no lo decide un modelo. Solo
cuentan los hallazgos del Grupo A cuyo origen es del PR. Se evalúa en este
orden:

1. **Pedir cambios**: hay al menos un Crítico o Alto confirmado.
2. **Revisar a mano**: no hay ninguno confirmado, pero hay un Crítico o Alto
   sin confirmar o con origen incierto, o la revisión quedó incompleta (un
   agente falló o venció, o quedaron sin revisar archivos que no son
   generados). Nunca dice "aprobar" en estos casos.
3. **Aprobar con comentarios**: solo hay Medio o Bajo confirmados (incluidos
   los de proceso de 6.6).
4. **Aprobar**: no hay nada.

Si el veredicto es "pedir cambios" y además la revisión quedó incompleta, se
agrega el aviso "revisión incompleta" en la primera línea. Lo que ya existía
nunca cambia el veredicto. El comando sin interfaz devuelve un código de salida
distinto por veredicto (los valores quedan a criterio de quien lo construya).

### 6.10 Cómo partir el PR

Cuando el PR supera el umbral de tamaño, el agente superior propone hasta 3
PRs más chicos, cada uno con título, archivos y líneas. Cada parte lleva sus
propios tests (nunca un PR solo de tests). Si una parte depende de otra, se
dice cuál va primero. (Tomado de PR-Agent.)

## 7. Interfaz

Los bocetos aprobados están en
`pr-review/.superpowers/brainstorm/*/content/` (`pr-list-layout-v3.html`,
`progress-v3.html`, `report-v2.html`, `models-v2.html`).

### 7.1 Pantallas

1. **Configuración inicial** (la primera vez): comprueba que `prb` y `claude`
   están instalados y con sesión iniciada, elige el workspace y, como último
   paso, los modelos de los agentes.
2. **Lista de PRs** (variante A), de un solo repo, con el nombre del repo en
   el encabezado: tabla con número, título, autor, barra de tamaño y
   antigüedad (la columna "repo" del boceto sobra, porque todo es del mismo
   repo); abajo, el detalle del PR seleccionado (CI, tareas,
   revisores, referencia de ClickUp, si pr-review ya lo revisó, archivos y
   modo sugerido). La barra de tamaño tiene una marca vertical en 400 líneas;
   lo que pasa de ahí va en ámbar.
3. **Elegir el modo:** los tres modos con el sugerido marcado. Cada uno muestra
   cuántos agentes lanza y con qué modelos, antes de confirmar.
4. **Progreso:** las siete etapas con su estado; en la que corre, una fila por
   agente con modelo, qué está haciendo, tiempo y candidatos. Los hallazgos no
   se muestran hasta pasar la validación, solo el contador.
5. **Reporte:** veredicto en una frase; lo que introduce el PR con qué pasa, por
   qué, solución con la regla citada y, en modo judgment, lo que dijo cada
   juez; los hallazgos "sin confirmar" y de "origen incierto" van en esta misma
   sección con esa marca visible; detalles menores y preguntas de diseño
   plegados y con tope; lo que ya existía, aparte y en gris; dudas (por
   ejemplo, posible impacto en otros repos); cómo partir el PR; al pie, lo
   descartado (con motivo), lo no revisado, los agentes que fallaron, las
   reglas usadas y dónde se guardó.
6. **Configuración de modelos** (tecla `m` desde la lista): una fila por
   agente, modelo con flechas, esfuerzo con `e`, combinaciones armadas con
   `p`, guardar con `g`. Lo que cambió respecto de la combinación de partida
   se marca.

### 7.2 Teclas principales

- Lista: `↑↓` moverse, `enter` elegir modo, `/` filtrar, `t` ver todos,
  `r` actualizar, `m` modelos, `q` salir.
- Progreso: `s` seguir en segundo plano, `l` ver qué hace cada agente,
  `c` cancelar.
- Reporte: `↑↓` moverse, `enter` expandir, `c` copiar hallazgo (listo para
  pegar como comentario en Bitbucket), `l` ver descartados, `o` abrir el PR en
  el navegador, `q` volver a la lista.

### 7.3 Segundo plano

Con `s`, la revisión sigue corriendo dentro del mismo proceso y se vuelve a la
lista. Al terminar llega un aviso del escritorio. Si el usuario intenta salir
con revisiones en curso, se le pide confirmación.

### 7.4 Estilo visual

Calmo por defecto; el color solo transmite estado. Tokens de los bocetos (para
fondo oscuro; para terminales claras hay que definir la variante):

| Uso | Color |
|---|---|
| Texto | `#D9DCE4` |
| Atenuado, metadatos, preexistentes | `#7D859A` |
| Listo, confirmado | `#86C9A8` |
| Selección, sobre el umbral, atención | `#E7B45A` |
| Referencias (archivos, ClickUp, modelos) | `#A99DD8` |
| Crítico (único uso del rojo) | `#EE7F7A` |

El rojo aparece solo para hallazgos críticos. Un PR grande no es un error: se
marca en ámbar.

## 8. Comandos

- `pr-review <repo>`: abre la interfaz con los PRs de ese repo. Sin argumento,
  si se corre dentro de una copia de un repo de Bitbucket usa ese repo; si no,
  muestra un selector con los repos del workspace (`prb repo list`).
- `pr-review review <repo> <pr> --mode ligero|exhaustivo|judgment`: revisión
  sin interfaz, como el `--batch` del script actual.
- `pr-review config models`: abre la configuración de modelos.

## 9. Configuración y archivos

`~/.config/pr-review/config.yaml`:

- workspace de Bitbucket (el repo se pasa al abrir la herramienta);
- modelo y esfuerzo por rol, y la combinación de partida;
- umbrales: 400 líneas, 15 archivos, tope de detalles menores;
- prefijo del ID de tarea;
- procesos `claude` en paralelo y timeout por agente;
- carpeta de reportes y carpeta de caché de repos.

Junto a la configuración: la política de revisión (6.8) y los prompts de cada
rol, que vienen embebidos en el binario y se pueden sobrescribir.

Cada revisión se guarda en `~/Documentos/reviews/<repo>/pr-<N>-<fecha>/`, la
misma carpeta que usa el script actual: `context.json`, `pr.diff`,
`findings.json` (todos los candidatos con su estado y motivo, incluidos los
descartados), `review.md` y un log por agente.

## 10. Lo que queda a criterio de quien lo construya

- Estructura de paquetes de Go, nombres internos y prompts exactos de cada rol.
- Esquemas JSON exactos, respetando los campos de 6.8.
- Cómo se agrupan archivos y cuándo un especialista se divide en varias
  instancias.
- Límites concretos del cargador de reglas (profundidad, tamaño).
- Patrones exactos de secretos y de archivos sensibles o generados.
- Cómo se clonan los repos en caché (SSH o HTTPS con las credenciales de git
  del usuario).
- Valores por defecto de concurrencia y timeouts.
- Variante de colores para terminal clara y adaptación a terminales de menos de
  100 columnas.
- Estrategia de tests. Sugerencia: simular `prb` y `claude` con procesos falsos
  (como hace gentle-ai con subprocesos auxiliares), tests de golden para el
  reporte y `teatest` para la interfaz.
- Cómo se detecta qué combinaciones de modelo y esfuerzo admite Claude Code.

## 11. Decisiones tomadas por el asistente (para revisar)

El usuario las aprobó como parte de los bocetos y descripciones, pero no las
pidió explícitamente:

- Las combinaciones de modelos por rol y los modelos de la tabla 6.7.
- Que la herramienta no ejecute el código del PR (los tests ya corrieron en el
  CI, que es condición para aparecer en la lista).
- Caché propia de repos con `git worktree`, sin tocar las copias del usuario.
- Que el veredicto lo calcule Go con reglas fijas, no un modelo.
- Las reglas de decisión de los jueces (6.9).
- Que solo Crítico y Alto pasen por validador individual.
- Que las reglas personales actuales pasen a ser la política por defecto.
- El modo sugerido en la lista y la confirmación de agentes y modelos antes de
  lanzar.
- El comando `review` sin interfaz y la carpeta de reportes del script actual.
- Que la comparación con criterios de aceptación de ClickUp quede fuera de la
  v1.
- Tres respuestas del validador (confirmado, refutado, sin resolver) y que lo
  grave sin resolver se muestre como "sin confirmar" en vez de descartarse.
- El veredicto "revisar a mano" para revisiones incompletas o con graves sin
  confirmar o de origen incierto.
- Qué orígenes cuentan como del PR (introducido, activado, empeorado).
- Destino de cada chequeo sin IA: secretos como candidato Crítico validado;
  título, descripción y tamaño como Bajo de proceso.
- Que Go aplique la regla del Grupo B y que las normas del repo ganen sobre la
  política cuando se contradicen.
- Impacto en otros repos fuera de la v1, solo como duda en el reporte.
- Ocultar de la lista los PRs propios y los que el usuario ya aprobó; contar
  un repo sin pipelines como CI aprobado.
- Sin argumento: usar el repo de la carpeta actual o mostrar un selector.

## Anexo A: política de revisión por defecto

Texto de las reglas que el autor usa hoy con su script, adaptado: se quitó la
sección sobre cómo obtener datos de Bitbucket (ahora lo hace la herramienta con
prb) y los números pasan a ser umbrales configurables, con estos valores por
defecto.

### Grupo A: se reporta siempre

- Tests: lógica nueva o modificada sin tests.
- Secretos: tokens, API keys, passwords, URLs con credenciales, .env o claves
  privadas.
- Config sin justificación: cambios en config, CI, Docker, dependencias,
  lockfiles, migraciones o permisos que la descripción del PR no explica.
- Tamaño: diffs de más de 400 líneas o 15 archivos con descripción vacía o
  mínima.
- Título: que empiece con el ID de la tarea (prefijo configurable, por ejemplo
  `ABC-1234`).
- Seguridad: inputs sin validar, SQL armado a mano, XSS, autorización faltante.
- Riesgo: breaking changes en APIs, migraciones irreversibles, N+1 o queries en
  loops.

### Grupo B: solo si en el mismo archivo o función hay un hallazgo del Grupo A

- Calidad menor: código comentado, logs de debug, TODOs sin ticket, nombres
  confusos.
- Sobreingeniería. Se marca solo si se cumple al menos uno de estos umbrales:
  1. Más de 2 niveles de indirección (wrapper → servicio → adapter…) para una
     operación que se llama desde un solo lugar.
  2. El mismo patrón o bloque estructural repetido en más de 3 archivos sin un
     comentario, ADR o descripción del PR que lo justifique.
  3. Una abstracción nueva (clase, interfaz, helper, factory) que agrega más de
     40 líneas y no reduce las líneas ni las ramas del código que la llama.
  4. Una interfaz o factory con una sola implementación y ningún test que la
     mockee.

  Si no se cumple ningún umbral, no se menciona, aunque parezca "rara". Formato:
  pregunta abierta y camino más simple sugerido. Nunca bloquea el review; la
  decisión es del revisor humano.

### Formato

- Lo que no se pueda confirmar desde el código va como duda, no como hallazgo.
- Todo en español.
