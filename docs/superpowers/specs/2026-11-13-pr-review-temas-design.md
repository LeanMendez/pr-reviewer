# pr-review: temas de color — diseño

Fecha: 2026-11-13. Estado: diseño, sin código. Complementa
`2026-11-12-pr-review-design.md` (sección 7.4, estilo visual).

## 1. Para qué

Hoy la interfaz de terminal tiene un solo juego de colores, pensado para fondo
oscuro y sin pintar el fondo (usa el de la terminal). El usuario quiere poder
elegir entre varios temas, cada uno con su **aspecto completo**: fondo propio
y colores propios, independientes de la terminal. Esto además resuelve el
pendiente de la variante para terminales claras (hay temas claros).

## 2. Objetivos

1. Elegir el tema desde la pantalla de configuración, **con vista previa** en
   vivo antes de confirmar.
2. 14 temas fijos (sección 5). No hay temas definidos por el usuario.
3. Todo tema es **accesible**: el texto llega a contraste 4,5:1 contra su
   fondo (WCAG AA) y los elementos gráficos (separadores, marcas de la barra
   de tamaño) a 3:1.
4. El tema elegido queda guardado y se usa en todas las pantallas.

## 3. Fuera de alcance

- Temas creados o editados por el usuario (archivos propios).
- Cambiar los colores de `review.md`, de la salida del comando `review` o de
  las notificaciones de escritorio: los temas son solo de la interfaz.
- Los banners, arte ASCII, frases de bienvenida y sonidos de claude-skins:
  solo se toman los colores.

## 4. Anti-objetivos

- Un tema que se lea mal: letras sin contraste suficiente, o el rojo de
  "Crítico" ilegible.
- Fondos cambiados para arreglar contraste: **los fondos no se tocan**, se
  ajustan solo las letras (decisión del usuario).
- Que una parte de la pantalla quede con el fondo de la terminal y otra con el
  del tema.

## 5. Los 14 temas

Cada tema tiene los mismos tokens que hoy usa la interfaz (spec 7.4) más el
fondo:

| Token | Uso |
|---|---|
| `bg` | fondo de toda la pantalla |
| `text` | texto normal |
| `dim` | metadatos, texto atenuado |
| `strong` | títulos, selección (negrita) |
| `accent` | selección, sobre el umbral, atención |
| `ok` | listo, confirmado |
| `ref` | referencias: archivos, ClickUp, modelos |
| `crit` | fondo de la etiqueta "Crítico" (único uso del rojo) |
| `tick` | separadores y marcas de la barra de tamaño (gráfico) |
| `gradient` | opcional: lista de colores para degradados (solo gemini) |

### 5.1 Propios (valores base elegidos por el asistente a partir de las referencias del usuario)

| Tema | Descripción | bg | text | dim | strong | accent | ok | ref | crit | tick |
|---|---|---|---|---|---|---|---|---|---|---|
| `pr-review` (por defecto) | el tema que diseñamos | `#1E2331` | `#D9DCE4` | `#7D859A` | `#F3F4F8` | `#E7B45A` | `#86C9A8` | `#A99DD8` | `#EE7F7A` | `#4E5568` |
| `claude-code` | claro, crema y ladrillo | `#F4EFE6` | `#2B2622` | `#7A7068` | `#14110E` | `#C15F3C` | `#4F7A3A` | `#3E6A8A` | `#B3261E` | `#D6CCBD` |
| `codex` | sobrio, toques verde agua | `#000000` | `#D4D4D4` | `#7A7A7A` | `#FFFFFF` | `#3CC4B4` | `#5FB39A` | `#8FB7B2` | `#E5534B` | `#2A2A2A` |
| `grok` | minimalista, detalle dorado | `#0B0B0B` | `#D9D9D9` | `#808080` | `#FFFFFF` | `#D4A537` | `#9CC48A` | `#7FA7E8` | `#E06C6C` | `#2B2B2B` |
| `gemini` | degradado azul, violeta y rosa | `#1E1E2E` | `#CDD6F4` | `#7F849C` | `#FFFFFF` | `#B58BD6` | `#94E2D5` | `#89B4FA` | `#F38BA8` | `#313244` |

