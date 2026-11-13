# #477 Migrar facturas a nueva tabla

https://bitbucket.org/acme/api-pagos/pull-requests/477

**Revisar a mano.** No alcanza para decidir: faltó revisar parte del PR. Revisión incompleta: 2 agentes fallaron, 1 archivo sin revisar.

## Lo que introduce este PR

### Medio `src/x.go:4`

Qué pasa de F1.

Por qué pasa: Por qué de F1.

Solución: Arreglo de F1.

## Al pie

Sin revisar:

- src/enorme.go (parche omitido)

Agentes que fallaron (lo que cubrían no se revisó del todo):

- `seguridad` (rol seguridad): src/x.go, src/y.go. Motivo: se venció el tiempo
- `validador_bugs-F1` (rol validador\_bugs): candidato F1. Motivo: salida inválida: JSON roto

Reglas usadas: ninguna encontrada.
Modo: exhaustivo, 5 agentes, 3 min 0 s.
