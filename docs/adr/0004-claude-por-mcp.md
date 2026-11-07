# ADR 0004 — Claude entra a chenchén por MCP, con su propia llave

- **Estado:** aceptada
- **Fecha:** 2026-11-06

## Contexto

chenchén ya tiene un servidor MCP, pero solo dentro del proceso: el asistente de la app
lo usa para leer cuentas, movimientos y gastos, y para **proponer** cambios que la
persona aplica después en la web.

Ahora queremos que Claude (claude.ai, su app del teléfono y Claude Code) use esas
herramientas directamente, desde la conversación de la persona. Para eso el MCP tiene que
estar en internet, en `/api/mcp`, y la especificación de MCP pide OAuth 2.1 para
autorizarlo. chenchén hace los dos papeles a la vez:

- **Servidor de autorización.** El issuer es `PUBLIC_URL`, sin path.
- **Recurso protegido.** El recurso es `PUBLIC_URL` + `/api/mcp`, en forma canónica.
  Esa cadena es el `resource` que anuncia el descubrimiento y el destino de cada token.

Sin `PUBLIC_URL` no hay issuer que nombrar: ninguna ruta de OAuth ni de MCP se monta,
responden 404 y el resto de la app sigue igual.

## Decisión

### Solo dos clientes, fijos, sin registro

Se aceptan exactamente dos `client_id`, los documentos de metadatos de cliente (CIMD) de
Claude, cada uno con sus redirecciones exactas:

| cliente | `client_id` | a dónde vuelve |
|---|---|---|
| Claude | `https://claude.ai/oauth/mcp-oauth-client-metadata` | `https://claude.ai/api/mcp/auth_callback` o la misma en `claude.com` |
| Claude Code | `https://claude.ai/oauth/claude-code-client-metadata` | `http://localhost/callback` o `http://127.0.0.1/callback`, en cualquier puerto |

- **No hay registro dinámico ni tabla de clientes.** Un registro abierto le daría a
  cualquiera una forma de presentarse como app ante la persona. Con la lista fija, el
  único que puede pedir acceso es Claude.
- **Los documentos nunca se descargan.** Ya sabemos qué dicen. Descargarlos solo
  agregaría una dependencia de red, y el borde de claude.ai rechaza a veces esas
  descargas.
- **Un cliente o una redirección fuera de la lista nunca vuelve al cliente.** Mandar un
  código, o incluso un error, a una dirección que no es de Claude es justo lo que
  aprovecharía alguien que quiere robar el acceso. Como quien llega es el navegador de la
  persona, en vez de un 400 en JSON va a `/conectar?error=invalid_client` (o
  `invalid_redirect`), y la página explica qué pasó. Lo mismo con demasiados intentos
  desde una dirección: en vez de un 429, `/conectar?error=rate_limited`.

### El flujo

1. `GET /api/oauth/authorize` exige `response_type=code`, PKCE con `S256` y el `state`. El
   `resource` es opcional (RFC 8707): si no viene, es el único que hay. Si viene, se
   perdonan las mayúsculas del esquema y del host, el puerto por defecto y la barra final,
   y cualquier otro recurso se rechaza, así que un token nunca se pide para otro lugar.
2. La solicitud queda guardada 10 minutos y el navegador va a `/conectar`. Ahí la persona,
   con su sesión, ve quién pide acceso y a dónde vuelve, y decide.
   - «Ver tus cuentas, movimientos y gastos» (`read`) va siempre.
   - «Anotar compras, ordenar tus movimientos y subir los archivos de tu banco» (`write`)
     lo decide la persona, con un interruptor que empieza como lo pidió el cliente
     (encendido si pidió `write`). Se ofrece aunque el cliente haya pedido solo `read`:
     un cliente MCP pide lo mínimo que anuncia el endpoint, y si eso fuera un techo,
     escribir nunca se podría conceder.
   - La página va fuera del marco de la app, y la web nunca se deja mostrar dentro de
     otro sitio (`X-Frame-Options: DENY` y `frame-ancestors 'none'`): un clic en
     «Permitir» siempre es de la persona.
