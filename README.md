<img src="assets/icon.svg" width="64" height="64" alt="milanuncios-cli icon">

# milanuncios-cli

Cliente de terminal para el marketplace de Milanuncios (Adevinta Spain), diseñado tanto para interacción ergonómica por personas en consola como para uso autónomo por Agentes de Inteligencia Artificial (Claude Code, Codex, Cursor) mediante integración nativa MCP.

Inspirado en la arquitectura y estándares de ingeniería de [`wallapop-cli`](https://github.com/Microck/wallapop-cli).

---

## 📚 Documentación de Especificación y Arquitectura

La investigación exhaustiva del funcionamiento y arquitectura de referencia se encuentra registrada en los siguientes documentos técnicos:

1. [**Extracción Exhaustiva de Funcionalidades y Capacidades**](docs/reference-wallapop-cli/01-features-and-capabilities.md): Catálogo completo de comandos, flags, modelo de autenticación web, búsquedas, gestión de artículos, mensajería, sistema de watches, sinks y servidor MCP.
2. [**Arquitectura Interna y Mecánica Técnica**](docs/reference-wallapop-cli/02-internal-architecture-and-mechanics.md): Stack zero-CGO, protocolo de cookies/tokens, evasión y pausas ante bloqueos WAF (CloudFront/Akamai), contrato estricto de I/O, códigos de retorno semánticos y algoritmos de diffing.
3. [**Estructura del Repositorio, Tooling y Docs**](docs/reference-wallapop-cli/03-repository-structure-and-tooling.md): Árbol de código, convenciones de nombres para agentes (`AGENTS.md`), generación de documentación de comandos desde Cobra y pipeline de testing.
4. [**Arquitectura de Milanuncios CLI & Roadmap por Slices Verticales**](docs/milanuncios-cli/01-architecture-and-roadmap.md): Adaptación al modelo de datos de Milanuncios y plan de entrega paso a paso en pequeños bloques atómicos.

---

## 🗺️ Roadmap de Implementación (Slices Verticales)

El desarrollo del proyecto se ejecutará mediante incrementos atómicos con registro en commits semánticos y PRs pequeños:

- [x] **Slice 0**: Infraestructura base, tooling, esqueleto del CLI con Cobra y CI.
- [x] **Slice 1**: Contrato de I/O (JSON, Pretty, JSONL, TOON), clasificador de errores y configuración TOML en XDG.
- [x] **Slice 2**: Cliente HTTP para Milanuncios y comando de búsqueda pública (`search`).
- [x] **Slice 3**: Inspección detallada de anuncios (`ad show`), imágenes y apertura en navegador.
- [x] **Slice 4**: Taxonomía de categorías y filtros específicos (`category list`, `--filter clave=valor`).
- [x] **Slice 5**: Autenticación web segura por cookies, gestión de perfiles (`auth login`, `me`).
- [ ] **Slice 6**: Motor local de monitorización y persistencia con SQLite (`watch add`, `watch check`, `watch run`).
- [ ] **Slice 7**: Canales de notificación (`sinks`: ntfy, webhooks, exec).
- [ ] **Slice 8**: Servidor MCP (*Model Context Protocol*) para Agentes de IA.
- [ ] **Slice 9**: Mensajería y chat.
- [ ] **Slice 10**: Sitio web de documentación, generador de referencia y empaquetado multiplataforma.

---

## ⚖️ Descargo de responsabilidad (*Disclaimer*)

Este proyecto es una herramienta comunitaria independiente no oficial y no está afiliada, asociada ni avalada por Milanuncios ni Adevinta Spain.
