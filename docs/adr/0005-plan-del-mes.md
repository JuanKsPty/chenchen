# ADR 0005 — El plan del mes: lo que la persona decide, al lado de lo que pasó

- **Estado:** aceptada
- **Fecha:** 2026-11-06

## Contexto

chenchén refleja lo que imprimió el banco (ADR 0001) y Gastos dice en qué se fue la plata.
Lo que no dice es cuánto queda para gastar este mes. Para eso hace falta algo que ningún
estado de cuenta trae: lo que la persona **espera**. Cuánto le entra y qué día, qué pagos
salen cada mes, cuánto aparta y, si quiere, cuánto le deja a cada categoría.

Hay cuatro cosas que cuidar:

- **Todo número de chenchén sale hoy de un banco** o de una compra anotada (ADR 0002). Un
  plan lo escribe la persona. Si se mezclara con los movimientos, un saldo podría moverse
  por algo que nadie pagó.
- **El gasto ya tiene una definición:** la suma de Gastos, con las compras pendientes y las
  tarjetas que paga otra persona. Si el plan contara el gasto a su manera, la persona vería
  dos cifras que no cuadran.
- **Un pago de cada mes casi nunca aparece igual.** Un seguro trae su impuesto en otra
  línea, un sueldo llega un día antes o después, y a veces el banco marca como
  transferencia lo que fue un pago.
- **La palabra "budget" ya está tomada:** nombra el tope de gasto del asistente
  (`00002_ai_budget`).

## Decisión

### Qué es el plan

> Ingreso esperado − pagos fijos − ahorro (− compras planeadas) = **libre para gastar**

- **Ingreso esperado.** Un ítem por depósito, con su día: dos sueldos de quincena son dos
  ítems. Es la suma de lo planeado, no de lo que entró.
- **Pagos fijos.** Lo que sale cada mes, con su día y el texto con que aparece en el
  estado.
- **Ahorro.** Lo que la persona aparta cada mes. Puede ir ligado a la cuenta de ahorro o de
  inversión real que lo guarda, y a una meta. Una cuenta respalda una sola meta a la vez:
  dos metas pueden usar la misma cuenta una después de la otra, nunca en el mismo mes.
- **Topes por categoría,** opcionales: cuánto de lo libre puede llevarse una categoría de
  gastos, con sus hijas.
- **Te quedan:** lo libre menos lo que ya salió de lo libre este mes. También por día.

### Espejo: solo se guarda lo que la persona decide

Siguiendo el ADR 0001:

- **Se guardan solo decisiones** (migración 00021):
  - los ítems (`plan_items`);
  - sus valores por mes (`plan_item_versions`);
  - los topes (`plan_caps`);
  - qué movimiento pagó qué, cuando la persona lo corrige (`plan_links`);
  - las sugerencias que descartó (`plan_dismissals`).
- **Lo real se calcula cada vez que se lee:** cuánto salió, qué movimiento pagó qué ítem,
  el estado de cada pago, los niveles y el avance de las metas.
- **El plan nunca escribe** una línea, una compra anotada ni un saldo declarado, y ninguna
  consulta de saldo lee sus tablas.
- **El ingreso extra nunca cambia lo libre por sí solo.** Se suma con «Sumar a lo libre»,
  que crea un ítem de un solo mes y se puede deshacer.

### Versiones: el pasado conserva su plan

Cada valor vale para un tramo de meses, de `from_month` a `to_month` (sin fin si
`to_month` es NULL). Es el monto y el día de un ítem, o el monto de un tope.

- **Un mes pasado no se escribe nunca.** Queda con el plan que tuvo. Una edición vale para
  el mes actual o uno posterior, hasta 24 meses adelante.
- **El mes siguiente ya está planeado,** porque un tramo abierto sigue.
- **«Desde este mes»** hace ceder los tramos que empezaban después. El tramo que cubría el
  mes se cierra el mes anterior, o se cambia ahí mismo si empezaba en ese mes.
- **«Solo este mes»** parte el tramo alrededor del mes, y los meses de los lados quedan
  como estaban.
- **Dos tramos vecinos iguales se juntan.** Una edición que no cambia nada no escribe
  nada: guardar dos veces lo mismo es inofensivo, y deshacer es volver a mandar lo
  anterior.
- **Quitar** «desde este mes» cierra el ítem el mes anterior, y «solo este mes» lo saca de
  ese mes. Un ítem sin ningún mes se borra. Los vínculos de los meses quitados se van con
  ellos.
