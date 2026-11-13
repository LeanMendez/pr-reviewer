# pr-review, parte 3: temas de color — Plan de implementación

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 14 temas de color con fondo propio y contraste accesible, elegibles desde Configuración → Tema con vista previa, guardados en la config.

**Architecture:** `internal/tui/theme` pasa de estilos fijos a un tema activo intercambiable. Las variables de estilo del paquete (`theme.Text`, `theme.Dim`, …) se reasignan con `theme.Use`, así los ~250 usos actuales no cambian. `theme.With` renderiza temporalmente con otro tema, para la vista previa, y `theme.Fill` pinta el fondo completo de la pantalla. El router aplica el tema al arrancar y al elegirlo, y pasa toda la vista por `Fill`. Toda la interfaz se renderiza en la goroutine de Bubble Tea, así que la reasignación es segura (se documenta como invariante).

**Tech Stack:** el mismo de la parte 2 (charm.land/bubbletea/v2 v2.1.1, lipgloss/v2 v2.1.0, github.com/charmbracelet/x/ansi). Sin dependencias nuevas.

**Spec:** `docs/superpowers/specs/2026-11-13-pr-review-temas-design.md`. La tabla 6.1 es autoritativa para todos los valores de color.

## Global Constraints

- Se mantiene todo lo de las partes 1 y 2: módulo, Go 1.27, textos en español, Conventional Commits sin atribución de IA, tests sin red, sin `prb` y sin `claude`.
- Los 14 nombres de tema, en este orden: `pr-review` (por defecto), `claude-code`, `codex`, `grok`, `gemini`, `nebula`, `noir`, `netrunner`, `grimoire`, `mission-control`, `mythos`, `retro86`, `brutalist`, `sensei`.
- Los colores son exactamente los de la tabla 6.1 de la spec, incluido `label`. El degradado de `gemini` es `#4796E4 → #867CCF → #C46B82`.
- Contraste (WCAG 2, luminancia relativa) contra `bg`:
  - 4,5:1 para `text`, `dim`, `strong`, `accent`, `ok`, `ref` y `crit`;
  - 3:1 para `tick`;
  - el texto `label` contra el fondo `crit`, 4,5:1;
  - el degradado muestreado en 9 puntos: 4,5:1 en el nombre y 3:1 en la barra.
- Los fondos nunca se ajustan.
- El rojo (`crit`) solo como fondo de la etiqueta "Crítico". Errores y agentes fallidos siguen en `accent`.
- Los temas no cambian `review.md`, la salida del comando `review` ni las notificaciones.
- Avisos de terceros: el aviso de copyright y el texto MIT de `basicScandal/claude-skins` (commit `efa1dca`) van en `THIRD_PARTY_NOTICES.md` y como comentario sobre las paletas que salen de ahí.

## Review Focus

1. **Un `tema` desconocido o vacío en la config** no impide arrancar: se usa `pr-review`, la lista muestra `No conozco el tema X; uso pr-review.` y guardar desde Modelos conserva el texto original. Lo pinean tests en la Task 3.
2. **Temas claros (`claude-code`, `sensei`):** ninguna celda queda con el fondo de la terminal (entrada de texto, filtros, avisos superpuestos, líneas vacías, el borde derecho). Lo pinea un test en la Task 3 que recorre las celdas de la vista del router.
3. **Cambiar de tema con una revisión corriendo en segundo plano:** el progreso, el reporte y la lista se ven con el tema nuevo en el próximo render. Ningún modelo puede guardar estilos calculados al construirse. Lo pinea un test en la Task 5.
4. **Redimensionar la terminal en el selector de temas,** incluso a menos altura que la lista de 14: nada se sale del ancho ni del alto, y la vista previa se reemplaza por el aviso. Lo pinea un test en la Task 4.
5. **El comando `review` y `review.md` no cambian.** Lo pinea un test en la Task 2 que compara la salida de `report.Markdown` y del comando con los golden existentes.

---

### Task 1: Los 14 temas y la regla de contraste

**Files:**
- Create: `internal/tui/theme/themes.go`, `internal/tui/theme/contrast.go`, `THIRD_PARTY_NOTICES.md`
- Test: `internal/tui/theme/themes_test.go`

