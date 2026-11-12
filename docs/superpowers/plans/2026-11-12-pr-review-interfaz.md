# pr-review, parte 2: interfaz — Plan de implementación

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** La interfaz de terminal de `pr-review`: configuración inicial, lista de PRs de un repo, elegir el modo, progreso, reporte y configuración de modelos, encima del motor de la parte 1.

**Architecture:** Cada pantalla es un modelo de Bubble Tea independiente. Expone `render(width, height int) string` para los tests y se comunica con el router solo por mensajes (`OpenModeMsg`, `StartReviewMsg` y los demás). El router (`internal/tui/app.go`) apila pantallas, mantiene las revisiones que corren en segundo plano y conecta cada pantalla con el motor (`pipeline.Run`, `prb.Client`, `config`). Los procesos externos (notificaciones, portapapeles, navegador) pasan por `execx.Exec` para poder simularlos en los tests.

**Tech Stack:** charm.land/bubbletea/v2 v2.1.1, charm.land/lipgloss/v2 v2.1.0, charm.land/bubbles/v2 v2.2.1, github.com/charmbracelet/x/ansi (para sacar los códigos ANSI en los tests), más el motor de la parte 1.

**Spec:** `docs/superpowers/specs/2026-11-12-pr-review-design.md`, secciones 7, 8 y 9. Los bocetos aprobados están en `.superpowers/brainstorm/*/content/`: `pr-list-layout-v3.html`, `progress-v3.html`, `report-v2.html` y `models-v2.html`. **Requisito:** la parte 1 (`2026-11-12-pr-review-motor.md`) tiene que estar completa.

## Global Constraints

- Todo lo de la parte 1 sigue vigente (módulo, idioma, commits sin atribución de IA, tests sin red).
- Tokens de color (spec 7.4), definidos una sola vez en `internal/tui/theme`:

  | Token | Valor |
  |---|---|
  | `Text` | `#D9DCE4` |
  | `Dim` | `#7D859A` |
  | `OK` | `#86C9A8` |
  | `Accent` | `#E7B45A` |
  | `Ref` | `#A99DD8` |
  | `Critical` | `#EE7F7A` |
  | `Tick` | `#4E5568` |
  | `Strong` | `#F3F4F8` (negrita) |

- El rojo (`Critical`) se usa **solo** para la etiqueta de hallazgos críticos. Un PR grande va en ámbar.
- Teclas (spec 7.2):
  - Lista: `↑↓`, `enter`, `/`, `t`, `r`, `m` y `q`.
  - Progreso: `s`, `l` y `c`.
  - Reporte: `↑↓`, `enter`, `c`, `l`, `o` y `q`.
  - Configuración de modelos: `↑↓`, `←→`, `e`, `p`, `g` y `esc`.
- Textos en español, en minúscula inicial donde el boceto lo muestra así, sin etiquetas en MAYÚSCULAS.
- Las pantallas no llaman a procesos ni a la red directamente: lo hacen a través de interfaces o comandos (`tea.Cmd`) que el router les inyecta.
- La API exacta de Bubble Tea v2 (tipo de los mensajes de tecla, firma de `View`) es la de la versión fijada. Los tests no dependen de ella: usan `internal/tui/tuitest.Key(name string) tea.Msg` y `render`.

## Review Focus

1. **Terminal de menos de 100 columnas:** la lista sigue usable sin cortes de línea. Por debajo de 100 se oculta la columna autor y por debajo de 80, la barra de tamaño. Lo pinea un test en la Task 3.
2. **Una revisión termina mientras el usuario está en otra pantalla:** llega la notificación, la fila del PR muestra "revisado hace un momento" y con `enter` sobre ese PR se ofrece abrir el reporte. Lo pinea un test en la Task 9.
3. **Salir con revisiones en curso:** se pide confirmación. Si se confirma, se cancelan los contextos y se esperan las limpiezas de worktrees. Lo pinea un test en la Task 9.
4. **prb sin sesión (exit 4) o sin red al cargar la lista:** se muestra una pantalla de error con la solución (`Corré prb auth login.`) y la opción de reintentar con `r`, sin que el programa se caiga. Lo pinea un test en la Task 3.
5. **No hay herramienta de portapapeles:** `c` muestra `No encontré wl-copy, xclip ni xsel para copiar.` en la barra de estado. Lo pinea un test en la Task 6.