- **Dos tramos de un ítem, o dos topes de una categoría, nunca cubren el mismo mes.** Lo
  impiden triggers en la base. No es una restricción de exclusión porque esa necesitaría
  `btree_gist`, y la API no arrancaría donde falte.
- **Lo que vive en el ítem vale para todos sus meses:** el nombre, la categoría, la cuenta,
  la meta y el texto con que se reconoce. Cambiar el texto cambia qué se reconoce también
  en meses pasados, igual que recategorizar cambia Gastos de meses pasados. Un monto
  planeado de un mes pasado no cambia nunca.
- **El tipo de un ítem no cambia.** Un sueldo no se vuelve un pago: sus meses y sus
  vínculos significarían otra cosa.
- **El servidor decide qué día es hoy,** el mes actual y los días que quedan, en hora de
  Panamá, no el navegador. Así la web y Claude dicen lo mismo.

### Cada salida cuenta una vez

El plan no cuenta el gasto a su manera: reparte en baldes el mismo gasto de Gastos.

- **Cada salida del mes cae en un solo balde:** un pago fijo, una compra planeada o lo libre
  («Llevas»). Por eso «Llevas» más lo asignado es siempre el total de Gastos.
- **Un pago fijo que ya salió cuesta lo que se pagó,** no lo planeado. Uno que aún no sale
  guarda su monto apartado.
- **El ahorro se aparta siempre del ingreso** y nunca suma en «Llevas»: llega a su cuenta
  como transferencia, y una transferencia nunca es gasto.
- **El pago de una tarjeta nunca es un pago fijo,** porque sus compras ya cuentan. Su último
  estado (corte, fecha de pago y pago mínimo) se muestra junto al mes en que vence, y en el
  mes actual también si vence después, pero nunca se resta. Está pagada cuando lo que entró
  a la tarjeta como transferencia después del corte cubre el mínimo. Si falta y vence en
  tres días o menos, el plan avisa.
- **Queda una sola doble cuenta.** Un pago que el reconocimiento no ve conserva su monto
  apartado y además suma en «Llevas». Siempre se ve («Aún no lo vemos») y se arregla con un
  toque. Es el mismo tipo de error que las propinas del ADR 0002.

### Reconocer qué pagó qué, al leer

- **Un ítem se reconoce** por su texto, comparado como una regla (sin mayúsculas ni
  tildes), por su monto, con margen, y por su día.
  - Un sueldo puede llegar en el mes de al lado.
  - Un pago fijo se busca en su mes.
  - Un cargo chico de la misma cuenta y el mismo día, como el impuesto de un seguro, se
    suma a su pago.
  - Sin texto no se reconoce solo: se ofrecen candidatos.
- **Lo hace una función pura** cada vez que se lee el plan, y nada de eso se guarda.
- **Cuando se equivoca, la persona lo corrige con un vínculo:** «fue este movimiento»,
  «este no» o «lo pagué desde otra cuenta». El vínculo es lo único que se guarda. Se va si
  se borra el movimiento o si se quitan esos meses del ítem.

### Vínculos: lo que dice la persona va primero

Al leer, primero se aplican los vínculos y después el plan reconoce el resto solo.

- **«Fue este»** (`assign`): ese movimiento paga ese mes del ítem, aunque no tenga su texto
  ni su monto.
  - Tiene que estar en ese mes o en uno de al lado, y ser un ingreso para un sueldo o un
    gasto para un pago fijo.
  - Paga una sola cosa a la vez. Para moverlo, primero se deshace el vínculo.
- **«Este no»** (`reject`): ese movimiento nunca se reconoce ni se ofrece para ese mes del
  ítem.
- **«Lo pagué desde otra cuenta»** (`paid`): el mes queda pagado y conserva lo planeado,
  que es como cuenta, una sola vez. Si después aparece su movimiento, cuenta lo pagado.
- **Si dos vínculos nombran el mismo movimiento, vale el más antiguo.** Si la persona dice lo
  contrario sobre el mismo movimiento y el mismo mes, vale lo último que dijo. Una línea es
  el mismo movimiento que la compra anotada que el plan muestra como ella, unida a ella o
  impresa con ella entre las que podría ser.
- **Un vínculo puede corregir un mes que ya pasó:** cambia qué se reconoció, nunca lo
  planeado. Ninguno pasa de 24 meses adelante.
- **Un pago asignado a otro mes se carga a ese mes.** El alquiler de octubre pagado el 2 de
  noviembre cuenta en los pagos fijos de octubre. En noviembre sigue en Gastos, pero no en
  «Llevas».