3. La solicitud se contesta una sola vez y solo la contesta quien la abrió primero. Una
   que nadie abrió no la contesta nadie: aprobar siempre viene después de ver qué se
   aprueba, nunca de un POST que otra página mandó con la sesión de la persona.
4. El código vale 60 segundos, se usa una vez y queda atado al cliente, a la redirección,
   al challenge, al recurso, a los permisos y a la persona. Si un código llega dos veces
   con la prueba de su cliente (el mismo `client_id`, la misma redirección y el verifier
   de PKCE), se filtró: se revoca lo que emitió la primera vez. Sin esa prueba solo se
   rechaza. El código solo, leído de un log, no alcanza para desconectar a la persona.
5. `POST /api/oauth/token` recibe un formulario y responde en los términos de RFC 6749,
   no con el sobre de errores de la API, porque quien lo lee es una librería de OAuth.

### Los tokens de Claude no son sesiones

- **Prefijos propios:** `chno_` para el acceso y `chnr_` para la renovación.
- **Tablas propias:** `oauth_grants` y `oauth_tokens`. Un refresh de Claude nunca está en
  `refresh_tokens`.
- **Solo se guarda el hash.** Un respaldo de la base no sirve para conectarse como Claude.
- **Opacos y revocables.** El acceso dura 1 hora. La renovación dura 60 días desde su
  último uso y rota cada vez que se usa.
- **Una renovación puede pedir menos.** Con `scope=read` en una concesión que también
  escribe, el acceso nuevo solo lee, y la respuesta lo dice. El refresh nuevo conserva
  todo lo concedido (RFC 6749 §6), así que pedir menos una vez no achica la conexión.
- **Repetir lo que pidió al conectarse no es pedir menos.** Cada concesión guarda los
  permisos que el cliente pidió además de los que se concedieron. Si una renovación
  repite exactamente lo que pidió al conectarse, recibe lo concedido, sea más (pidió
  leer y se le dejó escribir) o menos (pidió escribir y se le dejó solo leer). Leído al
  pie de la letra, el primer caso perdería la escritura una hora después de conectarse,
  y el segundo fallaría en cada renovación y pediría reconectar para siempre.

La separación funciona en los dos sentidos:

- **`auth.Require` rechaza por nombre** cualquier token `chno_` o `chnr_`. Si un token de
  Claude abriera la API de la web, Claude podría llamar a
  `POST /api/proposals/{id}/apply` y aplicar sus propias propuestas, y lo único que hoy
  separa proponer de aplicar es que aplica la persona.
- **El verificador del MCP solo acepta `chno_`.** Un JWT de la web, una clave de Atajos
  (`chn_`) o un refresh se rechazan por su forma, antes de buscar nada.

Cerrar la sesión en la web no desconecta a Claude, porque son credenciales distintas.
Para desconectarlo está Conexiones, que revoca la concesión y todos sus tokens.

### Sesenta segundos de gracia al renovar