---

### Task 1: Tema y componentes compartidos

**Files:**
- Create: `internal/tui/theme/theme.go`, `internal/tui/components/sizebar.go`, `internal/tui/components/keys.go`, `internal/tui/tuitest/tuitest.go`
- Test: `internal/tui/components/components_test.go`

**Interfaces:**
- Produces:
  - En `theme`: los estilos `Text`, `Dim`, `OK`, `Accent`, `Ref`, `Critical`, `Tick` y `Strong` de lipgloss, y `func SeverityLabel(s finding.Severity) string` (`Crítico` con fondo `Critical`, `Alto` en `Accent`, `Medio` en `Strong`, `Bajo` en `Dim`).
  - `func components.SizeBar(lines, threshold int) string`: 8 celdas hasta la marca y 4 de desborde. Cada celda vale `threshold/8` líneas, la última celda puede ser parcial (`▏▎▍▌▋▊▉`), las celdas vacías antes de la marca son `·` y la marca es `│`. El desborde va en `Accent` y termina en `»` si pasa de 4 celdas. El ancho visible es siempre 14.
  - `type Key struct{ Keys, Desc string }`; `func components.KeyHelp(ks []Key) string`.
  - `func tuitest.Key(name string) tea.Msg`, con `name` en `up`, `down`, `left`, `right`, `enter`, `esc` o una letra; `func tuitest.Plain(s string) string`, que saca el ANSI.

- [ ] **Step 1:** Run `go get charm.land/bubbletea/v2@v2.1.1 charm.land/lipgloss/v2@v2.1.0 charm.land/bubbles/v2@v2.2.1 github.com/charmbracelet/x/ansi`.
- [ ] **Step 2: Write the failing tests**
  - `TestSizeBar`, con `threshold=400` y comparando `Plain(...)`: 900 → `████████│████»`, 393 → `███████▉│     `, 252 → `█████···│     `, 43 → `▉·······│     `. El ancho visible es 14 en todos los casos.
  - `TestKeyHelp`: `KeyHelp([{"↑↓","moverse"},{"q","salir"}])` en texto plano da `↑↓ moverse    q salir`.
- [ ] **Step 3:** Run `go test ./internal/tui/...`. Expected: FAIL.
- [ ] **Step 4:** Implementar.
- [ ] **Step 5:** Run. Expected: PASS.
- [ ] **Step 6:** Commit `feat(tui): add theme, size bar and key help`.

### Task 2: Datos de la lista

**Files:**
- Create: `internal/tui/listdata/loader.go`
- Modify: `internal/report/store.go` (agregar `LastReview`)
- Test: `internal/tui/listdata/loader_test.go`, `internal/report/report_test.go`

**Interfaces:**
- Consumes: `prb.Client`, `eligibility.Split`, `prechecks.Run` (para el modo sugerido) y `pipeline.Suggest`.
- Produces:
  - `type Row struct{ Ctx prb.PRContext; Lines int; Suggested pipeline.Mode; Reason eligibility.Reason; LastReview time.Time; Reviewed bool }`.
  - `type Loader struct{ PRB prb.Client; Me prb.Identity; ReportsRoot string; Cfg config.Config; Parallel int }`.
  - `func (l Loader) Load(ctx, repo string) (ready []Row, hidden []Row, err error)`: `ListOpen`, después `Context` en paralelo (hasta `Parallel`, por defecto 6) y por último `Split`. `Lines` y `Suggested` salen de `pipeline.Suggest(prechecks.Summarize(ctx.Files.Files, cfg))`, sin bajar el diff.
  - `func report.LastReview(root, repo string, pr int) (time.Time, bool)`: busca la carpeta `pr-<N>-*` más reciente.
  - `func report.LoadLatest(root, repo string, pr int) (pipeline.Result, string, error)`: lee el `findings.json` de esa carpeta y devuelve también la ruta de su `review.md`. Lo usa el router para `OpenReportMsg`.