- **Un vínculo a una compra anotada la sigue hasta su línea,** sin reescribirse:
  - pendiente, la paga la compra;
  - unida a su línea, la paga la línea;
  - impresa sin unir, la paga la línea de las que podría ser que otro vínculo de ese mes
    nombra, o si no, la primera libre. Todas tienen su monto, así que el mes suma lo mismo
    con cualquiera;
  - descartada, o pasada por un estado que no la trae, no paga nada y el pago vuelve a
    apartar lo planeado. Si se restaura, lo vuelve a pagar.
- **Un vínculo solo paga con un movimiento de ese mes o de uno de al lado,** también al
  leerse. Si la línea de una compra anotada se imprime, o se le corrige el día, a más de un
  mes del mes que pagaba, no paga nada: ese mes vuelve a apartar lo planeado, y el
  movimiento cuenta una vez, en su propio mes, que no lee los vínculos de meses tan lejanos.
- **Lo que aún no se ve trae ayuda** para arreglarlo con un toque:
  - hasta tres candidatos. Un pago fijo con texto ofrece los gastos con ese texto, aunque el
    monto no cuadre; sin texto, los que están a 10 % de lo planeado y a 5 días de su día.
    Un sueldo ofrece los ingresos a 25 % de lo planeado y a una semana de su día, con
    cualquier texto;
  - una pista cuando el banco imprimió el pago como transferencia: una transferencia que
    sale ese mes con el texto del pago. Se arregla como siempre, desmarcando «Es una
    transferencia entre tus cuentas» en el movimiento. El plan solo la lee: una
    transferencia nunca cuenta como gasto.

### Niveles sin regañar

- **El servidor calcula los niveles,** para que la web y Claude digan lo mismo:
  - `near` al 80 % de lo libre o de un tope;
  - `over` al pasarse;
  - `no_free` si el plan no deja nada libre.
- **Se muestran en neutro:** sin rojo ni naranja, sin signos de exclamación y nunca «te
  pasaste». Se dice la cifra: «$42.10 más de lo previsto».

### Los nombres dicen plan

El paquete se llama `plan`, las tablas `plan_*` y las rutas `/api/plan`. Nunca "budget"
ni "presupuesto": esa palabra ya nombra el tope de gasto del asistente. Y ninguna
herramienta de Claude se llama solo "plan", porque ya existe `sync_plan`.

## Consecuencias

- **Ya llegó:**
  - el esquema completo, con las cinco tablas en una sola migración (00021), para quedar
    expuestos una sola vez a la regla de orden de goose;
  - guardar y quitar ingresos, pagos fijos, ahorro y topes, con sus versiones;
  - leer el plan del mes, con sus cuentas, niveles y quincenas, y las rutas para
    escribirlo (`/api/plan`);
  - los vínculos, con los candidatos, la pista de la transferencia y las tarjetas;
  - este ADR.

  Todavía no hay pantalla: el plan se lee y se escribe solo por la API.
- **Lo que falta llega después** y enmienda este ADR donde haga falta:
  - la pantalla bajo Gastos y la tarjeta de Inicio, con sus avisos;
  - las sugerencias de pagos fijos e ingresos sacadas del historial, y descartarlas;
  - las compras planeadas, que hoy se rechazan, y el avance de las metas según la cuenta
    ligada;
  - que Claude lea y edite el plan.
- **Un pago fijo que no se reconoce cuenta dos veces** hasta que la persona lo vincula.
  Siempre se ve, con sus candidatos o su pista.
- **Los topes se quedan cortos mientras haya gasto sin categoría.** Lo que no tiene
  categoría cuenta en lo libre, pero en ningún tope.
- **El plan es tan actual como el último estado importado** y las compras anotadas.
- **Ninguna edición cambia el monto planeado de un mes pasado.** Lo que sí alcanza al
  pasado:
  - editar lo que vive en el ítem (nombre, categoría, cuenta, meta y texto), que vale para
    todos sus meses;
  - un vínculo sobre un mes pasado, que cambia qué se reconoció en él;
  - borrar un movimiento vinculado, que se lleva su vínculo;
  - borrar una categoría, que se lleva sus topes, también los de meses pasados, y deja sus
    ítems sin categoría;
  - borrar una cuenta, que deja su meta sin cuenta.
- **La migración 00021 tiene que quedar por encima de toda migración de `main`** al
  fusionarse. goose no arranca si llega una versión menor que una ya aplicada.