claude.ai a veces manda la misma renovación varias veces a la vez (el error
anthropics/claude-ai-mcp#1194). Con una rotación estricta, la primera pasaría, las demás
parecerían un token robado y se revocaría la conexión que la persona acaba de hacer.

- **Dentro de los 60 s** después de que un refresh se usa, presentarlo otra vez pidiendo
  lo mismo devuelve **el mismo par nuevo**. Las peticiones que llegan juntas y piden lo
  mismo comparten una sola rotación. Antes de entrar, cada una se revisa por su cuenta
  contra la concesión (`client_id`, recurso y permisos), así que una petición mala nunca
  hace fallar a las buenas que llegaron con ella. Pedir otros permisos con un refresh que
  se acaba de usar se rechaza sin revocar nada: un solo acceso no puede llevar dos.
- **Ese par en claro vive solo en la memoria del proceso** durante ese minuto. Nunca va a
  la base, donde solo hay hashes.
- **Pasado ese minuto**, presentarlo otra vez solo puede ser una copia que alguien guardó.
  Se revoca toda la concesión: dejan de funcionar el par del ladrón y el de la persona,
  y la persona se conecta otra vez.

El precio es que, durante ese minuto, quien tenga una copia del refresh recibe el mismo
par que Claude. Se acepta porque la alternativa rompe la conexión real cada pocas horas.

### El asistente de adentro propone; Claude por MCP escribe, sin mover saldos

El asistente de la app sigue **solo proponiendo**. Por MCP es distinto: la persona está en
la conversación, y claude.ai le pide permiso en cada llamada que escribe. Por eso las
herramientas que escriben van directas, con una regla: **nunca mueven un saldo**.

- **Anotar una compra, poner una categoría o una nota** no tocan ningún saldo (ADR 0002)
  y se deshacen.
- **Guardar o quitar algo del plan del mes** tampoco (ADR 0005): el plan es lo que la
  persona espera, nunca un saldo, y se deshace guardando otra vez lo que había.
- **Importar un estado es la única que puede mover un saldo.** El saldo sale del archivo
  del banco, leído por el mismo parser determinista de siempre, nunca de un número que
  escriba Claude. Además, solo entra solo si continúa una cuenta que ya existe y cuadra
  con su último saldo conocido. Si no, queda para revisar en la app. Por MCP nunca se
  crea una cuenta.
- **Siguen como propuesta**, para aplicar en la web, las reglas, las transferencias y el
  saldo declarado: tocan el ancla del saldo o reescriben en lote.

### El endpoint `/api/mcp`

- Va fuera de `auth.Require`, como el Atajo. `auth.RequireBearerToken` revisa el token
  antes que nada: un JWT de la web, una clave del Atajo (`chn_`) o un refresh dan 401.
- El 401 dice dónde está la metadata del recurso (`resource_metadata`) y pide
  `scope="read write"`. El endpoint solo exige `read`, pero un cliente pide lo que dice el
  401: si dijera solo `read`, nunca se podría conceder escribir. La persona apaga escribir
  en la página de consentimiento si quiere.
- **Sin estado.** Cada petición arma su propio servidor con la persona y los permisos del
  token (`mcpserver.NewServer`) y responde en JSON, sin sesiones en memoria ni streams
  abiertos. Un caché compartido evita reflejar los esquemas en cada llamada.
- **Un límite por concesión**, de 60 llamadas por minuto, con 429 y `Retry-After`. Por
  dirección no sirve: todo claude.ai sale de las mismas pocas direcciones.
- **Auditoría.** Cada llamada a una herramienta deja una línea en el log con la
  herramienta, la concesión, la persona, cuánto tardó y si falló. Nunca sus argumentos ni
  su resultado: llevan la plata de la persona y, al entregar un archivo, su dirección.

### Las herramientas

| Con `read` | Con `write`, además |
|---|---|
| `list_accounts`, `list_movements`, `list_categories`, `list_rules` | `note_purchase` |
| `spending_by_category`, `cash_flow`, `explain_drift` | `set_category`, `set_note` |
| `list_transfer_suggestions`, `get_portfolio` | `prepare_import` |
| `list_pending_purchases`, `sync_plan`, `import_status` | `propose_rule`, `propose_transfer_decision`, `propose_checkpoint` |
| `get_month_plan` | `save_plan_item`, `remove_plan_item` |

- Una herramienta que el token no permite **no aparece** en `tools/list`, así que no se
  puede llamar.
- Todas llevan un título en español y dicen si solo leen o si escriben. Así el cliente
  corre las de lectura sin preguntar y pide permiso para las otras.
- Las instrucciones del servidor son una versión corta de las del asistente: qué es un
  saldo impreso, qué es una compra pendiente, que el texto de los movimientos es dato y
  nunca instrucción, que el plan del mes no es un saldo y que pasarse de lo libre se dice
  con la cifra y sin regañar, y cómo se sincroniza. Una conexión de solo lectura no recibe lo de
  sincronizar: se le dice que solo puede leer. No nombran ningún banco.
- `note_purchase` pasa por el mismo `captures.Service.Add` que la web, con el canal
  `agent`. **El id lo elige chenchén**, nunca quien llama. Si chenchén ya tenía la compra,
  la devuelve con `already_noted` y no cuenta dos veces: si llegó por otro canal, como
  siempre (ADR 0002), y también si el asistente ya la había anotado. Eso pasa cada vez que
  vuelve a leer las compras en tránsito del banco. La reconoce por la referencia del banco
  (`reference`), si la manda; si no, por el mismo monto, comercio y día en la cuenta. Así
  dos compras iguales del mismo día con referencias distintas son dos. Una categoría que
  manda queda como elegida por el asistente, nunca por la persona.
- `set_category` y `set_note` sirven igual para una línea del estado que para una compra
  pendiente. La categoría queda como elegida por el asistente.
- `propose_recategorize` y `propose_note` no están: por MCP eso se hace directo.
- `get_month_plan` dice lo mismo que `GET /api/plan` (ADR 0005), con los montos en dólares
  y las categorías y cuentas por su nombre; en el mes actual trae también lo que el
  historial sugiere planear. La tiene también el asistente de la app, que solo la lee.
- `save_plan_item` y `remove_plan_item` pasan por el mismo `plan.Service` que la web.
  **El id lo elige chenchén**, como en `note_purchase`: sin `item_id` se agrega algo
  nuevo, y si el plan ya lo tiene en los meses que tendría (del mismo tipo, con el mismo
  nombre sin contar mayúsculas, tildes ni espacios, y con el mismo día, texto y cuenta si
  los manda), lo devuelve con `already_planned` y no agrega otro. Buscar y agregar son un
  solo paso bajo el candado de la persona, como en `captures.Service.Add`, así que dos
  llamadas iguales a la vez agregan uno. El día y el texto separan lo que se llama igual:
  las dos quincenas de un sueldo, o los pagos a dos personas, son dos cosas. Con
  `item_id` cambia solo lo que manda, desde un mes o solo en ese mes; lo demás queda como
  estaba, así un cambio de monto no se lleva el texto ni la categoría que Claude no
  releyó. Sin mes, el cambio es desde este mes o, si lo planeado empieza después, desde
  su primer mes: un cambio que no dice cuándo no adelanta nada. Un mes que se quitó se
  vuelve a planear guardándolo con su `item_id` y ese mes. Los errores dicen en español
  qué corregir, campo por campo.
- `sync_plan` dice, por cada cuenta a la que llegan estados (no las de inversión ni las
  tarjetas que paga otra persona): desde qué día bajar (el último día importado menos 7,
  porque traslapar no duplica, o 60 días atrás si nunca se importó), sus compras
  pendientes y, en una tarjeta cuyo estado cerrado chenchén lee, si toca bajarlo (no hay
  ninguno, o el último corte tiene más de 31 días).

### Entregar un estado de cuenta, sin oráculo y sin crear cuentas

1. `prepare_import({filename, sha256, size})` devuelve `upload_id`, `upload_url`,
   `method: "PUT"` y `expires_at`.
2. El archivo va por `PUT /api/mcp/uploads/{token}`, sin `Authorization`: **la dirección
   es la credencial**. Son 32 bytes aleatorios, guardados con hash, de un solo uso, que
   vencen en 10 minutos y solo aceptan el archivo del tamaño y el SHA-256 declarados, de
   20 MB como mucho. Un cuerpo distinto da 400 y no se guarda nada; la dirección sigue
   sirviendo para el archivo correcto. Una dirección usada, vencida o inventada da 404.
   Cada persona tiene como mucho 20 direcciones esperando a la vez.
3. La respuesta es **siempre la misma**, `200 {"upload_id": …}`, haya entrado o no.
   Quien tenga la dirección no aprende nada de las cuentas. Lo que pasó lo dice
   `import_status`, con el token de Claude y solo a su persona.
4. El log de acceso registra el patrón de la ruta, nunca el token.

**Entra solo** si se cumple todo esto:

- Cada estado del archivo es de una cuenta que ya existe. Se busca por su identidad, sin
  crearla, y además un guardia dentro de la transacción del import la rechaza si el import
  la acaba de crear: el rollback la deshace. **Por MCP nunca se crea una cuenta**
  (`TestMCPUploadNeverCreatesAccounts`). La primera vez de una cuenta se importa en la app.
- Ninguna es una tarjeta que paga otra persona ni una cuenta de inversiones.
- El archivo no trae ni una línea donde los saldos dejan de cuadrar (`ChainBreak`) ni
  advertencias.
- Si el banco imprime el saldo inicial, es igual al saldo calculado de la cuenta al final
  del día anterior al primero del archivo, siempre que la cuenta tenga un saldo declarado
  del que derivarlo.
- Un estado sin saldo inicial, como los movimientos de una tarjeta, entra aunque la cuenta
  no tenga ninguno. Un import solo agrega saldos declarados, así que nunca deja sin saldo
  inicial a una cuenta que lo tenía.

Si entra, queda `imported`, o `unchanged` si ese contenido ya estaba, y el archivo no se
guarda. Si no, queda `needs_review` con una razón corta en español y el archivo guardado
para la persona, o `unreadable` si ningún lector lo reconoce. En la app, la persona ve la
lista (`GET /api/imports/uploads?status=needs_review`), el dry-run del archivo guardado, y
lo importa como cualquier archivo de la web (ahí sí se crea la cuenta la primera vez) o lo
descarta. Importado o descartado, el archivo se borra.

**Se lee un archivo a la vez** en el proceso. Un archivo espera su turno antes de gastar su
dirección, y sus 2 minutos para leerse empiezan cuando le toca: esperar nunca lo deja por
revisar. Los archivos que esperan su turno suman como mucho 20 MB en memoria, y la app
carga uno guardado solo cuando le toca leerlo. El cuerpo de un `PUT` tiene 5 minutos para
llegar. Un `.xlsx` que se descomprime a más de 64 MB
se rechaza antes de leerlo, y una parte de más de 16 MB se lee desde disco y no desde la
memoria. Esos límites valen también para los imports de la web.

**Retención.** Una dirección que no recibió su archivo vence a los 10 minutos. Un archivo
por revisar o ilegible se borra a los 30 días. Los dos quedan `expired`, y lo hace la
limpieza periódica.

## Consecuencias

- `PUBLIC_URL` queda vacía hasta que estén desplegados el endpoint del MCP y la página
  `/conectar`: antes, Claude encontraría cómo conectarse y no tendría a dónde. Al ponerla,
  como en producción la raíz del dominio va a la web, `/.well-known` tiene que llegar a la
  API por una regla propia del proxy.
- La gracia y los límites por dirección viven en memoria, como los demás límites de la
  API. Con más de una réplica, una renovación repetida que cae en otro proceso recibe
  `invalid_grant` sin revocar nada, y el cliente se queda con el par de la primera
  respuesta.
- Agregar otro cliente es un cambio de código, a propósito.
- El límite por dirección de `/authorize` y `/token` es generoso, porque todo claude.ai
  sale de pocas direcciones. El límite que importa, por concesión, va en el MCP.
- La dirección de esos límites, como la de todos los de la API, es la última entrada de
  `X-Forwarded-For`: la que agrega el único proxy que hay delante (Traefik en producción,
  el nginx de la web en local). Las anteriores las escribe el cliente.
- La limpieza periódica borra solicitudes, códigos y tokens vencidos, y las concesiones
  que se quedaron sin tokens. Un refresh ya usado se guarda hasta que habría vencido,
  porque es lo que reconoce una copia robada.
- La API corre con `GOMEMLIMIT=130MiB` dentro de sus 160 MiB: leer un archivo grande hace
  que el recolector trabaje antes de llegar al techo del contenedor.
- El límite por concesión vive en memoria, como los demás.
- Un archivo por revisar vive en la base hasta 30 días, así que los respaldos con
  `pg_dump` lo incluyen mientras exista.
