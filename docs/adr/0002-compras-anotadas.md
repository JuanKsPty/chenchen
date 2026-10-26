# ADR 0002 — Compras anotadas: aparte de lo que imprimió el banco

- **Estado:** aceptada
- **Fecha:** 2026-10-26

## Contexto

Según el ADR 0001, `entries` guarda **una fila por línea impresa** y no hay movimientos
manuales. Por eso chenchén solo se entera de un gasto cuando la persona importa el
estado de cuenta, a veces semanas después de la compra.

Ahora queremos que una compra aparezca al momento. Puede llegar de dos formas:

- **Desde el iPhone.** Una automatización de Atajos se dispara al pagar con Apple Pay y
  manda el monto y el comercio con una clave de la cuenta.
- **A mano.** La anota la persona, por ejemplo una compra con la tarjeta física.

Lo que manda Atajos no es confiable como fuente de verdad:

- Solo se dispara al pagar acercando el iPhone en una tienda. No se dispara con compras
  en línea, en apps ni con la tarjeta física.
- A veces no se dispara, llega tarde o llega dos veces. También se dispara con pagos
  **rechazados**.
- No trae la fecha de la compra ni un identificador. El monto llega como texto, con el
  formato del teléfono.
- Una propina o una preautorización hacen que el banco cobre un monto distinto.

Además, el saldo depende de que `entries` tenga solo lo impreso. `SumsByAccount`,
`SumThroughDay`, `SumBefore`, `AccountEntriesInOrder`, la conciliación y la búsqueda
de transferencias leen **todas** sus filas, sin ningún filtro. Si una compra anotada
fuera una fila más, movería el saldo y el descuadre, y la cuenta mostraría «No cuadra»
después de cada compra.

## Decisión

Las compras anotadas viven en su propia tabla, **`captures`**, y **nunca son filas de
`entries`**:

- **Nunca tocan un saldo.** Ninguna consulta de saldo, ancla, descuadre, conciliación o
  transferencias lee `captures`. Una consulta que no sabe de ellas no las ve, así que
  olvidar un filtro es imposible: no hay filtro que olvidar.
- **Si cuentan o no se deriva al leer, nunca se guarda.** Lo decide la vista
  `capture_states`, igual que el saldo y el descuadre se calculan al leer:

  | estado | qué significa | ¿cuenta como gasto? |
  |---|---|---|
  | `pending` | nada impreso puede ser esta compra todavía | **sí** |
  | `printed` | hay una línea de la misma cuenta, un gasto con el monto exacto, del día anterior a cinco días después, que ninguna otra compra reclamó y que la persona no rechazó para esta. Es un gasto si así la importó el estado o si la persona lo dijo: si después la persona la llama transferencia, la compra no vuelve a contar | no, cuenta la línea |
  | `missing` | un estado de la cuenta llega a tres días después de la compra y no la trae; casi siempre un pago rechazado | no |
  | `linked` | está unida a su línea | no, cuenta la línea |
  | `discarded` | la persona dijo que no pasó | no |

- **El estado de cuenta sigue siendo la verdad.** Cuando llega la línea, cuenta la línea
  y no la compra anotada. Unirlas solo pasa la categoría y la nota a la línea; nunca
  cambia su monto, sus fechas ni su descripción. Si la línea desaparece, la compra se va
  con ella en vez de volver a contar.
- **Solo se compara el día, el monto y la cuenta.** Ningún formato de estado trae la hora
  de la compra. El nombre del comercio tampoco decide, porque el teléfono y el banco
  nombran distinto la misma tienda.
- **Los tres días de espera** existen porque un estado puede terminar el día en que se
  descargó, con las compras de los últimos días todavía retenidas.
- La ventana y la espera viven en `store/captures.go` (`CaptureDaysBefore`,
  `CaptureDaysAfter`, `CaptureSettleDays`) y en la migración 00014. Un test las amarra.

Así, todo error se inclina a **contar de menos por un tiempo, nunca a contar doble**: en
cuanto algo impreso podría ser la compra, o un estado ya pasó su día sin traerla, la
compra deja de contar. La única excepción es cuando el banco cobra un monto distinto
(una propina o una preautorización): la línea no tiene el monto exacto, así que la
compra y su línea cuentan las dos hasta que la persona las una a mano o un estado
llegue a tres días después.

Se consideró guardar las compras como filas de `entries` con una marca de origen
(`origin = 'capture'`). Se descartó por cuatro razones:

- Cada consulta de saldo, ancla, descuadre, conciliación y transferencias necesitaría
  `AND origin = 'statement'`. Olvidarlo en una sola, hoy o en el futuro, mostraría un
  «No cuadra» falso o emparejaría una compra como transferencia.
- El trigger que impide editar lo importado no deja convertir la compra en su línea. Habría
  que borrar filas de `entries`, cosa que hoy nada hace.
- La identidad de una fila (`dedup_key`) sale de lo que imprimió el banco, y una compra
  anotada no tiene identidad impresa.
- Rompería la promesa del ADR 0001: una fila por línea impresa.

También se consideró guardar el estado de cada compra en una columna que el import
actualiza. Se descartó porque deja huecos donde la compra y su línea cuentan a la vez,
por ejemplo una compra anotada después del import o un par ambiguo.

### Una tarjeta que paga otra persona

Una tarjeta que la persona usa y otra persona paga (la de su papá, por ejemplo) es la
excepción a «el estado de cuenta es la verdad»: sus estados nunca llegan a chenchén. La
agrega la persona, con un alias y sus últimos 4 dígitos (`institution = 'manual'`,
`paid_by_other`, migración 00015), y vive solo de sus compras anotadas:

- **Nada impreso entra en ella.** Un trigger rechaza en esa cuenta líneas, estados,
  saldos declarados e imports. El import rechaza entero un estado de una tarjeta que
  termine en esos dígitos, o con líneas hechas con ella, y no se puede agregar a mano una
  tarjeta cuyos estados ya se importan. Así ninguna compra cuenta dos veces.
- **Sus compras quedan siempre `pending`.** Sin nada impreso que pueda ser ellas, cuentan
  siempre en Gastos y en el flujo del mes, hasta que la persona descarte alguna.
- **Su deuda no es de la persona.** Queda fuera de lo que debe y de su patrimonio. Su
  saldo es siempre cero, así que nunca le falta el saldo inicial ni muestra «No cuadra».

## Consecuencias

- El saldo, el ancla, el descuadre, la conciliación y las transferencias no cambian, y
  un test (`TestBalancesNeverSeeCaptures`) lo fija.
- Lo que sí debe incluir las compras pendientes (listas de movimientos, búsqueda, Gastos
  y el flujo del mes) tiene que leerlas a propósito, y solo las `pending` de
  `capture_states`.
- Con una propina o una preautorización, la compra y su línea cuentan las dos hasta que
  un estado llegue a tres días después o la persona las una a mano.
- En una cuenta que nunca recibe estados, un pago rechazado cuenta hasta que la persona
  lo descarte.
- La ventana (un día antes, cinco después) y la espera de tres días se calibraron con
  ejemplos, no con datos reales. Hay que revisarlas después de un mes de compras
  anotadas.
- Lo anotado puede corregirse o descartarse; lo impreso sigue sin poder editarse.
