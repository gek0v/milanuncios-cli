<p align="center">
  <img src="assets/icon.svg" width="96" height="96" alt="milanuncios-cli icon">
</p>

<h1 align="center">milanuncios-cli</h1>

<p align="center">
  <strong>Cliente de terminal autónomo y servidor MCP para el marketplace de Milanuncios.</strong><br>
  Diseñado tanto para uso ergonómico por personas en terminal como para agentes de IA (Claude Code, Cursor, Codex).
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License: MIT"></a>
  <a href="go.mod"><img src="https://img.shields.io/badge/go-1.25+-00ADD8.svg" alt="Go Version"></a>
  <a href="https://github.com/gek0v/milanuncios-cli/actions/workflows/ci.yml"><img src="https://github.com/gek0v/milanuncios-cli/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
</p>

---

## ⚡ Características Principales

- **Dual Audience**: Salida visual tabular (`--format pretty`) para personas y JSON estructurado directo por `stdout` para pipelines y Agentes de IA.
- **Protocolo MCP Integrado**: Servidor nativo JSON-RPC 2.0 stdio (`milanuncios mcp`) para herramientas autónomas en Claude Code, Cursor y Codex.
- **Búsqueda Avanzada**: Búsquedas por palabras clave con filtros de precio mínimo/máximo, provincia, categoría, ordenación y atributos dinámicos (`--filter clave=valor`).
- **Inspección de Anuncios**: Detalles completos de artículos (`ad show`), galería de fotos (`ad images`) y apertura directa en navegador (`ad open`).
- **Autenticación Web Segura**: Bóveda de credenciales local `credentials.toml` aislada con permisos POSIX `0600` e importación sencilla de cookies.
- **Monitorización Local (Zero-CGO)**: Motor de persistencia SQLite integrado (`modernc.org/sqlite`) para vigilar búsquedas o anuncios con detección de novedades, bajadas de precio y retirada de anuncios (`watch`).
- **Canales de Notificación**: Integración con notificaciones push vía `ntfy.sh`, webhooks HTTP (Discord, Slack, Zapier) y ejecución de comandos locales (`exec`).
- **Mensajería**: Consulta de hilos de chat y envío de mensajes (`chat list`, `chat show`, `chat send`).
- **Códigos de Salida Semánticos**: Códigos POSIX claros (`0` OK, `2` Usage, `3` Auth, `4` NotFound, `5` WAF Blocked, `7` API Changed con URL prefijada de GitHub issue).

---

## 📥 Instalación