**Interfaces:**
- Produces:
  - `type Theme struct{ Name, Desc string; BG, Text, Dim, Strong, Accent, OK, Ref, Crit, Tick, Label string; Gradient []string }`, con colores `#RRGGBB`.
  - `func All() []Theme`, que devuelve los 14 en el orden de las Global Constraints.
  - `func ByName(name string) (Theme, bool)`.
  - `const DefaultName = "pr-review"`.
  - `func Contrast(a, b string) float64` (WCAG 2).
  - `func Lerp(stops []string, x float64) string`, interpolación lineal en RGB, con x en [0,1].

- [ ] **Step 1: Write the failing tests**
  - `TestAllThemesOrderAndValues`: `All()` devuelve los 14 nombres en orden y cada tema tiene los valores de la tabla 6.1 de la spec, celda por celda, más `Desc` (5.1 para los propios; 5.2 para los de claude-skins). Gemini tiene `Gradient` `["#4796E4","#867CCF","#C46B82"]`; los demás, `nil`.
  - `TestThemesAreAccessible`: para cada tema se cumplen los mínimos de las Global Constraints. El degradado se muestrea con `Lerp` en x = 0, 1/8, …, 1.
  - `TestContrastKnownValues`: `Contrast("#000000","#FFFFFF")` da 21 (±0,01), `Contrast("#777777","#FFFFFF")` da 4,48 (±0,01) y es simétrico.
  - `TestByName`: `ByName("gemini")` da ok; `ByName("nope")` da false; `DefaultName` existe.
- [ ] **Step 2:** Run `go test ./internal/tui/theme/`. Expected: FAIL.
- [ ] **Step 3:** Implementar. Copiar los valores de la tabla 6.1 tal cual. Agregar el comentario de licencia sobre los 9 temas de claude-skins y `THIRD_PARTY_NOTICES.md` con el texto MIT (Copyright (c) 2026 basicScandal) y la URL del repo.
- [ ] **Step 4:** Run. Expected: PASS.
- [ ] **Step 5:** Commit `feat(theme): add the 14 accessible color themes`.

### Task 2: Tema activo intercambiable, fondo completo y degradado

**Files:**
- Modify: `internal/tui/theme/theme.go`, `internal/tui/components/sizebar.go`, `internal/tui/screens/list.go` (encabezado y marcador)
- Create: `internal/tui/theme/fill.go`
- Test: `internal/tui/theme/theme_test.go`, `internal/tui/theme/fill_test.go`, `internal/tui/components/components_test.go`, `internal/report/report_test.go` (Review Focus 5)

**Interfaces:**
- Consumes: Task 1.
- Produces:
  - Las variables existentes `theme.Text`, `Dim`, `OK`, `Accent`, `Ref`, `Critical`, `Tick` y `Strong` siguen siendo `lipgloss.Style`, pero ahora todas llevan `Background(bg)` del tema activo. `Strong` mantiene la negrita.
  - `func Use(t Theme)` reasigna las variables.
  - `func Current() Theme`.
  - `func With(t Theme, fn func() string) string`: usa `t` durante `fn` y restaura el tema anterior, aunque `fn` haga panic.
  - `func Brand(s string) string`: el texto en `Strong`, o carácter por carácter con el degradado si el tema tiene uno.
  - `func Marker() string`: `▌` en `Accent`, o con el color del medio del degradado.
  - `func Fill(content string, width, height int) string`: cada línea medida en celdas visibles queda exactamente de `width` (rellena con espacios del fondo o recorta), se agregan líneas de fondo hasta `height` y, tras cada reset SGR dentro de una línea, se vuelve a aplicar el fondo del tema.
  - `SeverityLabel(critico)` pasa a ` Crítico ` con texto `Label` sobre fondo `Crit` (sin `Reverse`).
  - `components.SizeBar`: con degradado, las celdas llenas hasta la marca toman `Lerp(gradient, i/7)`.
  - Al cargar el paquete se llama `Use(ByName(DefaultName))`.