- [ ] **Step 1: Write the failing tests**
  - `TestLoadSplitsReadyAndHidden`, con un `execx.Fake` que responde `list` y tres `context`: devuelve dos listos y uno oculto con su `Reason`. Los listos se ordenan por `UpdatedOn` descendente.
  - `TestLoadMarksReviewed`: con una carpeta `pr-477-20261112-0412` presente, la fila del 477 tiene `Reviewed=true` y `LastReview` en esa fecha.
  - `TestLoadPropagatesPRBError`: un `*prb.Error` con `no_credentials` vuelve tal cual.
  - `TestLastReviewPicksNewest`.
  - `TestLoadLatestRoundTrip`: lo que escribió `report.Write` se vuelve a leer igual.
- [ ] **Step 2:** Run. Expected: FAIL.
- [ ] **Step 3:** Implementar con `errgroup` y `SetLimit`.
- [ ] **Step 4:** Run. Expected: PASS.
- [ ] **Step 5:** Commit `feat(tui): load and classify PR rows`.

### Task 3: Pantalla de lista

**Files:**
- Create: `internal/tui/screens/list.go`
- Test: `internal/tui/screens/list_test.go`

**Interfaces:**
- Consumes: `listdata.Row` (Task 2) y los componentes de la Task 1.
- Produces:
  - `type ListModel struct`; `func NewList(repo string, load func() tea.Cmd) ListModel`.
  - `type RowsLoadedMsg struct{ Ready, Hidden []listdata.Row; Err error }`.
  - Mensajes de salida: `OpenModeMsg{ Row listdata.Row }`, `OpenReportMsg{ Repo string; PR int }`, `OpenModelsMsg{}` y `QuitMsg{}`.
  - `func (m ListModel) render(width, height int) string`.

- [ ] **Step 1: Write the failing tests** (las filas de ejemplo son las del boceto `pr-list-layout-v3.html`)
  - `TestListRender100`: el encabezado contiene `pr-review`, el nombre del repo y `5 listos para revisar`. Hay columnas `#`, `título`, `autor`, `tamaño (líneas)` y `hace`, sin columna `repo`. La fila seleccionada empieza con `▌`. Aparece la línea `HiddenLine` y abajo el detalle del seleccionado:
    - `#477  <título>` y la referencia de ClickUp;
    - `CI pasó (3 de 3)` (o `sin CI` cuando `Overall=="none"`), `tareas sin resolver 0` y `revisores 1 de 2 aprobó, nadie pidió cambios`;
    - `pr-review nunca lo revisó` (o `pr-review lo revisó hace 2 h`);
    - `23 archivos, +690 −210.` y, si pasa el umbral, `Es grande: 900 líneas, más del doble de las 400 recomendadas.`;
    - `Modo sugerido: exhaustivo.`

    Al pie va el `KeyHelp` de la lista.
  - `TestListNarrow`: a 99 columnas no aparece `autor`; a 79 no aparece la barra. Ninguna línea pasa del ancho.
  - `TestListKeys`: `down` mueve la selección; `enter` emite `OpenModeMsg` con la fila; `t` muestra también los ocultos en `Dim`, con su motivo; `/` + texto filtra por título o autor; `m` emite `OpenModelsMsg`; `q` emite `QuitMsg`; `r` vuelve a llamar a `load`.
  - `TestListEnterOnReviewed`: sobre una fila con `Reviewed`, `enter` emite `OpenModeMsg` y el detalle muestra la ayuda `o abrir el último reporte`. La tecla `o` emite `OpenReportMsg`.
  - `TestListError`: `RowsLoadedMsg{Err: &prb.Error{Code:"no_credentials"}}` muestra `No hay sesión de Bitbucket. Corré prb auth login y apretá r.`. Cualquier otro error muestra su mensaje y `apretá r para reintentar`.
  - `TestListEmpty`: sin listos muestra `No hay PRs listos para revisar en <repo>.` más la línea de ocultos.