`gemini` agrega `gradient: #4796E4 → #847ACE → #C3677F`, que se usa en el
nombre "pr-review" del encabezado, en el marcador de selección `▌` y en el
relleno de la barra de tamaño. Ningún otro tema usa degradado.

### 5.2 De claude-skins

Repositorio `github.com/basicScandal/claude-skins`, carpeta `skins/`, commit
`efa1dca`, licencia MIT. Se toman 9 skins: `nebula`, `noir`, `netrunner`,
`grimoire`, `mission-control`, `mythos`, `retro86`, `brutalist`, `sensei`.
Se excluye `default`, porque no define fondo (usa el de la terminal).

Correspondencia de cada skin a los tokens:
`bg` = `terminal.background`, `text` = `terminal.foreground`,
`dim` = `statusline.dim`, `accent` = `statusline.accent`,
`ok` = `terminal.palette.green`, `ref` = `terminal.palette.cyan`,
`crit` = `terminal.palette.red`, `tick` = `statusline.bar_empty`,
`strong` = `terminal.palette.white`.

Descripciones en español: nebula "violeta y naranja", noir "negro y crema",
netrunner "cian cyberpunk", grimoire "pergamino y marrón", mission-control
"ámbar sobre azul noche", mythos "azul profundo y dorado", retro86
"Commodore 64", brutalist "monocromo puro", sensei "claro, tinta japonesa".

El aviso de copyright y la licencia MIT de claude-skins se incluyen en el
repo (archivo de avisos de terceros) y en el código donde están sus paletas.

## 6. Regla de accesibilidad

- Mínimos: `text`, `dim`, `strong`, `accent`, `ok`, `ref`, `crit` y cada color
  de `gradient` llegan a **4,5:1** contra `bg`. `tick` llega a **3:1**.
  Contraste según la fórmula de WCAG 2 (luminancia relativa).
- Si un color no llega, se ajusta **solo su luminosidad** (mismo tono y
  saturación, en HSL), aclarándolo sobre fondo oscuro u oscureciéndolo sobre
  fondo claro, en el menor paso que alcance el mínimo. **El fondo nunca se
  modifica.**
- Etiqueta "Crítico": el texto de la etiqueta (sobre fondo `crit`) es, entre
  `bg`, `strong`, blanco y negro, el que tenga más contraste con `crit`.
- Los valores ajustados quedan **fijos en el código** (no se calculan al
  arrancar), y un test verifica los mínimos de los 14 temas.
- Referencia de lo que cambia con esta regla (calculado en el boceto): en casi
  todos solo `dim` y `tick`; `claude-code` además `accent` y `ok`; `noir` y
  `grimoire` `crit`; `sensei` `strong` y `ref`; `retro86` casi todo (su paleta
  original tiene muy poco contraste).
- Algoritmo de referencia: pasos de 0,5 % de luminosidad HSL. Si ni siquiera
  la luminosidad extrema alcanza el mínimo, el color pasa a blanco (fondo
  oscuro) o negro (fondo claro).

### 6.1 Valores finales (autoritativos)

Estos son los valores **ya ajustados** que se usan en el código, calculados
con el algoritmo de arriba sobre los valores base (5.1) y las paletas de
claude-skins `efa1dca`. El builder no necesita bajar el repo de skins: si su
cálculo da otro valor, manda esta tabla. `label` es el color del texto de la
etiqueta "Crítico" sobre `crit`.