- [ ] **Step 1: Write the failing tests**
  - `TestUseSwapsStyles`: después de `Use(sensei)`, `Text.Render("x")` contiene la secuencia de fondo de `#F5F2EB` y de letra de `#2C2C2C`. `Use(default)` la restaura.
  - `TestWithRestores`: `With(gemini, fn)` ve `Current().Name=="gemini"` dentro de `fn` y `pr-review` después, también si `fn` hace panic (recuperado en el test).
  - `TestBrandGradient`: con gemini, `Brand("pr-review")` tiene 9 colores de letra distintos; con codex, un solo color, el de `Strong`.
  - `TestFillEveryCellHasBackground`: con un contenido que mezcla texto con estilo, texto sin estilo, resets en el medio de la línea y líneas cortas, `Fill(c, 40, 6)` da 6 líneas de 40 celdas. Al parsear con `github.com/charmbracelet/x/ansi`, cada celda tiene el fondo del tema activo.
  - `TestSeverityLabelCritico`: con codex, la etiqueta tiene fondo `#E5534B` y letra `#000000`.
  - `TestSizeBarGradient`: con gemini, las 8 celdas de 900 líneas tienen colores crecientes del degradado; con pr-review, un solo color.
  - `TestMarkdownUnaffectedByTheme` (Review Focus 5): con `Use(sensei)`, `report.Markdown` sigue igual a los golden y sin secuencias ANSI.
- [ ] **Step 2:** Run `go test ./internal/tui/... ./internal/report/`. Expected: los tests nuevos FALLAN.
- [ ] **Step 3:** Implementar. En `list.go`, el encabezado usa `theme.Brand("pr-review")` y el marcador de selección usa `theme.Marker()`. Actualizar los tests existentes que tenían fijos los valores RGB del tema viejo: el `pr-review` de 6.1 cambia `dim` y `tick`, y la etiqueta Crítico ya no usa `Reverse`.
- [ ] **Step 4:** Run `make test lint`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(theme): switchable active theme, full background and gradient`.

### Task 3: Tema en la configuración y aplicado a toda la interfaz

**Files:**
- Modify: `internal/config/config.go`, `internal/tui/app.go`, `internal/tui/router.go`
- Test: `internal/config/config_test.go`, `internal/tui/app_test.go`

**Interfaces:**
- Consumes: Tasks 1 y 2.
- Produces:
  - `config.Config.Tema string` con YAML `tema,omitempty`, sin validar en `Load`.
  - En `internal/tui`: `func themeFor(cfg config.Config) (theme.Theme, string)`, que devuelve el tema y la nota (vacía si el nombre se conoce o está vacío).
  - `App.View()` pasa todo el contenido final por `theme.Fill(content, width, height)`, incluidos los avisos de salida y de cancelación.

- [ ] **Step 1: Write the failing tests**
  - `TestConfigTemaRoundTrip`: `Save` y `Load` conservan `tema: noir`. Sin el campo, `Tema` queda en `""`. Un nombre desconocido carga sin error.
  - `TestAppAppliesConfiguredTheme`: `NewApp` con `Cfg.Tema="claude-code"` deja `theme.Current().Name=="claude-code"`.
  - `TestAppUnknownThemeNote` (Review Focus 1): con `Tema="vaporwave"` se usa `pr-review` y la lista muestra `No conozco el tema vaporwave; uso pr-review.`. Guardar desde la pantalla de Modelos escribe de nuevo `tema: vaporwave`.
  - `TestAppViewFillsWholeScreen` (Review Focus 2): con `claude-code` y con `sensei`, a 100x30, la vista del router en la lista, en el aviso de salida y en la configuración inicial (con la entrada de texto activa) tiene cada celda con el fondo del tema.
- [ ] **Step 2:** Run `go test ./internal/config/ ./internal/tui/`. Expected: FAIL.
- [ ] **Step 3:** Implementar. `NewApp` llama `theme.Use` con `themeFor(d.Cfg)` y, si hay nota, la deja en la lista con `WithNote`.
- [ ] **Step 4:** Run `make test lint`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(tui): apply the configured theme to the whole interface`.

### Task 4: Menú de configuración y selector de temas con vista previa

**Files:**
- Create: `internal/tui/screens/settings.go`, `internal/tui/screens/themes.go`
- Test: `internal/tui/screens/settings_test.go`, `internal/tui/screens/themes_test.go`