### Desde releases binarios (Recomendado)
Descarga el binario precompilado para tu sistema operativo (Linux, macOS, Windows en AMD64 y ARM64) desde la sección de [Releases de GitHub](https://github.com/gek0v/milanuncios-cli/releases).

### Con Go (1.25+)
```bash
go install github.com/gek0v/milanuncios-cli/cmd/milanuncios@latest
```

### Compilar desde el código fuente
```bash
git clone https://github.com/gek0v/milanuncios-cli.git
cd milanuncios-cli
make build
# Binario generado en ./bin/milanuncios
```

---

## 🚀 Guía Rápida de Uso

### 1. Búsqueda y Navegación
```bash
# Búsqueda con salida tabular en consola
milanuncios search "bicicleta carretera" --min-price 300 --max-price 800 --province Valencia --format pretty

# Salida JSON lista para procesar con jq
milanuncios search "portatil thinkpad" --max-price 400 | jq '.items[].title'

# Explorar categorías y sus filtros específicos
milanuncios category list
milanuncios search filters --category 10

# Búsqueda con filtros dinámicos específicos de categoría
milanuncios search "coche" --category 11 --filter combustible=diesel --filter cambio=automatico
```

### 2. Inspección de Anuncios
```bash
# Ver información detallada de un anuncio
milanuncios ad show 492817294

# Listar enlaces directos a todas sus fotografías
milanuncios ad images 492817294

# Abrir el anuncio en tu navegador web predeterminado
milanuncios ad open 492817294
```

### 3. Autenticación y Favoritos
```bash
# Iniciar sesión importando cookies desde tu navegador
milanuncios auth login --cookies cookies.json

# Comprobar estado de la sesión activa
milanuncios auth status --check

# Ver tu perfil de usuario y anuncios favoritos
milanuncios me show
milanuncios me favorites
milanuncios ad favorite 492817294
```

### 4. Monitorización de Búsquedas y Anuncios (`watch`)
```bash
# Añadir una búsqueda para monitorizar cada 5 minutos
milanuncios watch add search "macbook air m1" --name macbook-m1 --interval 5m --max-price 600

# Añadir seguimiento de precio a un anuncio específico
milanuncios watch add ad 492817294 --name control-bici --interval 10m

# Listar todos los monitores activos
milanuncios watch list

# Ejecutar una comprobación manual inmediata
milanuncios watch check --all

# Iniciar demonio de monitorización periódica en segundo plano
milanuncios watch run --interval 3m

# Consultar eventos registrados (nuevos anuncios, bajadas de precio, retirados)
milanuncios watch events
```

### 5. Canales de Notificación (`sinks`)
Configura tus alertas en `~/.config/milanuncios-cli/config.toml`:

```toml
[sinks.movil]
type = "ntfy"
topic = "mis-alertas-milanuncios"

[sinks.discord]
type = "webhook"
url = "https://discord.com/api/webhooks/..."
```

Prueba el envío inmediato de notificaciones:
```bash
milanuncios sink list
milanuncios sink test movil
```

### 6. Mensajería y Chat
```bash
# Ver hilos de conversación activos
milanuncios chat list

# Leer mensajes de una conversación
milanuncios chat show <conversation-id>

# Enviar mensaje
milanuncios chat send <conversation-id> "Hola, ¿sigue disponible?"
```

---

## 🤖 Integración con Agentes de IA (MCP)

`milanuncios-cli` incluye un servidor stdio [Model Context Protocol](https://modelcontextprotocol.io/) listo para producción.

### Autoinstalación en asistentes de código
```bash
# Para Claude Code
milanuncios mcp install claude-code

# Para Cursor
milanuncios mcp install cursor

# Para OpenAI Codex
milanuncios mcp install codex
```

### Herramientas MCP expuestas
| Herramienta | Descripción |
|---|---|
| `milanuncios_search` | Búsqueda con filtros de precio, ubicación y categoría |
| `milanuncios_get_ad` | Información completa de cualquier anuncio por ID o URL |
| `milanuncios_categories` | Taxonomía jerárquica de categorías |
| `milanuncios_category_filters` | Catálogo de filtros dinámicos disponibles |
| `milanuncios_me` | Datos de perfil de usuario autenticado |
| `milanuncios_favorites` | Anuncios guardados como favoritos |
| `milanuncios_watch_list` | Lista de monitores locales |
| `milanuncios_watch_check` | Chequeo manual de diffing de precios y novedades |
| `milanuncios_watch_events` | Consulta de eventos recientes |
| `milanuncios_chat_list` | Conversaciones activas de chat |
| `milanuncios_chat_show` | Historial de mensajes de un hilo |
| `milanuncios_chat_send` | Envío de mensajes de chat |

Para consultar la guía interna que se proporciona a los LLMs:
```bash
milanuncios skills get milanuncios-usage
```

---

## 📋 Códigos de Salida Semánticos

| Código | Significado | Descripción |
|---|---|---|
| `0` | **Success** | Ejecución correcta |
| `1` | **Generic** | Error interno o inesperado |
| `2` | **Usage** | Error en flags, argumentos o validación |
| `3` | **Auth** | No autenticado o sesión expirada |
| `4` | **NotFound** | Anuncio, categoría, monitor o sink no encontrado |
| `5` | **Blocked** | Bloqueo por WAF / rate limit (activa enfriamiento de 15 min) |
| `6` | **Network** | Fallo de conexión o DNS |
| `7` | **APIChanged** | Cambio en API upstream (incluye URL prellenada para abrir issue) |
| `130` | **Interrupted** | Interrupción por el usuario (`SIGINT`/`Ctrl+C`) |

---

## 📖 Referencia y Documentación

- [**Guía de Comandos**](docs/commands/): Referencia completa autogenerada de cada comando y flag.
- [**Pautas para Agentes (`AGENTS.md`)**](AGENTS.md): Reglas de arquitectura, invariantes y diseño para asistentes de IA.
- [**Guía de Contribución**](CONTRIBUTING.md): Instrucciones para desarrollo y pull requests.
- [**Política de Seguridad**](SECURITY.md): Procedimiento de reporte responsable de vulnerabilidades.

---

## ⚖️ Descargo de Responsabilidad (*Disclaimer*)

Este proyecto es una herramienta comunitaria independiente no oficial y no está afiliada, asociada ni avalada por Milanuncios ni por Adevinta Spain. Utiliza este software de forma ética y responsable respetando los términos de servicio de la plataforma.

## 📄 Licencia

Este proyecto está distribuido bajo la licencia [MIT](LICENSE).