| Tema | bg | text | dim | strong | accent | ok | ref | crit | tick | label |
|---|---|---|---|---|---|---|---|---|---|---|
| `pr-review` | `#1E2331` | `#D9DCE4` | `#838A9E` | `#F3F4F8` | `#E7B45A` | `#86C9A8` | `#A99DD8` | `#EE7F7A` | `#646D85` | `#000000` |
| `claude-code` | `#F4EFE6` | `#2B2622` | `#746B63` | `#14110E` | `#AC5435` | `#4E7839` | `#3E6A8A` | `#B3261E` | `#9E8763` | `#FFFFFF` |
| `codex` | `#000000` | `#D4D4D4` | `#7A7A7A` | `#FFFFFF` | `#3CC4B4` | `#5FB39A` | `#8FB7B2` | `#E5534B` | `#5A5A5A` | `#000000` |
| `grok` | `#0B0B0B` | `#D9D9D9` | `#808080` | `#FFFFFF` | `#D4A537` | `#9CC48A` | `#7FA7E8` | `#E06C6C` | `#5E5E5E` | `#000000` |
| `gemini` | `#1E1E2E` | `#CDD6F4` | `#82879E` | `#FFFFFF` | `#B58BD6` | `#94E2D5` | `#89B4FA` | `#F38BA8` | `#65678D` | `#000000` |
| `nebula` | `#0E0520` | `#E0D6F0` | `#A946F1` | `#E0D6F0` | `#FF6B35` | `#39FF14` | `#B388FF` | `#FF3D3D` | `#7A2BDC` | `#000000` |
| `noir` | `#0D0D0D` | `#F5E6C8` | `#7A7A7A` | `#F5E6C8` | `#D4A857` | `#7A9E7E` | `#8AA6A3` | `#BC5E5E` | `#5F5F5F` | `#000000` |
| `netrunner` | `#0A0A0A` | `#80CBC4` | `#008A73` | `#80CBC4` | `#00E5FF` | `#69F0AE` | `#00E5FF` | `#FF1744` | `#5D5D5D` | `#000000` |
| `grimoire` | `#1A120B` | `#D4C4A8` | `#A27353` | `#D4C4A8` | `#C9A84C` | `#6B8E23` | `#5F8A8B` | `#C26337` | `#825A37` | `#000000` |
| `mission-control` | `#0B1120` | `#FFB000` | `#557DBE` | `#E8E8E8` | `#FFB000` | `#33FF33` | `#88AACC` | `#FF3333` | `#3060A9` | `#000000` |
| `mythos` | `#0D1B3D` | `#E8EAF6` | `#727FC9` | `#E8EAF6` | `#FFD700` | `#66BB6A` | `#4DD0E1` | `#EF5350` | `#4D59DA` | `#000000` |
| `retro86` | `#40318D` | `#ACA6E5` | `#ADA4E4` | `#ADADAD` | `#ACA6E5` | `#79BC79` | `#70B8B8` | `#E19A9A` | `#8D80DA` | `#000000` |
| `brutalist` | `#000000` | `#FFFFFF` | `#757575` | `#FFFFFF` | `#FFFFFF` | `#FFFFFF` | `#AAAAAA` | `#FFFFFF` | `#5B5B5B` | `#000000` |
| `sensei` | `#F5F2EB` | `#2C2C2C` | `#766D65` | `#7E6B3F` | `#C41E3A` | `#5B7553` | `#587372` | `#C41E3A` | `#9B8A60` | `#FFFFFF` |

Degradado de `gemini` ya ajustado: `#4796E4` → `#867CCF` → `#C46B82`.

Notas: en `retro86` el atenuado queda casi igual al texto (es el precio del
contraste en la paleta del C64); en `brutalist` acento, ok y crit son blanco,
como en el original monocromo — ahí la selección se distingue por el marcador
y la negrita.

## 7. Interfaz

- La tecla `m` de la lista pasa a abrir **Configuración**, con dos opciones:
  **Modelos** (la pantalla que ya existe) y **Tema**. El comando
  `pr-review config models` sigue abriendo modelos directo. *(Decisión del
  asistente; el usuario pidió "desde la pantalla de configuración".)*
- Pantalla **Tema**: lista de los 14 temas (nombre y descripción) y debajo una
  vista previa de la lista de PRs con datos de ejemplo, pintada con el tema
  resaltado. La vista previa cambia al moverse con ↑↓. `enter` guarda el tema
  y lo aplica a toda la interfaz; `esc` vuelve sin cambiar (se restaura el
  tema que estaba). Boceto aprobado:
  `.superpowers/brainstorm/131062-1794535888/content/themes.html` (selector)
  y `themes-v2.html` (los 14 temas con el contraste corregido).