**Interfaces:**
- Consumes: Tasks 1 y 2, `ListModel`/`NewList`/`RowsLoadedMsg`/`listdata.Row` (lista), `BackMsg` (mode.go) y los helpers clean/fit.
- Produces:
  - `type SettingsModel struct`; `func NewSettings() SettingsModel`, con las opciones `Modelos` y `Tema`. Mensajes `OpenModelsMsg{}` (ya existe en list.go: reutilizarlo) y `OpenThemesMsg{}`; `esc` emite `BackMsg`.
  - `type ThemesModel struct`; `func NewThemes(current string) ThemesModel`, que preselecciona `current` o `pr-review` si no lo conoce. Mensaje `ThemeChosenMsg{ Name string }`; `esc` emite `BackMsg`.
  - `func sampleRows(now time.Time) []listdata.Row`: los 3 PRs de los bocetos (#477 de 900 líneas, #88 de 393, #1290 de 130), con estado listo.

- [ ] **Step 1: Write the failing tests**
  - `TestSettingsMenu`: muestra `Modelos` y `Tema`; `enter` en cada uno emite `OpenModelsMsg` u `OpenThemesMsg`; `esc` emite `BackMsg`.
  - `TestThemesListAndPreview`: a 100x40 lista los 14 con su descripción y la vista previa contiene `#477` y `Migrar facturas`. Al bajar a `gemini`, la vista previa tiene el fondo `#1E1E2E` y `theme.Current()` sigue siendo el tema activo previo.
  - `TestThemesEnterEsc`: `enter` emite `ThemeChosenMsg{"gemini"}`; `esc` emite `BackMsg`. En ningún caso cambia `theme.Current()`.
  - `TestThemesUnknownPreselectsDefault`: `NewThemes("vaporwave")` arranca con el cursor en `pr-review`.
  - `TestThemesShortTerminal` (Review Focus 4): a 80x12 ninguna línea pasa de 80 celdas, hay 12 líneas como máximo, la lista se desplaza para mostrar el tema seleccionado y aparece `agrandá la terminal para ver la vista previa`.
- [ ] **Step 2:** Run `go test ./internal/tui/screens/`. Expected: FAIL.
- [ ] **Step 3:** Implementar. La vista previa es `theme.With(t, func() string { return NewList(...) con RowsLoadedMsg{Ready: sampleRows(now)} renderizado a (width, previewHeight) })`. El selector se renderiza con el tema activo; solo la vista previa usa el resaltado.
- [ ] **Step 4:** Run `make test lint`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(tui): add settings menu and theme picker with live preview`.

### Task 5: Navegación del router y guardado del tema

**Files:**
- Modify: `internal/tui/router.go`, `internal/tui/app.go`, `internal/tui/screens/list.go` (texto de la ayuda de teclas)
- Test: `internal/tui/app_test.go`, `internal/tui/screens/list_test.go`

**Interfaces:**
- Consumes: Tasks 3 y 4, la función de guardado de config que ya usa el router para Modelos.
- Produces: estados del router para Configuración y Tema.

- [ ] **Step 1: Write the failing tests**
  - `TestAppSettingsNavigation`:
    - `m` en la lista abre Configuración;
    - `enter` en Modelos abre la pantalla de modelos y `esc` ahí vuelve a Configuración;
    - `enter` en Tema abre el selector y `esc` vuelve a Configuración;
    - `esc` en Configuración vuelve a la lista;
    - `pr-review config models` sigue abriendo Modelos directo y `esc` sale como antes.
  - `TestAppThemeChosen`: `ThemeChosenMsg{"noir"}` guarda la config con `tema: noir` (y el resto de los campos intactos), deja `theme.Current().Name=="noir"` y vuelve a Configuración. Si el guardado falla, muestra `No se pudo guardar: …` y el tema activo no cambia.
  - `TestAppThemeSwitchWithBackgroundReview` (Review Focus 3): con una revisión corriendo en segundo plano, se elige `sensei` y al volver a la pantalla de progreso su vista tiene el fondo `#F5F2EB`.
  - `TestListKeyHelpSaysConfiguracion`: la ayuda de teclas de la lista dice `m configuración`, donde antes decía `m modelos`.
- [ ] **Step 2:** Run `go test ./internal/tui/...`. Expected: FAIL.
- [ ] **Step 3:** Implementar.
- [ ] **Step 4:** Run `go test -race ./...` y `make test lint`. Expected: PASS.
- [ ] **Step 5:** Verificación manual en una terminal real, sin prb, para ver los colores:
  1. `go run ./cmd/pr-review config models`, solo para ver el fondo;
  2. después, en un repo inexistente, `pr-review x`;
  3. `m` → Tema;
  4. recorrer los 14 temas y elegir `claude-code`;
  5. salir y volver a entrar.

  Expected: el tema persiste y toda la pantalla tiene el fondo crema.
- [ ] **Step 6:** Commit `feat(tui): navigate settings and save the chosen theme`.
