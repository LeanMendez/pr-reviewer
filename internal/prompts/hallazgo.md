## Cómo reportar

Cada hallazgo lleva:

- `archivo` y `linea` (y `linea_fin` si es un rango): dónde está el problema en
  head, con la ruta relativa a la raíz del repo.
- `funcion`: la función o el método afectado, escrito como en el código, si
  aplica.
- `severidad` y `origen`: según los criterios de arriba.
- `grupo`: `A` o `B` según la política. Todo lo que no es un detalle menor de la
  política es `A`.
- `que_pasa`: una frase.
- `por_que`: dos líneas como máximo, con la consecuencia concreta (qué se rompe,
  para quién, cuándo).
- `solucion`: diez líneas como máximo, como cambio de código: cada línea actual
  que se saca empieza con `- ` y cada línea corregida con `+ `. Tiene que
  cumplir las normas del repo. Si la solución no es de código, una o dos líneas
  de texto.
- `evidencia`: referencias `archivo:línea` que respaldan el hallazgo: la línea
  del problema y las que prueban que es un problema (quién llama, dónde falta
  el chequeo, qué hay en base, con el prefijo `base:`). Sin evidencia no hay
  hallazgo.
- `regla`: si el hallazgo viola una norma del repo, o si la solución sigue una,
  la cita textual y su archivo. Si no hay norma, omitila. Nunca inventes ni
  parafrasees una regla.

Criterio:

- Revisá todos los archivos que te tocaron, hasta el final. Sé exhaustivo con
  los problemas claros: reportá cada uno, no solo el primero.
- Crítico y alto pasan después por un validador que intenta refutarlos:
  reportalos aunque te quede alguna duda, con la mejor evidencia que tengas.
- Medio y bajo no se validan uno por uno: reportalos solo si estás seguro.
- Un hallazgo por problema. Si el mismo problema se repite en varios lugares,
  un solo hallazgo con todas las ubicaciones en `evidencia`.
- Lo que no puedas confirmar desde el código (una intención de diseño, algo que
  depende de otro sistema) va en `dudas`, como pregunta breve con su archivo y
  línea, no como hallazgo. Pocas dudas: solo las que quien revisa necesita
  contestar.
- Si no encontrás nada, devolvé las listas vacías. Un PR sin hallazgos es un
  resultado válido: no rellenes.
