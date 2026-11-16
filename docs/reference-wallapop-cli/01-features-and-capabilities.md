# Wallapop CLI: Extracción Exhaustiva de Funcionalidades y Capacidades

Este documento desglosa en profundidad todas las capacidades funcionales ofrecidas por [`wallapop-cli`](https://github.com/Microck/wallapop-cli), extraídas directamente de su especificación técnica, código fuente Go y documentación en producción.

---

## 1. Filosofía y Enfoque del Proyecto

`wallapop-cli` está diseñado bajo un paradigma dual:
1. **Humano en terminal**: Proporciona comandos ergonómicos, salida con formato enriquecido (`pretty`) con colores y tablas legibles, atajos para navegador y comandos de confirmación interactiva.
2. **Agente de IA / Automatización**: Formatos estructurados nativos (`json`, `jsonl` y `toon`), contratos estrictos de entrada/salida (stdout para datos puros, stderr para diagnósticos y errores), códigos de salida semánticos, compatibilidad nativa con MCP (*Model Context Protocol*), y un skill embebido (`wallapop skills get wallapop-usage`) para autodescripción de capacidades a LLMs.

---

## 2. Catálogo Completo de Comandos y Capacidades

### 2.1 Autenticación y Gestión de Sesiones (`auth`)

Wallapop no cuenta con API pública con login por usuario/contraseña para compradores. Utiliza Keycloak tras NextAuth (`__Secure-next-auth.session-token`).

- **`wallapop auth login`**:
  - Asistente interactivo paso a paso para importar cookies desde el navegador (formato exportado por extensiones como *Cookie-Editor*, *EditThisCookie*, cabecera HTTP `Cookie:` o devtools).
  - Admite `--cookies <archivo>` para automatización o agentes.
  - Guarda la sesión cifrada mediante el motor desacoplado **Galleton** en una bóveda local privada (`$XDG_CONFIG_HOME/wallapop-cli/galleton/`).
  - Pregunta al usuario si desea mantener la sesión viva en segundo plano mediante un servicio del sistema (`--service` / `--no-service`).
- **`wallapop auth status`**:
  - Inspecciona el estado de la sesión local offline.
  - Con `--check`: verifica activamente contra la API de Wallapop si el token bearer es válido y utilizable.
- **`wallapop auth refresh`**:
  - Solicita explícitamente al motor la renovación anticipada de la sesión y emite un nuevo token.
- **`wallapop auth logout`**:
  - Invalida y elimina la sesión del perfil activo de forma segura tanto en la bóveda como en la configuración.
- **`wallapop auth service {enable|disable|status|run|stop}`**:
  - Controla el demonio/servicio de renovación en segundo plano.
  - `enable`: Registra una unidad systemd de usuario (Linux), un LaunchAgent (macOS) o una tarea programada interactiva (Windows).
  - `run`: Ejecuta el motor en primer plano para entornos en contenedores o supervisores externos.
  - `status`: Muestra métricas del demonio, leases activas, si está bajo proxy y si se encuentra pausado tras un bloqueo WAF (`paused_until`).
- **Bypass para CI**:
  - Variable de entorno `WALLAPOP_SESSION_TOKEN`: permite inyectar la cookie directamente en ejecución sin persistir credenciales en disco.

---

### 2.2 Gestión de Perfiles (`profile`)

Permite gestionar múltiples cuentas de Wallapop de forma aislada e independiente en la misma máquina.

- **`wallapop profile list`**: Enumera los perfiles configurados, indicando cuál es el predeterminado y el estado de su sesión.
- **`wallapop profile use <nombre>`**: Establece el perfil predeterminado para comandos futuros.
- **`wallapop profile remove <nombre>`**: Elimina las credenciales y configuración del perfil especificado.
- **Flag global `--profile <nombre>`** (o env `WALLAPOP_PROFILE`): Ejecuta cualquier comando bajo el contexto del perfil indicado sin modificar el predeterminado.

---

### 2.3 Búsqueda y Navegación (`search`, `category`)

- **`wallapop search [keywords]`**:
  - Búsqueda textual sobre el catálogo de productos.
  - **Geolocalización**: Toda búsqueda requiere un centro (latitud, longitud y radio). Si no se indican banderas explícitas (`--lat`, `--lng`, `--radius`), se extraen automáticamente de la ubicación guardada en el perfil del usuario autenticado.
  - **Filtros genéricos**:
    - `--min-price <num>`, `--max-price <num>`.
    - `--distance <km>`.
    - `--category <id>`.
    - `--condition <nuevo|como_nuevo|bueno|en_buen_estado|lo_ha_dado_todo>` (repetible).
    - `--sort <newest|price_asc|price_desc|distance>`.
    - `--limit <num>` (tamaño de página).
    - `--next <cursor>`: Paginación fluida recibiendo el `next_page` emitido en la llamada anterior.
  - **Filtros específicos por categoría (`--filter clave=valor`)**:
    - Pares dinámicos (ej: `--filter brand=Toyota --filter max_km=120000` en motor).
- **`wallapop search filters --category <id>`**:
  - Inspecciona y devuelve el catálogo de filtros válidos, tipos y valores permitidos para una categoría específica.
- **`wallapop category list`**:
  - Lista la taxonomía de categorías de Wallapop con sus IDs numéricos correspondientes.

---

### 2.4 Anuncios y Acciones de Vendedor (`item`)

- **Lectura e Inspección**:
  - **`wallapop item show <hash|url>`**: Obtiene la ficha completa del producto (título, descripción, precio, divisa, estado, fotos, vendedor, dimensiones, opciones de envío, fecha de publicación y modificación).
  - **`wallapop item images <hash|url>`**: Devuelve las URLs directas de alta resolución de todas las imágenes del artículo.
  - **`wallapop item open <hash|url>`**: Abre la URL pública del producto en el navegador predeterminado del sistema operativo.
- **Acciones Sociales / Comprador**:
  - **`wallapop item favorite <hash|url>`**: Marca el producto como favorito en la cuenta.
  - **`wallapop item unfavorite <hash|url>`**: Desmarca el producto de favoritos.
- **Acciones de Vendedor (anuncios propios)**:
  - **`wallapop item reserve <hash>`**: Marca un producto propio como reservado (o quita la reserva con `--off`).
  - **`wallapop item sold <hash>`**: Marca el producto como vendido (requiere confirmación interactiva o flag `--yes`).
  - **`wallapop item delete <hash>`**: Elimina permanentemente el anuncio (requiere confirmación interactiva o flag `--yes`).
  - **`wallapop item reactivate <hash|url>`**: Reactiva/republica un anuncio propio que haya expirado.

---

### 2.5 Usuarios y Cuenta Propia (`user`, `me`)

- **`wallapop user show <user_hash|url|slug>`**: Muestra el perfil público de un vendedor (nombre, puntuación, tiempo de respuesta, verificación).
- **`wallapop user items <user_hash|url|slug>`**: Lista todos los artículos actualmente a la venta de dicho vendedor.
- **`wallapop user reviews <user_hash|url|slug>`**: Lista las valoraciones y comentarios recibidos por el vendedor.
- **`wallapop me show`**: Detalles de la cuenta actualmente conectada en el perfil activo.
- **`wallapop me items [--sold]`**: Lista los productos publicados por el usuario conectado (activos o vendidos).
- **`wallapop me favorites`**: Lista todos los productos marcados como favoritos por la cuenta.

---

### 2.6 Chat y Mensajería (`chat`)

Wallapop implementa mensajería en tiempo real soportada sobre PubNub y API REST.

- **`wallapop chat list`**:
  - Lista las conversaciones activas, ordenadas de más reciente a más antigua.
  - Flags: `--unread` (solo conversaciones con mensajes no leídos), `--archived` (conversaciones archivadas), `--limit <n>`.
- **`wallapop chat show <conv_hash|item_hash>`**:
  - Muestra el historial completo de mensajes en orden cronológico.
  - Marca automáticamente los mensajes como leídos, salvo que se especifique `--no-mark-read`.
- **`wallapop chat send <conv_hash|item_hash> <mensaje>`**:
  - Envía un mensaje de texto a una conversación existente.
  - Si el mensaje es `-`, lee el cuerpo del mensaje desde `stdin` (útil para pipes o agentes).
- **`wallapop chat start <item_hash|url> <mensaje>`**:
  - Inicia una nueva conversación con el vendedor de un producto (o reusa la existente si ya se había hablado de ese producto) enviando el mensaje inicial.
- **`wallapop chat open <conv_hash>`**:
  - Abre un modo chat interactivo en la terminal por líneas en tiempo real mediante long-polling/suscripción PubNub. Con `--format jsonl`, emite los mensajes entrantes en streaming.
- **`wallapop chat archive <conv_hash> [--undo]`**:
  - Archiva o desarchiva una conversación.

---

### 2.7 Búsquedas Guardadas de Wallapop (`alert`)

- **`wallapop alert list`**:
  - Lista las búsquedas guardadas en la nube de Wallapop por el usuario (solo lectura).
- Permite vincularlas con el motor de monitoreo local con `wallapop watch add search --from-alert <id>`.

---

### 2.8 Monitoreo y Rastreo de Cambios (`watch`)

A diferencia de las alertas remotas de Wallapop, los **Watches** viven 100% en local en una base de datos SQLite (`state.db`).

- **Objetivos observables**:
  - `watch add search <keywords> [filtros...] --name <nombre>`: Vigila búsquedas periódicas.
  - `watch add item <hash|url> --name <nombre>`: Vigila cambios en un producto (precio, estado reservado/vendido).
  - `watch add seller <user> --name <nombre>`: Vigila si un vendedor sube nuevos artículos.
  - Flag `--emit-initial`: Reporta los artículos actuales en la primera ejecución en lugar de usarlos solo como línea base (*baseline*).
  - Flag `--notify <sink>`: Asocia la notificación a un canal externo configurado.
- **Ejecución y Eventos**:
  - **`wallapop watch check [nombres...] [--all]`**: Comprueba una o todas las vigilancias una vez, compara con la instantánea anterior, detecta diferencias, emite eventos y actualiza la base de datos localmente.
  - **`wallapop watch run [--interval <duración>]`**: Bucle continuo en primer plano ejecutando comprobaciones periódicas.
  - **`wallapop watch events [nombre] [--since <duración>]`**: Consulta el historial de eventos detectados guardados en SQLite.
  - **`wallapop watch list / remove`**: Administración de vigilancias guardadas.
- **Tipos de Eventos detectados**:
  - `item.new`: Nuevo artículo detectado en búsqueda.
  - `item.price_changed`: Cambio de precio (registra `from` y `to`).
  - `item.reserved` / `item.unreserved`: Cambio de estado de reserva.
  - `item.sold`: Artículo vendido.
  - `item.removed`: Artículo borrado o desaparecido.
  - `item.edited`: Modificaciones sustanciales en el anuncio.
  - `seller.new_item`: Nuevo artículo publicado por un vendedor vigilado.
- **Servicio en Segundo Plano**:
  - **`wallapop watch service {install|uninstall|status}`**: Configura un temporizador `systemd` a nivel de usuario en Linux o un `LaunchAgent` en macOS para ejecutar `watch check` desatendido.

---

### 2.9 Canales de Notificación (`sink`)

Permite canalizar los eventos detectados por `watch` fuera de la consola estándar:

- **Tipos soportados**:
  - **`ntfy`**: Notificaciones push a dispositivos móviles mediante servidores públicos o auto-alojados de [ntfy.sh](https://ntfy.sh).
  - **`webhook`**: Envío de payloads HTTP POST JSON hacia endpoints generales o plantillas predefinidas para **Discord** y **Slack**.
  - **`exec`**: Ejecución de un comando o script local arbitrario recibiendo el payload completo del evento en formato JSON por `stdin`.
- **Comandos**:
  - **`wallapop sink list`**: Muestra los sinks configurados en `config.toml`.
  - **`wallapop sink test <nombre>`**: Envía un evento sintético de prueba para validar la conectividad y formato del canal.

---

### 2.10 Integración con Agentes IA y Servidor MCP (`mcp`)

Implementa un servidor nativo de **Model Context Protocol (MCP)** mediante `stdio`:

- **`wallapop mcp`**: Inicia el servidor MCP JSON-RPC 2.0.
  - Las herramientas MCP llaman directamente a los comandos del CLI sin duplicar lógica.
  - **Catálogo de herramientas MCP disponibles**:
    - `search`
    - `item_show`
    - `user_show`
    - `watch_check`
    - `chat_list`, `chat_show`, `chat_send`, `chat_start` (en desarrollo/activables).
  - **Herramientas explícitamente excluidas por seguridad**:
    - Acciones destructivas o irreversibles de vendedor (`item_reserve`, `item_sold`, `item_delete`, `item_create`). Si un agente las solicita, el servidor devuelve un error explicativo indicando que deben ser realizadas por una persona en la terminal.
- **`wallapop mcp install {claude-code|codex|cursor} [--profile <nombre>]`**:
  - Registra automáticamente el servidor `wallapop` en la configuración del asistente (`~/.claude.json`, `~/.codex/config.toml`, `~/.cursor/mcp.json`) inyectando el binario absoluto y fijando el perfil.

---

### 2.11 Diagnóstico y Soporte (`doctor`)

- **`wallapop doctor`**:
  - Comprueba exhaustivamente la salud del entorno local:
    - Integridad sintáctica de `config.toml` y `credentials.toml`.
    - Validez de la sesión activa y tiempo restante de la cookie.
    - Conectividad con la API pública y privada de Wallapop.
    - Conexión del proxy configurado (si aplica).
    - Estado del demonio de sesión `galleton` y si se encuentra bajo pausa por WAF.
    - Estado de los servicios de sistema instalados (`auth service`, `watch service`).

---

### 2.12 Configuración (`config`)

- **`wallapop config path`**: Muestra la ruta física del archivo `config.toml`.
- **`wallapop config get <clave>`**: Consulta un valor específico de la configuración.
- **`wallapop config set <clave> <valor>`**: Modifica o agrega una clave de configuración.
- **`wallapop config list`**: Imprime el archivo TOML completo.

---

### 2.13 Auto-actualizaciones (`update`)

- **`wallapop update`**:
  - Comprueba y actualiza el binario a la última release de GitHub.
  - Respeta el gestor de paquetes por el cual fue instalado (Homebrew, Scoop, AUR, npm o binario directo con verificación de checksums).
  - Flags: `--check` (solo comprueba si hay nueva versión), `--yes` (actualiza sin confirmación interactiva).
  - Los comandos interactivos habituales realizan un chequeo pasivo máximo una vez al día; se desactiva con `WALLAPOP_NO_UPDATE_CHECK=1` o `--no-input`.