- [ ] **Step 2:** Run `go test ./internal/tui/screens/ -run List`. Expected: FAIL.
- [ ] **Step 3:** Implementar `ListModel` (`Init`, `Update`, `View` con la API de Bubble Tea v2; `View` delega en `render`).
- [ ] **Step 4:** Run. Expected: PASS.
- [ ] **Step 5:** Commit `feat(tui): add PR list screen`.

### Task 4: Elegir el modo

**Files:**
- Create: `internal/tui/screens/mode.go`
- Test: `internal/tui/screens/mode_test.go`

**Interfaces:**
- Consumes: `pipeline.Mode`, `pipeline.Stages`, `pipeline.EstimateAgents` y `config.Config`.
- Produces: `type ModeModel struct`; `func NewMode(row listdata.Row, cfg config.Config) ModeModel`; mensajes `StartReviewMsg{ Row listdata.Row; Mode pipeline.Mode }` y `BackMsg{}`. `BackMsg` se define acá y lo reutilizan las Tasks 5, 6 y 7.

- [ ] **Step 1: Write the failing tests**
  - `TestModeRender`: muestra los tres modos con el sugerido preseleccionado y marcado como `sugerido`. Cada uno lleva una línea con sus etapas y `unos N agentes` (con `EstimateAgents`), más los modelos de sus roles principales según `cfg`; por ejemplo, para judgment: `jueces: opus y sonnet`.
  - `TestModeKeys`: `down` y `enter` emiten `StartReviewMsg` con el modo elegido; `esc` emite `BackMsg`.
- [ ] **Step 2:** Run. Expected: FAIL.
- [ ] **Step 3:** Implementar.
- [ ] **Step 4:** Run. Expected: PASS.
- [ ] **Step 5:** Commit `feat(tui): add mode picker`.

### Task 5: Pantalla de progreso

**Files:**
- Create: `internal/tui/screens/progress.go`
- Test: `internal/tui/screens/progress_test.go`

**Interfaces:**
- Consumes: `pipeline.Event`, `pipeline.Stages` y `pipeline.Stage.Name`.
- Produces:
  - `type ProgressModel struct`; `func NewProgress(row listdata.Row, mode pipeline.Mode, events <-chan pipeline.Event, cancel context.CancelFunc) ProgressModel`.
  - `type EventMsg pipeline.Event`; `type ReviewDoneMsg struct{ Result pipeline.Result; Err error }`.
  - Mensajes de salida: `BackgroundMsg{ PR int }` y `OpenResultMsg{ Result pipeline.Result }`.
  - `func waitEvent(ch <-chan pipeline.Event) tea.Cmd`.

- [ ] **Step 1: Write the failing tests** (comparados contra el boceto `progress-v3.html`)
  - `TestProgressStages`: después de `StageDone(1)`, `StageDone(2)`, `StageDone(3)` y `StageStarted(4)`, las etapas 1 a 3 llevan `✓`, la 4 lleva `●` y las 5 a 7 llevan `·`. En modo `ligero` la etapa 6 no aparece.
  - `TestProgressAgents`: `AgentStarted{Agent:"bugs-1", Role:bugs, Model:opus}` + `AgentActivity{Text:"leyendo src/a.go"}` + `AgentDone{Candidates:2}` dejan una fila con `bugs y lógica`, `opus`, `terminó` y `2 candidatos`. Un `AgentFailed` muestra `falló` en `Accent` (no en rojo).
  - `TestProgressHidesFindings`: la pantalla muestra `N candidatos hasta ahora.` y la aclaración `No se muestran hasta que pasen la validación`, sin ningún texto de hallazgo.
  - `TestProgressKeys`: `s` emite `BackgroundMsg`; `c` llama a `cancel` y muestra `Cancelando…`; `l` alterna la vista de detalle con las últimas 5 actividades de cada agente.
  - `TestProgressDone`: `ReviewDoneMsg` exitoso emite `OpenResultMsg`; con error muestra el error y `q volver a la lista`.
- [ ] **Step 2:** Run. Expected: FAIL.
- [ ] **Step 3:** Implementar. El tiempo de cada agente sale de un `tick` cada 1 s.
- [ ] **Step 4:** Run. Expected: PASS.
- [ ] **Step 5:** Commit `feat(tui): add review progress screen`.

