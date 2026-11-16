# Wallapop CLI: Estructura del Repositorio, Tooling y Ecosistema de Documentación

Este documento detalla la estructura física, archivos de gobernanza, estrategia de documentación, automatización y pipeline de CI/CD del repositorio [`wallapop-cli`](https://github.com/Microck/wallapop-cli).

---

## 1. Árbol de Directorios del Repositorio

```
wallapop-cli/
├── .github/
│   ├── ISSUE_TEMPLATE/
│   │   ├── api-change.yml        # Plantilla prellenada para reportar cambios en la API
│   │   └── bug_report.yml
│   └── workflows/
│       ├── ci.yml                # Pipeline principal: fmt, vet, staticcheck, tests, docs sync
│       ├── managed-sessions.yml  # Pruebas periódicas del ciclo de vida de sesiones
│       ├── pages.yml             # Despliegue de la web de documentación
│       ├── release.yml           # Compilación y publicación con GoReleaser
│       └── verify-release.yml    # Verificación de firmas y artefactos
├── cmd/
│   ├── wallapop/
│   │   └── main.go               # Punto de entrada principal del binario
│   ├── bundle-galleton/          # Generador para incrustar el demonio Galleton
│   └── docsgen/                  # Generador que extrae la referencia de docs desde el árbol Cobra
├── docs/                         # Especificaciones técnicas y arquitectura
│   ├── adr/                      # Architecture Decision Records
│   │   ├── 0001-session-from-web-cookie.md
│   │   ├── 0002-offers.md
│   │   └── 0003-cli-managed-galleton.md
│   ├── cli-spec.md               # Contrato formal completo del CLI y endpoints
│   ├── galleton.md               # Ciclo de vida de sesiones, bóveda y demonio
│   └── spec-wallapop-cli-v1.md   # Especificación de requisitos original v1
├── docs-site/                    # Web pública de documentación (wallapop.micr.dev)
│   ├── app/                      # Next.js App Router
│   │   ├── docs/                 # Páginas de documentación renderizadas con Fumadocs
│   │   └── llms.txt/             # Endpoint dinámico optimizado para LLMs
│   ├── content/docs/             # Documentos MDX fuente (guías, instalación, referencia)
│   ├── tests/                    # Tests E2E con Chromium
│   └── package.json
├── examples/                     # Archivos de configuración y plantillas de ejemplo
├── images/                       # Activos visuales, diagramas y GIFs de demostración
├── internal/                     # Paquetes privados de Go (código fuente)
│   ├── cli/                      # Comandos Cobra, parsing de flags y wizards
│   │   └── skills/               # Guías para agentes embebidas en el binario (go:embed)
│   ├── config/                   # Carga y serialización de config.toml y credentials.toml
│   ├── fakewallapop/             # Servidor HTTP fake con fixtures reales para testing
│   ├── filelock/                 # Locks de archivo multiplataforma (Unix / Windows)
│   ├── galleton/                 # Integración con el motor de sesiones en segundo plano
│   ├── mcp/                      # Servidor Model Context Protocol (JSON-RPC)
│   ├── output/                   # Formateadores (json, jsonl, pretty, toon) y errores
│   ├── sink/                     # Entrega de notificaciones (ntfy, webhook, exec)
│   ├── store/                    # Persistencia SQLite (state.db)
│   ├── updater/                  # Motor de auto-actualización multiplataforma
│   ├── wallapop/                 # Cliente de la API de Wallapop y tipos del dominio
│   └── watch/                    # Algoritmo de diffing y generación de eventos
├── npm/                          # Envoltorio npm para instalación vía `npx wallapop`
│   ├── cli.cjs                   # Script de descarga y ejecución del binario nativo
│   └── package.json
├── scripts/
│   └── install.sh                # Script de instalación rápida vía `curl -fsSL ... | sh`
├── .goreleaser.yaml              # Configuración de compilación cruzada y empaquetado
├── AGENTS.md                     # Documento de contexto y lenguaje formal para IA
├── go.mod / go.sum               # Definición del módulo y dependencias Go
├── Makefile                      # Automatización de tareas de desarrollo
└── README.md                     # Presentación principal del proyecto
```

---

## 2. Estrategia y Tipología de Documentación

### 2.1 README.md: Orientado al Usuario y Transparencia
- **Estructura**:
  1. Identidad clara: resumen de propuesta de valor en un párrafo.
  2. Quickstart directo y comandos de mayor impacto.
  3. Demostraciones visuales (*recordings / GIFs reales*) que muestran búsquedas, filtros e inspección.
  4. Tabla sinóptica de comandos y verbos disponibles.
  5. Tabla del modelo de autenticación (qué desbloquea la sesión y qué funciona sin login).
  6. Documentación de configuración de sinks (ntfy, discord, slack).
  7. Códigos de salida y convenciones de error.
  8. **Disclaimer legal explícito**: Declara la naturaleza no oficial del cliente, los términos de servicio de la plataforma y recomendaciones de uso responsable.

### 2.2 AGENTS.md: Alineación Semántica para Agentes IA
- Define el **lenguaje formal del dominio**:
  - Evitar palabras ambiguas (*"Listing, product, ad, post"* $\rightarrow$ Usar siempre **`Item`**).
  - *"Owner, vendor"* $\rightarrow$ Usar **`Seller`** o **`User`**.
  - *"Query"* $\rightarrow$ Usar **`Search`** (query + filtros + ubicación).
  - *"Tracker, monitor, alert"* $\rightarrow$ Usar **`Watch`** (local) vs **`Alert`** (guardado remoto en Wallapop).
  - *"Poll, tick, scan"* $\rightarrow$ Usar **`Check`**.
- Describe las entidades y sus invariantes para que los LLMs razonen con precisión sin alucinaciones.

### 2.3 `docs/cli-spec.md`: La Especificación Contractual
- Documento normativo que describe:
  - Todo endpoint HTTP consumido (métodos, URLs base, cabeceras, respuestas).
  - Flags globales y comportamiento según si el entorno es interactivo (TTY) o no interactivo.
  - Política de seguridad (confirmación obligatoria para borrados y ventas, redacción de secretos, etc.).

### 2.4 ADRs (*Architecture Decision Records*)
- Registran de forma inmutable el porqué de decisiones de diseño críticas:
  - `0001`: Por qué usar la cookie de sesión web y no login por email/password (MFA, proveedores OAuth externos, seguridad de contraseñas).
  - `0002`: Gestión de ofertas de precio y por qué no se automatiza la aceptación de pagos/envíos.
  - `0003`: Adopción del demonio desacoplado Galleton para el ciclo de vida de sesiones.

### 2.5 Generación Automatizada de la Referencia de Comandos (`cmd/docsgen`)
- La documentación de referencia no se escribe a mano para evitar desincronizaciones (*drift*).
- El comando `make docs` ejecuta `go run ./cmd/docsgen`, que recorre el árbol de comandos de Cobra y genera automáticamente los archivos MDX en `docs-site/content/docs/reference/`.
- CI ejecuta `make docs-check`, que falla si alguien modificó un flag o comando en el código sin regenerar la documentación.

---

## 3. Tooling y Automatización: El Makefile

El repositorio proporciona un flujo de trabajo simplificado y riguroso:

```makefile
.PHONY: build test check fmt vet lint install docs docs-check

build:
	go run ./cmd/bundle-galleton
	go build -tags galleton_bundle -o bin/wallapop ./cmd/wallapop

test:
	go test ./... -count=1

fmt:
	gofmt -w .

vet:
	go vet ./...

lint:
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...

check: vet lint test
	test -z "$$(gofmt -l .)"

docs:
	go run ./cmd/docsgen

docs-check: docs
	git diff --exit-code -- docs-site/content/docs/reference
	test -z "$$(git ls-files --others --exclude-standard -- docs-site/content/docs/reference)"

install:
	go run ./cmd/bundle-galleton
	go install -tags galleton_bundle -ldflags "-X main.version=$$(git describe --tags --always --dirty)" ./cmd/wallapop
```

---

## 4. Estrategia de Testing (Cero Mocks de Red)

1. **`internal/fakewallapop`**:
   - Implementa un servidor HTTP de prueba (`httptest.Server`) que simula tanto `es.wallapop.com` como `api.wallapop.com`.
   - Utiliza fixtures JSON estáticas grabadas de sesiones reales (con IDs y datos personales redactados).
   - Permite verificar búsquedas, filtros, respuestas de error HTTP 403/429/500 y flujos de renovación sin tocar la API de producción.
2. **Aislamiento de XDG en Tests**:
   - Cada test que interactúa con configuración o base de datos redefine `XDG_CONFIG_HOME` y `XDG_DATA_HOME` a un directorio temporal creado con `t.TempDir()`.
   - Garantiza que las pruebas unitarias jamás lean ni modifiquen los perfiles reales de la máquina del desarrollador.
3. **Tests de Egresos de Red (`egress_test.go`)**:
   - Comprueba que las llamadas respeten estrictamente la directiva de proxy y no emitan paquetes directos no autorizados.

---

## 5. Distribución y Empaquetado

Mediante **GoReleaser** y scripts auxiliares, se distribuye en múltiples ecosistemas:
- **Binarios estáticos autónomos**: Compilados para Linux (x86_64, arm64), macOS (Intel, Apple Silicon) y Windows (x86_64, arm64).
- **Homebrew Cask**: Para usuarios de macOS (`brew install Microck/tap/wallapop`).
- **Scoop Manifest**: Para usuarios de Windows (`scoop install wallapop`).
- **AUR**: Arch User Repository para distribuciones basadas en Arch Linux.
- **npm Package**: Paquete ligero (`npm i -g wallapop` / `npx wallapop`) cuyo script `install.cjs` detecta la plataforma y descarga el binario Go precompilado correspondiente.
- **Instalador shell**: `curl -fsSL https://wallapop.micr.dev/install.sh | sh`.
