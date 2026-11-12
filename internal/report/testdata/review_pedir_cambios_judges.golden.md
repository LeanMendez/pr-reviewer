# #477 Migrar facturas a nueva tabla

https://bitbucket.org/acme/api-pagos/pull-requests/477

**Pedir cambios.** Un problema crítico que introduce este PR: puede perder datos al migrar.

## Lo que introduce este PR

### Crítico `migraciones/0042_facturas.sql:18`

Qué pasa de F1.

Por qué pasa: Por qué de F1.

Solución, según `docs/reglas/migraciones.md` ("toda migración se tiene que poder revertir"): Arreglo de F1.

Jueces: A lo confirma; B lo confirma.

### Alto `src/facturas/pdf.go:9` · **Sin confirmar**

Qué pasa de F3.

No se pudo validar: tomalo como una sospecha y revisalo vos.

Por qué pasa: Por qué de F3.

Solución: Arreglo de F3.

### Medio `src/facturas/repo.go:55` · **Origen incierto**

Qué pasa de F4.

No está claro si el PR lo introdujo o ya estaba.

Por qué pasa: Por qué de F4.

Solución: Arreglo de F4.

### Medio `src/facturas/servicio.go:142`

Qué pasa de F2.

Por qué pasa: Por qué de F2.

Solución: Arreglo de F2.

Jueces: A lo confirma; B lo ve menor.

## Detalles menores y preguntas de diseño

- `src/facturas/nombres.go:3` Qué pasa de F5.

## Ya existía antes de este PR

El PR toca código que ya tenía estos problemas. Sirven de contexto y no cuentan para el veredicto.

- `src/facturas/legacy.go:30` Consulta dentro de un loop (N+1).

## Dudas

- `src/facturas/api.go:12` ¿Esto cambia el contrato que usa el repo de reportes?

## Cómo partir este PR

1. Tabla nueva y migración (migraciones/, 360 líneas)
2. Servicio y repositorio (src/facturas/, 540 líneas): depende de la 1, se mergea después

## Al pie

Descartados: 2.

- `src/a.go:1` Qué pasa de F7. Motivo: el validador no encontró la ruta que describe
- `src/b.go:2` Qué pasa de F8. Motivo: los dos jueces lo ven irrelevante

Excluidos de la revisión (generados): package-lock.json.

Reglas usadas: CLAUDE.md, docs/reglas/migraciones.md.
Modo: judgment, 12 agentes, 6 min 40 s.
