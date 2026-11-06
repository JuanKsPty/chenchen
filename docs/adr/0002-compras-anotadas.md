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

### Una compra, por cuantos lados llegue

Una misma compra puede llegar por varios lados, cada uno una **señal**: el Atajo, a mano,
el asistente (`agent`, lo que lee mientras el estado no llega) y, cuando se lean los
avisos del banco, un correo o una notificación. Un pago con Apple Pay puede generar dos:
el Atajo y el correo del banco. La regla de duplicados del Atajo (mismo monto y comercio
dentro de dos minutos) no sirve entre canales, porque el teléfono y el banco nombran distinto la
misma tienda.

Por eso cada compra anotada guarda sus señales en **`capture_signals`** (migración
00017), con una fila por canal:

- Toda compra tiene al menos una, la del canal que la anotó primero, y **nunca dos del
  mismo canal**: dos señales de un canal son dos compras, aunque sean del mismo monto y
  del mismo día.
- Antes de anotar nada, `captures.JoinSignal` busca la compra de la que habla la señal
  nueva: de la misma cuenta, con el mismo monto en centavos y sin señal de ese canal.
  1. Un canal con hora (el Atajo, el correo o la notificación) busca primero una que otro
     canal con hora oyó a una hora o menos (`JoinWindow`). Gana la más cercana en el
     tiempo.
  2. Después, y a mano o desde el asistente solo así, porque su hora es la de quien la
     escribe y no la de la compra, busca una anotada a un día o menos (`JoinDays`). Gana
     el día más cercano y luego la hora más cercana.

  Un canal con hora se une a una compra **en cualquier estado**: el correo de un pago
  rechazado que la persona descartó es ese mismo pago. A mano o desde el asistente,
  **nunca a una descartada**: si la persona descartó lo que mandó el Atajo y después
  escribe una compra del mismo monto ese día, está diciendo que sí pasó, y unida a la
  descartada no contaría.
- Si la encuentra, agrega la señal. **Nunca cambia el monto, el día ni el estado de la
  compra**: una unida a su línea o descartada sigue así y no vuelve a contar. Si no, la
  anota con esa señal como primera.
- Lo escrito a mano o por el asistente (comercio, categoría y nota) queda en la señal, y
  completa lo que a la compra le falte mientras siga abierta: la nota, si no tiene, y la
  categoría, si la suya vino de una regla o no tiene, con la misma precedencia de las
  reglas (lo que alguien eligió gana a una regla). Una unida a su línea no cambia: la
  línea es la compra ahora.
- Desde el asistente, una compra que ya contó (la misma referencia del banco o, sin ella,
  el mismo monto, comercio y día en la cuenta, en cualquier estado) es esa misma compra:
  lee las mismas compras en tránsito cada vez que mira. Se la reconoce por lo que mandó la primera vez, que queda en su
  señal, así que la encuentra aunque la persona la haya corregido o descartado después.
- A mano, la respuesta lo dice (`already_noted`) y devuelve la compra que ya estaba. Si el
  formulario repite el intento con el mismo id, la encuentra por ese id (`caller_id` de
  la señal, único por persona) en vez de anotarla. Al Atajo nunca se le dice: su
  respuesta es la misma byte a byte, para que quien tenga la clave no pueda preguntar qué
  compras ya existen. Por lo mismo, su regla de duplicados lee sus señales, no las
  compras.
- Solo un canal con hora le da hora a la compra en las listas; a mano o desde el
  asistente, no.

Cuando se equivoca, **cuenta de menos**: dos compras reales del mismo monto, el mismo día
y en la misma cuenta se unen solo si a cada una la contó un canal que a la otra le falta.
La segunda queda sin contar hasta que llega el estado, cuyas líneas cuentan solas. Lo
contrario, contar dos veces una compra porque dos canales la contaron a horas distintas,
es justo lo que esto evita. Así se mantiene «nunca contar doble».

Esto vale también para los canales con hora, y es a propósito. Un ejemplo: un café de
$3.50 a las 8:00 que solo mandó el Atajo y otro de $3.50 a las 15:00 en la misma tarjeta
que solo contó el correo del banco. El correo de las 15:00 no encuentra nada a una hora
o menos, pero por el día se une al café de las 8:00: chenchén cuenta $3.50 en vez de $7.00
hasta que llega el estado, que trae las dos líneas y cuenta las dos. Sin esa
regla del día, el correo de una sola compra que llega más de una hora tarde, cosa que
pasa, la anotaría otra vez y la contaría dos veces. Entre contar de menos hasta el estado
y contar doble, se elige contar de menos.

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
  anotadas. Lo mismo vale para la hora y el día dentro de los que dos señales son una
  compra (`JoinWindow`, `JoinDays`), que un test fija.
- Ninguna señal toca un saldo, y `TestBalancesNeverSeeSignals` lo fija igual que
  `TestBalancesNeverSeeCaptures`.
- Lo anotado puede corregirse o descartarse; lo impreso sigue sin poder editarse.