- Con un tema activo, **toda la pantalla** usa su fondo: cada celda visible,
  incluidas las líneas vacías y el espacio a la derecha del contenido.
- La configuración inicial no agrega un paso de tema: arranca con
  `pr-review`. *(Decisión del asistente.)*

### 7.1 Detalles de la interfaz (decisiones del asistente tras la revisión del documento)

- **Vista previa:** se dibuja con la pantalla de lista real, con 3 PRs de
  ejemplo fijos (los de los bocetos: #477 grande, #88, #1290), pintada con el
  tema resaltado **sin cambiar el tema activo** hasta `enter`. Así la vista
  previa nunca se aparta de la lista verdadera.
- **Terminales bajas:** la lista de 14 temas se desplaza si no entra; la vista
  previa se muestra solo si sobra altura (si no, una línea "agrandá la
  terminal para ver la vista previa").
- **Navegación:** `m` en la lista → menú Configuración (Modelos, Tema);
  `esc` en Tema vuelve al menú; `esc` en el menú vuelve a la lista. La ayuda
  de teclas de la lista dice `m configuración`. No hay comando
  `config tema`.
- **Rojo:** `crit` se usa solo como fondo de la etiqueta "Crítico", con el
  color `label` de 6.1 encima. Errores y agentes fallidos siguen en `accent`
  (como hoy), nunca en rojo.
- **Degradado de gemini:** en el nombre "pr-review" va carácter por carácter;
  en la barra de tamaño, celda por celda a lo largo de las 8 celdas hasta la
  marca; el marcador `▌` usa el color del medio del degradado. Los colores
  intermedios del degradado también tienen que llegar a 4,5:1 en el nombre y
  a 3:1 en la barra (el test los muestrea).
- **Fondo completo:** se considera cumplido cuando, en cada pantalla, cada
  línea mide exactamente el ancho de la terminal, la pantalla ocupa toda la
  altura y ningún tramo vuelve al fondo por defecto de la terminal (los
  estilos de cada token llevan el fondo del tema, y el relleno también).
  Al salir, la terminal recupera sus colores (pantalla alternativa).
- Los tests existentes siguen pasando con el tema por defecto `pr-review`
  (sus valores 6.1 reemplazan a los de la spec 7.4).

## 8. Configuración

- Nuevo campo `tema` en `~/.config/pr-review/config.yaml` con el nombre del
  tema. Si falta, se usa `pr-review`.
- El campo se guarda tal cual (texto) y `config.Load` no lo valida: un nombre
  desconocido no impide arrancar. La interfaz usa `pr-review`, la lista
  muestra una nota al abrir ("No conozco el tema X; uso pr-review.") y la
  pantalla Tema preselecciona `pr-review`. Guardar desde Modelos conserva el
  texto que había; guardar desde Tema escribe el tema elegido. *(Decisión del
  asistente.)*

## 9. A criterio de quien lo construya

- Cómo pasan los estilos del tema a las pantallas (hoy son estilos de paquete
  fijos en `internal/tui/theme`).
- Cómo se pinta el fondo completo con Bubble Tea v2 / Lip Gloss v2.
- La forma de interpolar el degradado.
- Si los valores ajustados se generan con un pequeño programa y se pegan en el
  código, o se escriben a mano; en ambos casos con el test de contraste.

## 10. Decisiones del asistente (para revisar)

- Los valores base de `claude-code`, `codex`, `grok` y `gemini` (tabla 5.1).
- La correspondencia skin → tokens (5.2) y las descripciones en español.
- Dónde va el degradado de gemini.
- `m` abre un menú de Configuración con Modelos y Tema.
- Sin paso de tema en la configuración inicial; `pr-review` por defecto.
- Nombre de tema desconocido → `pr-review` con nota.
- Los temas no afectan `review.md`, la salida de `review` ni las notificaciones.
- Los valores finales de 6.1 (incluido el ajuste del propio `pr-review`, cuyo
  atenuado y separadores no llegaban al mínimo).
- Todo lo de 7.1: vista previa con la lista real y datos fijos, terminales
  bajas, navegación del menú, rojo solo en la etiqueta, reparto del degradado,
  criterio de "fondo completo".