### Task 6: Pantalla de reporte

**Files:**
- Create: `internal/tui/screens/report.go`, `internal/tui/sys/sys.go`
- Modify: `internal/report/markdown.go` (agregar `CommentText`)
- Test: `internal/tui/screens/report_test.go`, `internal/tui/sys/sys_test.go`

**Interfaces:**
- Consumes: `pipeline.Result`, `report.VerdictLine` y `theme.SeverityLabel`.
- Produces:
  - `type ReportModel struct`; `func NewReport(r pipeline.Result, s sys.System, savedAt string) ReportModel`.
  - `type System struct{ Exec execx.Exec; LookPath func(string) (string, error) }`.
  - `func (s System) Copy(ctx, text string) error`: usa la primera de `wl-copy`, `xclip -selection clipboard` o `xsel --clipboard --input` que exista; si no hay ninguna, devuelve `ErrNoClipboard`.
  - `func (s System) Open(ctx, url string) error`, con `xdg-open`.
  - `func (s System) Notify(ctx, title, body string) error`, con `notify-send`; si no existe, no hace nada y devuelve nil.
  - `func report.CommentText(f finding.Finding) string`: qué pasa, por qué y la solución en un bloque de código, listo para pegar en Bitbucket.

- [ ] **Step 1: Write the failing tests** (comparados contra el boceto `report-v2.html`)
  - `TestReportRender`:
    - la primera línea tiene el veredicto en `Strong` y la frase; `revisar_a_mano` se muestra como `Revisar a mano.`;
    - sigue la sección `Lo que introduce este PR`, con cada hallazgo: etiqueta de severidad, `archivo:línea` en `Ref`, `que_pasa` en `Strong`, `Por qué pasa:`, la `Solución, según <archivo de regla>` con líneas `-` y `+`, y `Jueces:` si hay votos;
    - `sin confirmar` y `origen incierto` se ven junto a la etiqueta;
    - los detalles menores van plegados (`N detalles menores  enter para verlos`);
    - `Ya existía antes de este PR` va en `Dim`;
    - después `Dudas` y `Cómo partir este PR`;
    - al pie: descartados, sin revisar, reglas usadas y `Guardado en <ruta>`.
  - `TestReportOnlyCriticalIsRed`: el estilo `Critical` aparece únicamente en las etiquetas `Crítico`.
  - `TestReportKeys`: `enter` sobre los detalles menores los despliega; `c` sobre un hallazgo llama a `Copy` con `CommentText` y muestra `Copiado.`; `l` alterna la lista de descartados con su motivo; `o` llama a `Open` con la URL del PR; `q` emite `BackMsg`.
  - `TestReportNoClipboard`: si `Copy` devuelve `ErrNoClipboard`, muestra `No encontré wl-copy, xclip ni xsel para copiar.`.
  - `TestSysCopyPicksFirstAvailable`, usando un `LookPath` falso.
- [ ] **Step 2:** Run. Expected: FAIL.
- [ ] **Step 3:** Implementar. Usar `bubbles/viewport` para el scroll.
- [ ] **Step 4:** Run. Expected: PASS.
- [ ] **Step 5:** Commit `feat(tui): add report viewer`.

### Task 7: Configuración de modelos

**Files:**
- Create: `internal/tui/screens/models.go`, `internal/tui/screens/presetpicker.go`
- Test: `internal/tui/screens/models_test.go`

**Interfaces:**
- Consumes: `config.AllRoles`, `config.PresetAssignments`, `config.EffortsFor`, `config.Save` y `config.Path`.
- Produces:
  - `type ModelsModel struct`; `func NewModels(cfg config.Config, save func(config.Config) error) ModelsModel`; mensaje `ModelsSavedMsg{ Cfg config.Config }`. Para volver usa el `BackMsg` de la Task 4.
  - `type PresetPickerModel struct`; `func NewPresetPicker(cfg config.Config) PresetPickerModel`; mensajes `PresetChosenMsg{ Preset config.Preset }` y `CustomChosenMsg{}`.
  - `func roleLabel(r config.Role) (name, desc string)`, con los nombres y descripciones del boceto `models-v2.html` (por ejemplo `Agente superior` y `arma el plan y la síntesis`).
  - `func effortLabel(e config.Effort) string`: `""` → `—`, `low` → `bajo`, `medium` → `medio`, `high` → `alto`, `xhigh` → `muy alto`, `max` → `máximo`.

