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
