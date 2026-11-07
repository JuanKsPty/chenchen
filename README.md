# chenchén

Una app de finanzas personales que lee lo que tus bancos ya imprimieron —los estados
de cuenta de tus cuentas y tarjetas de crédito, y tu cuenta de **Interactive
Brokers**— y te dice cuánto tienes, cuánto debes, en qué se te va y, sobre todo,
**dónde deja de cuadrar una cuenta con su banco**. No mueve tu plata: solo la ordena.

Backend en Go sobre PostgreSQL, interfaz en React y un asistente de IA que analiza tus
datos a través del [Model Context Protocol](https://modelcontextprotocol.io).

**En vivo:** <https://chenchen.juank.tech>

---

## Qué hay adentro

Cuatro secciones, en pestañas arriba en escritorio y en una barra abajo en el
teléfono, más el asistente y los ajustes:

| Sección | Qué ves |
|---|---|
| **Inicio** (`/inicio`) | Tu patrimonio neto —lo que tienes, lo que debes y tus inversiones—, los avisos que piden algo de ti (una cuenta que no cuadra, cuentas sin saldo inicial, transferencias por confirmar, archivos que Claude subió y no entraron solos), el mes en curso y lo más reciente. |
| **Cuentas** (`/cuentas`) | Cuentas, tarjetas e inversiones con su saldo. Desde aquí importas un estado de cuenta o conectas Interactive Brokers. Cada cuenta abre su detalle: saldo, conciliación línea por línea, corte y pago mínimo de la tarjeta, posiciones. |
| **Movimientos** (`/movimientos`) | Todo lo importado, con búsqueda por comercio, monto o nota, filtros por cuenta, categoría, fechas y tipo, y exportación a CSV. Tocar un movimiento abre Categorizar. |
| **Gastos** (`/gastos`) | En qué se te va el mes por categoría, entradas y salidas por mes, las transferencias que esperan un sí o un no y las reglas (`/gastos/reglas`). |
| **Asistente** | Un panel a la derecha en escritorio y una hoja en el teléfono. Le preguntas por tu dinero; propone cambios y tú decides cuáles aplicar. |
| **Ajustes** (`/ajustes`) | Modo claro, oscuro o el del sistema; el PIN y este dispositivo; las conexiones con Interactive Brokers, con los atajos del iPhone que anotan tus compras y con Claude; y tus reglas. |

En un dispositivo de confianza entras con un PIN de 6 dígitos, y la app trae manifiesto
e íconos para agregarla a la pantalla de inicio del teléfono.

---

## Levantarlo

```sh
git clone https://github.com/JuanKsPty/chenchen.git
cd chenchen
cp .env.example .env
docker compose up
```

La interfaz queda en <http://localhost:5173> y la API en <http://localhost:8080>. Cada
valor del `.env` tiene un default que funciona, incluida la clave de IA, que puede
quedar vacía. El stack completo cabe en unos 450 MiB: API 160m, web 32m, PostgreSQL 256m.

Para desarrollar, solo la base de datos:

```sh
docker compose -f docker-compose.dev.yml up -d
cd api && go run ./cmd/api
cd web && npm install && npm run dev
```

---

## El modelo: un espejo de lo que imprimió el banco

La verdad sobre tu dinero la tiene el banco, no esta app. Así que chenchén no lleva una
contabilidad propia: guarda **una fila por cada línea que imprimió un estado de
cuenta**, con el saldo que el banco imprimió al lado, y todo lo demás se calcula al
leer.

| Tabla | Qué guarda |
|---|---|
| `accounts` | Una por cuenta real, identificada como la identifica su banco: institución, número y moneda. Si el estado de cuenta trae número de cliente y de producto, cuenta el del producto. La excepción es una tarjeta que paga otra persona: la agregas tú, con un alias y sus últimos 4 dígitos (`manual`, `paid_by_other`), y nada impreso entra en ella. Sus compras anotadas cuentan como gasto tuyo; su deuda no entra en lo que debes ni en tu patrimonio. |
| `entries` | Una por línea impresa. Monto con signo desde tu punto de vista: un cargo de tarjeta es negativo, y una tarjeta que debe $14.30 tiene saldo −14.30. Fecha civil de Panamá, sin zona horaria que la corra un día. |
| `statements` | Período y saldos del estado de cuenta: inicial, final, disponible, retenido. |
| `balance_checkpoints` | «Al cierre de este día el saldo era X», del estado de cuenta, del Cash Report de IBKR o ingresado a mano. Uno por cuenta es el **ancla**. |
| `import_runs` | Cada import o sincronización con lo que vio: líneas nuevas, duplicadas, fallidas, advertencias. |

**Saldo** = saldo inicial (derivado del ancla) + la suma de los movimientos.
**Descuadre** = saldo declarado − saldo calculado. Cero significa que la cuenta dice
exactamente lo mismo que su banco.

Un trigger impide cambiar el monto, las fechas o la cuenta de una línea importada. Lo
que sí puedes cambiar —categoría, nota, si es una transferencia— no altera ningún saldo.

### Por qué no TigerBeetle

chenchén empezó sobre un ledger de doble entrada en TigerBeetle. Para una app que
**refleja** saldos que ya existen en otro lado, esa regla de no sobregiro inventaba
saldos iniciales, perdía líneas que el ledger rechazaba y dejaba las tarjetas fuera del
patrimonio. [La decisión completa está en el ADR](docs/adr/0001-postgres-only.md).

### Conciliación

`GET /api/accounts/{id}/reconciliation` recorre la cuenta en el orden del banco y, en
cada línea, pone el saldo calculado junto al que imprimió el banco. La pantalla de la
cuenta se abre en **la primera línea donde la diferencia cambia**: si un import perdió
una fila, esa es la fila siguiente. También marca los huecos entre estados de cuenta,
que son movimientos que nunca se importaron.

### Transferencias entre tus cuentas

Pagar la tarjeta desde tu cuenta no es gastar dos veces. La fila «Pagos» de la tarjeta
y los depósitos o retiros de IBKR se importan como `transfer`. Si hay **exactamente
una** línea bancaria del monto opuesto en ±5 días, el par se confirma solo al importar.
Si hay dudas, chenchén te lo sugiere en Gastos para que lo confirmes o lo descartes, y
un par descartado no vuelve a aparecer.

Lo que el banco ya dice en la línea también cuenta, esté o no la otra cuenta en chenchén:

- Una transferencia «entre cuentas» es `transfer`.
- Un ACH, una TEF o una transferencia que lleva tu nombre (nombre y primer apellido, como
  en tu perfil o en el estado de cuenta) es `transfer`.
- Un pago que te manda una persona desde la app del banco es `refund`: casi siempre es su
  parte de algo que pagaste tú, así que baja tu gasto en vez de inflar tus ingresos.
- Una nota de crédito (una compra revertida, una promoción) es `refund`.
- La comisión del banco es `fee`.

Las transferencias y la compraventa de valores **nunca** cuentan como gasto ni ingreso.
Gasto es gasto + comisiones + intereses − reembolsos, e ingreso es ingreso, con el
mismo filtro para Movimientos, el CSV, Gastos y el asistente.

### Reglas

«Lo que contenga SUPER 99 va a Supermercado.» Una regla se aplica a cada import y,
si quieres, a lo que ya importaste, pero **nunca pisa una categoría que elegiste a
mano**.

### El plan del mes

> Ingreso esperado − pagos fijos − ahorro = **libre para gastar**

`GET /api/plan?month=AAAA-MM` dice cuánto te queda para gastar en el mes y cuánto por
día. Guardas lo que esperas (`PUT /api/plan/items/{id}` y `PUT /api/plan/caps/{categoría}`):
cada sueldo con su día, los pagos que salen cada mes, lo que apartas y, si quieres, un
tope por categoría. Lo real se calcula cada vez que se lee, con los mismos movimientos
que suma Gastos, y nada de eso se guarda:

- **Cada salida del mes cae en un solo lugar:** el pago fijo que pagó, o lo libre. Un
  pago que ya salió cuesta lo que se pagó; uno que no ha salido guarda su monto.
- **Un pago se reconoce** por su texto, comparado como una regla, por su monto (±25 %) y
  por su mes. Un sueldo, por su texto, su monto y una semana alrededor de su día, aunque
  llegue en el mes de al lado. El cargo chico que va al lado de un pago, en la misma
  cuenta y el mismo día, como el impuesto de un seguro, se suma a ese pago.
- **Cuando se equivoca, lo corriges** con `POST /api/plan/links` («fue este movimiento»,
  «este no» o «lo pagué desde otra cuenta») y lo deshaces con `DELETE`. Un movimiento paga
  una sola cosa, y si es de un mes vecino se carga a ese mes. Un pago que aún no se ve
  ofrece hasta tres candidatos, y una pista si el banco lo imprimió como transferencia.
- **Las tarjetas** muestran su fecha de pago y si ya cubriste el mínimo, sin restar nada:
  sus compras ya cuentan.
- **El ingreso extra nunca cambia lo libre** por sí solo, y el ahorro nunca cuenta como
  gasto: llega a su cuenta como transferencia.
- **Al 80 %** de lo libre, o de un tope, el plan dice que está cerca; un centavo más allá
  del total, que va por encima. El día de hoy, el mes y los días que quedan los decide el
  servidor, en hora de Panamá.
- **Cada valor vale por tramos de meses.** Un mes pasado conserva el plan que tuvo, y una
  edición vale desde un mes o solo para ese mes, hasta 24 meses adelante.
- **El historial sugiere qué planear** (`GET /api/plan/suggestions`): lo que entró o salió
  una vez al mes, con el mismo monto (±10 %), en al menos dos de los últimos tres meses
  cerrados y sin haber dejado de llegar, y lo que apartas cada mes en una cuenta de ahorro
  o de inversión. Un sueldo de quincena son dos sugerencias, y el impuesto de un cargo se
  suma a él. Nunca sugiere transferencias, el pago de la tarjeta ni lo que el plan ya
  tiene, y un pago por Yappy a una persona se llama «Yappy de cada mes», sin su nombre.
  Lo que descartas (`PUT /api/plan/dismissals`) no vuelve hasta que lo deshaces con
  `DELETE`.

Como todo lo demás, el plan nunca mueve un saldo ([ADR 0005](docs/adr/0005-plan-del-mes.md)).

---

## Imports

Subes el estado de cuenta tal como te lo da tu banco. El formato se reconoce solo, por
su contenido:

| Fuente | Archivo | Qué se lee |
|---|---|---|
| Cuenta bancaria | `.xlsx` | Movimientos, saldo corrido por línea y el saldo total. Las fechas traen la hora de la descarga, no la del movimiento, así que solo se lee el día. La versión `.txt` de la misma exportación no dice de qué cuenta es y se rechaza (`account_not_stated`). |
| Cuenta bancaria | `.csv` (Latin-1) | Producto, saldo inicial, en libros y disponible, con el resumen de débitos y créditos como control. |
| Tarjeta de crédito | `.txt` o `.xlsx` | Movimientos y fecha de proceso. El mismo movimiento bajado en texto y en Excel es uno solo. No traen saldos: la tarjeta queda **sin saldo inicial** hasta que subes su estado cerrado en PDF o anotas lo que debías al corte. |
| Estado cerrado de la tarjeta | `.pdf` | Solo el resumen: la fecha de corte y la deuda al corte, que queda como el saldo de la tarjeta ese día. La deuda anterior es la del corte previo y solo sirve para comprobar que el resumen cuadra. Sus movimientos se ignoran a propósito: no traen referencia, y llegan con el `.txt` o el `.xlsx`. Un PDF sin ese título y ese resumen no se reconoce (`unrecognised_format`). |
| Interactive Brokers | Flex Web Service | Cash Transactions, Trades (Execution), Open Positions y Cash Report. |

- **Reimportar es seguro.** La clave de dedup sale solo de la identidad externa de la
  línea, nunca de un id interno, así que el mismo archivo no crea filas nuevas y dos
  archivos que se solapan se deduplican.
- **Cada archivo va en una transacción.** O entra entero o no entra.
- **Una cadena de saldos rota es una advertencia, no un rechazo.** Solo se rechaza lo
  que no se puede leer.
- **Solo revisar.** `POST /api/imports/dry-run` lee el archivo y devuelve lo que
  entendió —cuenta, período, saldos, primer quiebre— sin escribir nada.
- **Un archivo que no se puede leer se explica en palabras.** La API responde con un
  código estable (`unrecognised_format`, `unreadable_statement`, …) y un mensaje para
  la persona; el detalle técnico del parser queda en el log.

### IBKR

El cliente no envía fechas: IBKR devuelve el *Period* guardado en el Flex Query. Para
que la sincronización traiga datos nuevos, configura el query con **Period = Last 365
Calendar Days** y las secciones Open Positions (Summary), Trades (Execution), Cash
Transactions (Detail) y **Cash Report**. IBKR publica un día después de su proceso
nocturno, así que sincroniza por la mañana. Cada sync muestra el período que devolvió
IBKR y la fecha en que lo generó, y advierte si vino viejo o le falta una sección.

```sh
docker compose --profile ibkrsync run --rm ibkrsync
```

Es un job de una sola pasada, pensado para programarse una vez al día por la mañana.

---

## El asistente

Un **servidor MCP que corre dentro del mismo binario**, conectado al cliente por un
transporte en memoria. Las herramientas llaman a los mismos servicios que los handlers
REST.

| Lee | Propone |
|---|---|
| `list_accounts`, `list_movements`, `list_categories`, `list_rules` | `propose_recategorize` |
| `spending_by_category`, `cash_flow` | `propose_note`, `propose_rule` |
| `explain_drift`, `list_transfer_suggestions` | `propose_transfer_decision` |
| `get_portfolio` | `propose_checkpoint` |

**Ninguna herramienta escribe.** Una herramienta `propose_*` comprueba el cambio contra
tus datos y lo guarda como propuesta, con un resumen que escribe el servidor y no el
modelo. El chat la muestra como una tarjeta y **solo tu «Aplicar» hace el cambio**. Al
aplicar se vuelve a comprobar, y un `UPDATE` condicional hace que dos clics la apliquen
una sola vez. Nada de lo que puede proponer cambia un saldo, y ninguna herramienta mueve
dinero.

**Claude, desde fuera.** Con `PUBLIC_URL`, el mismo servidor responde en `/api/mcp` a
Claude (claude.ai, su app y Claude Code), con un token propio por OAuth
([ADR 0004](docs/adr/0004-claude-por-mcp.md)). Ahí lee lo mismo, más
`list_pending_purchases`, `sync_plan` e `import_status`. Si la persona lo permite, además
anota compras (`note_purchase`), pone categoría y nota (`set_category`, `set_note`) y
entrega estados de cuenta (`prepare_import`). Nada de eso mueve un saldo, salvo el archivo
que generó el propio banco, que entra solo si continúa una cuenta que ya existe y cuadra
con ella; si no, queda por revisar en la app.

**Ninguna herramienta acepta un `user_id`.** La identidad viene del token de la petición
y queda fijada en el servidor MCP antes de que el modelo diga nada, así que no hay
argumento que falsificar. Las descripciones de los movimientos entran al contexto del
modelo, y cada resultado que las trae le recuerda que son datos, no instrucciones.

### El gasto tiene techo

El registro está abierto, así que la clave de API es alcanzable por cualquiera que se
registre. Cada llamada reserva su costo estimado antes de hacerse, en una sola
sentencia:

```sql
UPDATE ai_budget SET reserved_micros = reserved_micros + $3
 WHERE scope = $1 AND key = $2
   AND spent_micros + reserved_micros + $3 <= cap_micros
```

Si no afecta ninguna fila, la llamada no se hace. Hay tres techos: el total, el diario y
el diario por usuario. Gana el más estrecho. Sin clave o sin presupuesto, el asistente
dice que no está disponible y por qué. Todo lo demás sigue funcionando.

---

## Variables de entorno

Todo tiene un default que funciona; ver `.env.example`.

| Variable | Default | Para qué |
|---|---|---|
| `DATABASE_URL` | `postgres://chenchen:chenchen@localhost:5432/chenchen?sslmode=disable` | PostgreSQL. |
| `JWT_SECRET` | *(vacío)* | Firma los tokens. Vacío, se genera uno por proceso y las sesiones no sobreviven un reinicio. |
| `COREBANK_CONFIRM_WIPE` | *(vacío)* | La migración 00011 reemplaza el modelo de dinero y borra los datos financieros anteriores. La API no arranca sobre una base anterior a ella si esto no vale `00011`. |
| `ANTHROPIC_API_KEY` | *(vacío)* | Opcional. Nunca en el repo: CI falla si aparece una clave en un archivo versionado. |
| `ANTHROPIC_MODEL` | `claude-sonnet-5` | |
| `AI_BUDGET_USD` / `AI_DAILY_BUDGET_USD` / `AI_USER_DAILY_BUDGET_USD` | `4.00` / `1.00` / `0.30` | Techos de gasto del asistente. |
| `AI_MAX_TOOL_TURNS` | `4` | Vueltas de herramientas por mensaje. |
| `IBKR_TOKEN_ENCRYPTION_KEY` | *(vacío)* | 32 bytes en base64 (`openssl rand -base64 32`). Cifra el token de IBKR. Vacío, vincular IBKR se rechaza. Cambiarlo invalida el token guardado. |
| `PUBLIC_URL` | *(vacío)* | La dirección de la app, sin path (`https://chenchen.example.com`). Con ella, Claude puede conectarse a chenchén por OAuth y MCP ([ADR 0004](docs/adr/0004-claude-por-mcp.md)). Vacía, esas rutas responden 404 y todo lo demás sigue igual. Solo `localhost` admite `http`. Déjala vacía hasta que estén desplegados el endpoint del MCP y la página `/conectar`. |
| `OAUTH_RATE_LIMIT` | `30` | Pedidos por minuto y por dirección a `/api/oauth/authorize` y a `/api/oauth/token`, contados por separado. |
| `ENV` / `LOG_LEVEL` | `development` / `info` | `production` pasa los logs a JSON. |
| `VITE_SHORTCUT_URL` | *(vacío)* | Opcional, al construir la imagen de la web: el enlace https de iCloud a un atajo listo para anotar compras desde el iPhone. Vacío, la web muestra solo los pasos para armarlo. Cambiarlo pide reconstruir la imagen; nunca es un secreto. |

---

## Estructura

```
api/
  cmd/api                 la API
  cmd/import              importa un archivo a mano contra la base
  cmd/ibkrsync            sincroniza todas las cuentas de IBKR, una pasada
  internal/statement      parsers puros: cuenta .xlsx, cuenta .csv, tarjeta .txt y .xlsx, estado .pdf
  internal/ibkr           cliente y parser del Flex Web Service
  internal/imports        un archivo → una transacción
  internal/accounts       saldos, anclas, conciliación
  internal/movements      búsqueda, filtros, categoría, nota, CSV
  internal/reports        gasto, ingreso y flujo
  internal/plan           el plan del mes: lo que esperas al lado de lo que pasó
  internal/transfers      pares de transferencias entre tus cuentas
  internal/rules          reglas de categoría
  internal/proposals      lo que propone el asistente
  internal/mcpserver      las herramientas del asistente
  internal/chat           el loop del asistente y su techo de gasto
  migrations/             goose, embebidas en el binario
web/                      Vite + React + TypeScript + Tailwind
deploy/                   la API tal como se despliega
docs/adr/                 decisiones de arquitectura
```

---

## Desarrollo y CI

```sh
docker compose -f docker-compose.dev.yml up -d
cd api && TEST_DATABASE_URL='postgres://chenchen:chenchen@localhost:5432/chenchen?sslmode=disable' go test ./...
cd web && npm run build
```

Cada test de integración crea su propia base desechable a partir de
`TEST_DATABASE_URL`. Sin ella esos tests se saltan en local. En CI (`CI=true`) fallan,
así que una base que falta nunca pasa como verde.

`.github/workflows/ci.yml` corre en cada pull request, incluidos los de una pila de PRs:

| Trabajo | Qué comprueba |
|---|---|
| `api` | `gofmt`, `go vet`, `go mod tidy -diff`, build estático y `go test -race` contra PostgreSQL |
| `web` | formato, typecheck y build |
| `secrets` | que no haya claves ni `.env` versionados |
| `images` | que los dos Dockerfiles construyan |

La API compila sin cgo (`CGO_ENABLED=0`). cgo solo se usa para el detector de carreras.

**No hay trabajo de deploy.** Dokploy despliega en cada push a `main`. El control está
en el merge, con una branch protection que exige estos checks.