- [ ] **Step 1: Write the failing tests**
  - `TestModelsRender`: muestra una fila por rol, en el orden de `AllRoles`, con nombre, descripción, modelo y esfuerzo. El encabezado dice `partiste de Balanceado, con 0 cambios`.
  - `TestModelsCycleModel`: `right` sobre `Seguridad` pasa de `opus` a `sonnet`, la fila muestra `cambiado` en `Accent` y el encabezado dice `con 1 cambio`.
  - `TestModelsEffortRespectsModel`: con `haiku`, `e` no cambia nada (queda `—`). Al pasar de `opus`/`high` a `haiku`, el esfuerzo vuelve a `""`.
  - `TestModelsPreset`: `p` recorre `balanceado`, `economico` y `potente` y reemplaza todas las asignaciones.
  - `TestModelsSave`: `g` llama a `save` con `Preset` (o `personalizado` si hay cambios) y `Models`, y emite `ModelsSavedMsg`; `esc` emite `BackMsg` sin llamar a `save`.
  - `TestPresetPicker`: muestra las cuatro opciones con `Balanceado` marcado como `recomendado` y una vista previa de dos columnas de rol, modelo y esfuerzo del preset resaltado, más la línea `Uso por revisión: ligero 4 agentes, exhaustivo 9, con judgment 11.`. `enter` sobre `Personalizado` emite `CustomChosenMsg`.
- [ ] **Step 2:** Run. Expected: FAIL.
- [ ] **Step 3:** Implementar.
- [ ] **Step 4:** Run. Expected: PASS.
- [ ] **Step 5:** Commit `feat(tui): add model configuration screens`.

### Task 8: Configuración inicial

**Files:**
- Create: `internal/tui/screens/onboarding.go`
- Test: `internal/tui/screens/onboarding_test.go`

**Interfaces:**
- Consumes: `prb.Client.AuthStatus`, `execx.Exec` (para `claude --version`), `PresetPickerModel` y `ModelsModel` (Task 7) y `config.Save`.
- Produces: `type OnboardingModel struct`; `func NewOnboarding(deps OnboardingDeps) OnboardingModel`; `type OnboardingDeps struct{ PRB prb.Client; Exec execx.Exec; Save func(config.Config) error }`; mensaje `OnboardingDoneMsg{ Cfg config.Config }`.

- [ ] **Step 1: Write the failing tests**
  - `TestOnboardingToolsOK`: el paso 1 de 3 muestra `prb: sesión iniciada como <user>` y `Claude Code: <versión>`, y `enter` avanza.
  - `TestOnboardingToolsMissing`: si prb devuelve `no_credentials`, muestra `Corré prb auth login y apretá r.`; si `claude` no existe, muestra `No encontré claude. Instalá Claude Code.`. En los dos casos no deja avanzar.
  - `TestOnboardingWorkspace`: el paso 2 precarga `Identity.DefaultWorkspace`; con el campo vacío, `enter` muestra `Escribí el workspace de Bitbucket.`.
  - `TestOnboardingModels`: el paso 3 usa `PresetPickerModel`. Elegir `Balanceado` llama a `Save` con el workspace y el preset y emite `OnboardingDoneMsg`; `Personalizado` abre `ModelsModel` y, al guardar, termina igual.
  - `TestOnboardingEscGoesBack`: `esc` en el paso 3 vuelve al paso 2.
- [ ] **Step 2:** Run. Expected: FAIL.
- [ ] **Step 3:** Implementar. El paso 2 usa `bubbles/textinput`.
- [ ] **Step 4:** Run. Expected: PASS.
- [ ] **Step 5:** Commit `feat(tui): add first-run setup`.

### Task 9: Router, revisiones en segundo plano y comando raíz

