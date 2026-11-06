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
   - «Anotar compras y ordenar tus movimientos» (`write`) lo decide la persona. Se ofrece
     aunque el cliente haya pedido solo `read`: un cliente MCP pide lo mínimo que anuncia
     el endpoint, y si eso fuera un techo, escribir nunca se podría conceder.
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
herramientas que escriben (llegan en el PR del MCP) van directas, con una regla: **nunca
mueven un saldo**.

- **Anotar una compra, poner una categoría o una nota** no tocan ningún saldo (ADR 0002)
  y se deshacen.
- **Importar un estado es la única que puede mover un saldo.** El saldo sale del archivo
  del banco, leído por el mismo parser determinista de siempre, nunca de un número que
  escriba Claude. Además, solo entra solo si continúa una cuenta que ya existe y cuadra
  con su último saldo conocido. Si no, queda para revisar en la app. Por MCP nunca se
  crea una cuenta.
- **Siguen como propuesta**, para aplicar en la web, las reglas, las transferencias y el
  saldo declarado: tocan el ancla del saldo o reescriben en lote.

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
