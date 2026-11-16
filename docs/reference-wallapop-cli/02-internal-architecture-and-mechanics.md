# Wallapop CLI: Arquitectura Interna y Mecánica Técnica

Este documento analiza en profundidad el funcionamiento interno, patrones de diseño, gestión de red, protección anti-bot, persistencia y protocolos de [`wallapop-cli`](https://github.com/Microck/wallapop-cli).

---

## 1. Stack Tecnológico y Principios de Diseño

El proyecto está construido en **Go** (versión 1.25) con una política estricta de dependencias mínimas y zero-CGO:

| Componente | Librería / Mecanismo | Razón técnica |
|---|---|---|
| **CLI Framework** | `github.com/spf13/cobra` + `pflag` | Estándar de facto en la industria para CLI complejos con subcomandos anidados y autocompletado de shells. |
| **Persistencia Local** | `modernc.org/sqlite` | Base de datos SQLite pura en Go (sin dependencias CGO ni necesidad de GCC para compilar). Portabilidad cruzada total. |
| **Configuración** | `github.com/pelletier/go-toml/v2` | Parser TOML estricto y tipado; rechaza claves desconocidas para evitar errores silenciosos en la config. |
| **Ubicaciones del Sistema** | `github.com/adrg/xdg` | Sigue rigurosamente el estándar XDG Base Directory en Linux, macOS y Windows. |
| **Formato para LLMs** | `github.com/toon-format/toon-go` | Formato TOON (*Token-Oriented Object Notation*) para reducir drásticamente el uso de tokens al alimentar salidas a agentes IA. |
| **Gestión de Sesiones** | `github.com/Microck/galleton` | Demonio/SDK desacoplado para almacenar, proteger y renovar sesiones en segundo plano. |
| **HTTP & Protocolos** | Estándar `net/http` | Sin frameworks HTTP pesados, control granular de Timeouts, User-Agents, Proxies y Headers. Sin SDK externo de PubNub. |

---

## 2. Mapa Arquitectónico de Capas

```
                            [ Terminal / Scripts / Agentes IA ]
                                           │
                                           ▼
                               ┌───────────────────────┐
                               │     cmd/wallapop      │  (main.go entrypoint)
                               └───────────┬───────────┘
                                           │
                                           ▼
                               ┌───────────────────────┐
                               │     internal/cli      │  (Cobra command tree, flags,
                               └───────┬────────┬──────┘   interactive wizard, setup)
                                       │        │
               ┌───────────────────────┘        └────────────────────────┐
               ▼                                                         ▼
     ┌───────────────────┐                                     ┌───────────────────┐
     │   internal/mcp    │                                     │  internal/output  │
     │  (MCP JSON-RPC)   │                                     │(Formatters: json, │
     └─────────┬─────────┘                                     │ pretty, toon, err)│
               │                                               └───────────────────┘
               ▼
     ┌───────────────────┐
     │ internal/wallapop │  <── Cliente API HTTP, REST endpoints, PubNub polling,
     └─────────┬─────────┘      mecanismo de reintentos y anti-bloqueo WAF
               │
      ┌────────┴────────┬─────────────────────┬────────────────────┐
      ▼                 ▼                     ▼                    ▼
┌───────────┐     ┌───────────┐         ┌───────────┐        ┌───────────┐
│ internal/ │     │ internal/ │         │ internal/ │        │ internal/ │
│  galleton │     │   store   │         │   watch   │        │   sink    │
│ (Vault &  │     │  (SQLite  │         │ (Diffing  │        │ (ntfy,    │
│  Daemon)  │     │ state.db) │         │  engine)  │        │ webhook)  │
└───────────┘     └───────────┘         └───────────┘        └───────────┘
```

---

## 3. Modelo de Autenticación y Token Minting

### 3.1 Anatomía de la Sesión Web de Wallapop
Wallapop autentica usuarios mediante NextAuth con Keycloak:
1. En el navegador del usuario existe una cookie `HttpOnly` y `Secure` llamada `__Secure-next-auth.session-token`.
2. El frontend web de Wallapop llama periódicamente a `GET https://es.wallapop.com/api/auth/session` enviando esta cookie.
3. El endpoint responde con un JSON que contiene un JWT de corta duración (`token`), emitido por Keycloak, con una vida útil de **5 minutos**.
4. Cada llamada a la API privada (`https://api.wallapop.com/...`) debe incluir la cabecera `Authorization: Bearer <token>` y `X-DeviceOS: 0`.

### 3.2 El Motor de Sesiones Galleton (`internal/galleton`)
Para evitar almacenar contraseñas ni pedir inicios de sesión que requieran resolver captchas o MFA en terminal:
- El usuario exporta su cookie una sola vez (`wallapop auth login`).
- La cookie se guarda en una bóveda privada cifrada (`$XDG_CONFIG_HOME/wallapop-cli/galleton/`), protegida con permisos estrictos `0600` / `0700`.
- **Arquitectura de Demonio Efímero / Servicio**:
  - Por defecto, un proceso ligero en segundo plano gestiona las renovaciones mientras haya clientes activos (usando leases de proceso).
  - Emite tokens Bearer en memoria según se necesiten. Los tokens Bearer caducan en 5 minutos y **nunca** se escriben en disco.
  - Si el usuario habilita el servicio (`wallapop auth service enable`), se registra a nivel del sistema operativo para renovar la cookie antes de que expire (30 días).

---

## 4. Resiliencia de Red y Evasión Anti-Bot

Wallapop opera detrás del CDN y WAF de **CloudFront**, que aplica estrictas políticas de filtrado de bots. El cliente (`internal/wallapop/client.go`) implementa medidas de protección específicas:

### 4.1 Identificación y User-Agent Dual
1. **User-Agent Honesto**: Por defecto envía `wallapop-cli/<version> (+https://github.com/Microck/wallapop-cli)`.
2. **Fallback automático**: Si CloudFront rechaza la petición con un `403 Forbidden` que contiene páginas HTML típicas de CloudFront (*"The request could not be satisfied"*):
   - El cliente cambia dinámicamente a un User-Agent realista de navegador de escritorio (`Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36...`).
   - Reintenta la petición una vez. Si funciona, mantiene el User-Agent de navegador durante el resto de la vida del proceso.
   - Emite una notificación informativa por `stderr`.

### 4.2 Detección de Bloqueos y Pausa de Enfriamiento (*Cooldown*)
- Si la respuesta es un HTTP `429 Too Many Requests` o un `403` con cuerpo HTML (no JSON):
  - El cliente detecta un bloqueo por protección de bots (`isBotBlock`).
  - Llama inmediatamente a `c.reportBlock()`, ordenando al motor Galleton **pausar todas las renovaciones y peticiones durante 15 minutos** (`paused_until`).
  - Esto evita insistir y provocar un baneo permanente de la dirección IP.
  - El comando aborta inmediatamente con **Código de Salida 5** informando al usuario o agente del tiempo de espera requerido.

### 4.3 Política Estricta de Proxies
- Soportado vía `--proxy` o variable de entorno `WALLAPOP_PROXY` (`socks5h://...`, `http://...`).
- **Garantía de no fuga**: Si se especifica un proxy y este está caído o inaccesible, el comando falla de inmediato con **Código de Salida 6**. **Bajo ninguna circunstancia** cae en una conexión directa por seguridad y privacidad del usuario.

---

## 5. Contrato de Entrada/Salida (I/O) y Formateo

Para ser 100% interoperable con humanos, tuberías Unix (`jq`, `grep`) y agentes LLM:

### 5.1 Separación Estricta de Flujos
- **`stdout`**: **Solo datos puros**.
  - No se imprimen cabeceras decorativas, barras de progreso ni mensajes de saludo.
  - Un único documento JSON válido por invocación (o líneas JSON en modo `jsonl`).
- **`stderr`**: **Diagnósticos, avisos y errores**.
  - Logs de depuración (`--debug`).
  - Mensajes de progreso o avisos del sistema.
  - Envoltorio de error estructurado o legible.
  - Prompts de confirmación interactiva.

### 5.2 Formatos de Salida (`--format`)
- **`json`** (predeterminado): JSON formateado/indentado estándar, ideal para inspección y parseo con `jq`.
- **`jsonl`**: JSON Lines (un objeto JSON independiente por línea). Obligatorio para eventos en streaming (`watch check`, `chat open`).
- **`pretty`**: Tablas alineadas mediante `text/tabwriter`, con encabezados resaltados en negrita (si hay TTY y soporte ANSI), valores truncados con elipsis limpia y sin caracteres gráficos pesados de cajas.
- **`toon`**: Serialización de datos orientada a tokens, que preserva la estructura minimizando el uso de delimitadores y comillas redundantes para consumo ultra-eficiente por LLMs.

### 5.3 Redacción Estricta de Secretos en Logs
- En modo `--debug`, el cliente vuelca las peticiones y respuestas HTTP completas en `stderr`.
- El cliente mantiene una lista de cadenas sensibles registradas (`c.redactStrings`).
- Todo token bearer, cookie de sesión o ID de dispositivo se reemplaza automáticamente por `[REDACTED]` antes de emitirse a `stderr`.

---

## 6. Sistema de Errores y Códigos de Salida

### 6.1 Catálogo Semántico de Códigos de Salida

| Código | Significado | Criterio de uso |
|---|---|---|
| **0** | Éxito | Operación completada con éxito (incluye "no hubo cambios"). |
| **1** | Error genérico | Fallos no clasificados o errores internos inesperados. |
| **2** | Error de uso (*Usage*) | Argumentos inválidos, flags desconocidas, coordenadas ausentes, prompt cancelado o `--no-input` violado. |
| **3** | Error de autenticación | Sesión no configurada, cookie expirada o token rechazado por el servidor. |
| **4** | No encontrado (*Not Found*) | El producto, usuario, conversación o vigilancia no existe (HTTP 404/410). |
| **5** | Bloqueado / Rate Limit | CloudFront 403 HTML, HTTP 429 o sesión bajo pausa preventiva. Requiere enfriamiento. |
| **6** | Error de red / Timeout | Imposibilidad de contactar con el host, timeout superado o proxy inaccesible. |
| **7** | API Changed | El endpoint respondió con una estructura inesperada o un código no manejado. Indica que la plataforma web cambió su API privada. |
| **130** | Interrupción | Cancelación por señal de usuario (`Ctrl+C` / SIGINT). |

### 6.2 Envoltorio de Error JSON (`--error-format json`)
Para herramientas automatizadas y agentes IA, los errores en `stderr` adoptan esta forma canónica:
```json
{
  "code": 7,
  "category": "api_changed",
  "retryable": false,
  "message": "wallapop returned a response the cli does not understand",
  "http_status": 200,
  "endpoint": "GET /api/v3/general/search",
  "suggested_commands": ["wallapop doctor", "rerun with --debug"],
  "issue_url": "https://github.com/Microck/wallapop-cli/issues/new?template=api-change.yml&..."
}
```

### 6.3 Mecanismo de Issue Prefill ante Cambios de API (Exit 7)
Cuando la API devuelve un esquema que no valida:
- La herramienta genera una URL que prellena automáticamente una plantilla de issue en GitHub.
- Incluye: versión del CLI, arquitectura y SO, comando ejecutado (sin valores privados de argumentos), endpoint HTTP, código de respuesta y un extracto de los primeros 300 caracteres del cuerpo con datos sensibles redactados.

---

## 7. Motor de Watches y Persistencia de Estado (`internal/store`, `internal/watch`)

### 7.1 Esquema de Datos en SQLite
- El archivo `state.db` se almacena en `$XDG_DATA_HOME/wallapop-cli/state.db`.
- Tablas principales:
  - `watches`: id, target_type (`search`, `item`, `seller`), query, filtros serializados, nombre, sink_name, última comprobación.
  - `seen_items`: relación de artículos observados en la última instantánea para cada watch (almacena hash, precio, estado, fecha).
  - `events`: historial de eventos emitidos (tipo de evento, watch_id, timestamp, payload del item en JSON, diff del cambio en JSON).

### 7.2 Algoritmo de Detección de Cambios (Diffing)
- En cada ejecución de `watch check`:
  1. Realiza la llamada de búsqueda o detalle a la API.
  2. Carga los elementos previos registrados en `seen_items`.
  3. Compara elemento a elemento:
     - Si un item no existía: emite `item.new` (o `seller.new_item`).
     - Si el precio cambió: emite `item.price_changed` registrando `{from, to}`.
     - Si el campo `reserved` cambió: emite `item.reserved` o `item.unreserved`.
     - Si el campo `sold` cambió: emite `item.sold`.
     - Si un item de la instantánea anterior ya no aparece en el feed: emite `item.removed`.
  4. **Entrega antes de comprometer (*Crash-only design*)**: Los eventos se envían a los Sinks configurados. Solo después de una entrega exitosa se actualiza el estado en SQLite. Si el proceso muere a la mitad, la próxima ejecución reemitirá en lugar de perder cambios silenciosamente.

---

## 8. Arquitectura del Servidor MCP (`internal/mcp`)

- Implementación directa del protocolo JSON-RPC 2.0 sobre `stdin`/`stdout`.
- **Patrón Command-Bridge**: En lugar de replicar lógica de negocio, el servidor MCP ejecuta el mismo runner interno del CLI inyectando los argumentos sintetizados en un slice `argv[]` y configurando `--format json` y `--error-format json`.
- Captura la salida y devuelve un `CallToolResult` estándar de MCP con bloques de texto y señalización `isError`.
- **Validación de Esquema Estricta**: Cada herramienta define su JSON Schema. Parámetros nulos o desconocidos se rechazan a nivel RPC antes de llamar al CLI para que el modelo aprenda el error inmediatamente.