**Files:**
- Create: `internal/tui/app.go`, `internal/tui/reviews.go`, `internal/tui/screens/repopicker.go`, `internal/cli/tui.go`
- Modify: `internal/cli/root.go` (`pr-review [repo]` abre la interfaz; agregar `config models`)
- Test: `internal/tui/app_test.go`, `internal/cli/tui_test.go`

**Interfaces:**
- Consumes: todas las pantallas y la parte 1 (`cli.Deps`, `cli.ResolveRepo`, `repocache`, `pipeline.Run` y `report.Write`).
- Produces:
  - `type App struct`; `func NewApp(d cli.Deps, sys sys.System, repo string, cfgFound bool) App`. Sin configuración arranca en onboarding; si `repo==""` arranca en el selector de repos; si no, en la lista.
  - `type Reviews struct`, que es el registro de revisiones en curso por número de PR, con su `cancel`, su canal de eventos y su resultado. Métodos: `Start(row, mode) (<-chan pipeline.Event, tea.Cmd)`, `Running() int` y `CancelAll()`.
  - `func ReviewCmd(d cli.Deps, repo string, row listdata.Row, mode pipeline.Mode, events chan<- pipeline.Event) tea.Cmd`, que hace la misma secuencia que el comando `review` (Context, Diff, Prepare, Run, Write y limpieza) y devuelve `ReviewDoneMsg`.
  - `func cli.DetectRepo(exec execx.Exec, dir string) (string, bool)`: lee `git remote get-url origin` y acepta `git@bitbucket.org:ws/repo.git`, `https://bitbucket.org/ws/repo(.git)` y `ssh://git@bitbucket.org/ws/repo.git`.
  - `type RepoPickerModel struct`, que lista los repos de `prb.Client.Repos`, filtra con `/` y emite `RepoChosenMsg{ Repo string }`.

- [ ] **Step 1: Write the failing tests**
  - `TestAppStartScreens`: sin configuración arranca en onboarding; con configuración y `repo==""`, en el selector de repos; con `repo`, en la lista.
  - `TestAppFlow`: `OpenModeMsg` → modo; `StartReviewMsg` → progreso y `Reviews.Running()==1`; `OpenResultMsg` → reporte; `BackMsg` → lista.
  - `TestAppBackground`: `BackgroundMsg` vuelve a la lista con la revisión corriendo. Cuando llega `ReviewDoneMsg`, se llama a `sys.Notify` con `pr-review` y `#477 listo: Pedir cambios.`, la fila del 477 pasa a `Reviewed` y `enter` + `o` sobre ella abre el reporte.
  - `TestAppQuitConfirm`: con una revisión en curso, `QuitMsg` muestra `Hay 1 revisión en curso. ¿Salir igual? (s/n)`. Con `n` sigue; con `s` llama a `CancelAll`, espera los `ReviewDoneMsg` y sale.
  - `TestAppModelsFromList`: `OpenModelsMsg` → configuración de modelos; `ModelsSavedMsg` actualiza la configuración que usan las revisiones siguientes.
  - `TestDetectRepo`: los tres formatos de URL de Bitbucket y uno de GitHub (`false`).
  - `TestRootArgs`: `pr-review stock` abre la interfaz con `<workspace>/stock`; `pr-review config models` abre directo la configuración de modelos; `pr-review review …` sigue funcionando.
- [ ] **Step 2:** Run `go test ./internal/tui/ ./internal/cli/`. Expected: FAIL.
- [ ] **Step 3:** Implementar. El programa corre con `tea.NewProgram(app)` en pantalla alternativa. `Reviews.Start` crea un `context.WithCancel` por revisión.
- [ ] **Step 4:** Run `make test lint`. Expected: PASS.
- [ ] **Step 5:** Verificación manual, con aviso al usuario porque usa su suscripción:
  1. `go run ./cmd/pr-review <repo>` con la configuración borrada (onboarding completo);
  2. lista;
  3. un PR en modo `ligero`;
  4. `s` durante el progreso;
  5. esperar la notificación;
  6. abrir el reporte;
  7. `c` y pegar.

  Expected: cada pantalla se parece a su boceto y el reporte coincide con `review.md`.
- [ ] **Step 6:** Commit `feat(tui): wire app router, background reviews and root command`.
