<p align="center">
  <img src="assets/logo.svg" alt="noah" width="140">
</p>

<h1 align="center">noah</h1>

<p align="center"><b>Memoria persistente para agentes de programación con IA.</b><br>
Un solo archivo, en local, sin cuenta, sin nube, sin servicios en segundo plano.</p>

<p align="center"><a href="README.md">Read in English</a> · <a href="docs/ARCHITECTURE.md">Arquitectura</a> · Licencia: Apache-2.0</p>

---

## Para qué sirve

Los agentes (Claude Code, Cursor…) olvidan todo al terminar la sesión. `noah` les da un cuaderno
con buscador que vive en tu equipo: decisiones tomadas, errores resueltos, convenciones del
proyecto. El agente guarda lo importante mientras trabaja y lo encuentra en la siguiente sesión,
en el proyecto correcto, sin que tengas que pegar contexto.

- **Privado.** Todo queda en un archivo SQLite de tu ordenador. No se envía nada a ningún sitio.
- **Simple.** Un binario. Sin servidor, sin puertos, nada que mantener encendido.
- **Rápido.** Búsquedas en milisegundos; 10.000 memorias ocupan unos 26 MB.
- **Seguro.** Las credenciales se tachan antes de guardarse y una memoria escrita por un agente nunca puede pisar una tuya.
- **Por proyecto.** Cada memoria pertenece al proyecto donde estás y no se mezcla con otros.

> **Estado: v0.1, pre-lanzamiento.** Funciona y tiene tests, pero es joven. Los límites conocidos están [al final](#límites-conocidos).

## Instalación

Hasta que haya binarios publicados necesitas [Go 1.26+](https://go.dev/dl/).

```sh
git clone https://github.com/dicapower/noah.git
cd noah
go build -o noah ./cmd/noah
sudo mv noah /usr/local/bin/        # o cualquier carpeta de tu PATH
noah version
```

(Cuando el repositorio sea público: `go install github.com/dicapower/noah/cmd/noah@latest`.)

Es Go puro, sin dependencias en C: el mismo comando compila en macOS, Linux y Windows.

## Conecta tu agente (2 minutos)

`noah setup` imprime los comandos exactos para tu equipo. Nunca modifica tu configuración.

### Claude Code

```sh
claude mcp add noah --scope user -- noah mcp
```

Opcional pero recomendable: cargar las memorias recientes al empezar cada sesión. Añade esto a `~/.claude/settings.json`:

```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [ { "type": "command", "command": "noah hook session-start" } ] }
    ]
  }
}
```

### Cursor

Añade a `~/.cursor/mcp.json` (o `.cursor/mcp.json` dentro de un proyecto):

```json
{ "mcpServers": { "noah": { "command": "noah", "args": ["mcp"] } } }
```

### Cualquier otro cliente MCP

Ejecuta `noah mcp` como servidor MCP por stdio.

Después, trabaja con normalidad. Pídele a tu agente cosas como *"recuerda que usamos Firebase Auth para el login"* o *"¿qué decidimos sobre la base de datos?"*. El agente decide cuándo guardar y buscar con las herramientas de abajo.

## Si vienes de Engram

Trae tus memorias con un comando. Tu base de datos de Engram solo se lee, desde una copia temporal.

```sh
noah import engram --dry-run     # vista previa: cuenta por proyecto, no escribe nada
noah import engram               # importar (archivo por defecto: ~/.engram/engram.db)
```

Cada proyecto de Engram pasa a ser un proyecto de noah. Se conservan topic keys, tipos, marcas de fijado y fechas originales; se omiten las borradas y las reemplazadas; las credenciales que haya dentro se tachan. Ejecutarlo dos veces no duplica nada. Las memorias importadas quedan marcadas como escritas por un agente; añade `--trust user` si quieres protegerlas de ediciones de agentes.

## Uso desde la terminal

```sh
noah save --kind decision --topic auth/provider "Usamos Firebase Auth con custom claims para roles"
noah search "¿qué proveedor gestiona el login?"
noah search --all "despliegue docker"     # en todos los proyectos
noah context                               # fijadas + más recientes de este proyecto
noah get 42
noah forget 42
```

`--topic` es un nombre estable: guardar de nuevo con el mismo topic actualiza la memoria en vez de duplicarla. `--pin` la mantiene siempre en el contexto.

## Qué recibe el agente

Seis herramientas, pocas a propósito porque cada descripción consume contexto en cada petición.

| Herramienta | Qué hace |
|---|---|
| `mem_search` | Búsqueda en lenguaje natural; devuelve las mejores coincidencias con un fragmento |
| `mem_save` | Guarda una memoria; el mismo `topic` la revisa |
| `mem_get` | Lee una memoria completa |
| `mem_context` | Memorias fijadas y más recientes de este proyecto |
| `mem_forget` | Borra una memoria errónea u obsoleta |
| `mem_spaces` | Lista los proyectos y su tamaño |

## Cómo se decide el proyecto

Una memoria pertenece a un *space* (proyecto), elegido en este orden:

1. la variable de entorno `NOAH_SPACE`,
2. el archivo `.noah` más cercano (escribe dentro el nombre del proyecto),
3. el nombre de la carpeta raíz del repositorio git,
4. el nombre de la carpeta actual.

Tu carpeta personal y `/` se agrupan en `_unscoped`. Las búsquedas se quedan en el proyecto actual salvo que pidas todos (`--all`, o `scope: "all"` desde el agente). `noah space` te dice en qué proyecto estás.

> Si tu agente lanza los servidores MCP fuera de la carpeta del proyecto, define `NOAH_SPACE` en su configuración MCP.

## Seguridad

La memoria de agentes es una superficie de ataque: lo que un agente guarda se vuelve a leer en un prompt futuro.

- **Las credenciales no llegan a la base de datos.** Claves de API, tokens, claves privadas y contraseñas en URLs se tachan al guardar. `NOAH_SECRETS=block` rechaza en su lugar; `off` lo desactiva. `noah doctor` revisa lo ya guardado.
- **Dos niveles de confianza.** Lo que guardas desde la terminal es `user`; lo que guarda un agente es `agent`. Un agente no puede sobrescribir, fijar ni borrar una memoria `user`.
- **Contexto etiquetado.** Las memorias inyectadas van en un bloque que indica que son notas de referencia, no instrucciones, y quién escribió cada una. Una memoria no puede cerrar ese bloque desde dentro.
- **Sin red.** El agente lanza `noah mcp` por stdio. No hay servidor HTTP al que una web pueda acceder. El archivo de base de datos se crea privado (`0600`).

## Todos los comandos

```
noah mcp | setup | hook session-start        integración con agentes
noah save | search | context | get | forget  memorias
noah spaces | space                          proyectos
noah import engram                           migrar desde Engram
noah doctor | export | version               mantenimiento
```

`noah <comando> -h` muestra los flags. `noah export > backup.jsonl` guarda todo en JSON lines.

| Variable de entorno | Significado | Por defecto |
|---|---|---|
| `NOAH_DB` | archivo de base de datos | `~/.noah/noah.db` |
| `NOAH_SPACE` | forzar el proyecto | detectado |
| `NOAH_SECRETS` | `redact`, `block` u `off` | `redact` |

## Problemas frecuentes

- **El agente no ve las herramientas.** Reinícialo tras añadir el servidor y comprueba la ruta con `which noah`; usa la ruta completa en la configuración si el agente no hereda tu `PATH`.
- **Las búsquedas no devuelven nada.** Puede que estés en un proyecto distinto al de guardado. Ejecuta `noah space`, `noah spaces` o busca con `--all`.
- **Algo no cuadra.** `noah doctor` comprueba la integridad de la base de datos, el índice de búsqueda y las credenciales guardadas.
- **Empezar de cero.** Borra `~/.noah/noah.db` (haz antes copia con `noah export`).

## Rendimiento

Memorias sintéticas de ~900 caracteres en una máquina de 2 vCPU:

| Memorias | Búsqueda típica | Inserciones/s | Tamaño | Preguntas naturales acertadas |
|---:|---:|---:|---:|---:|
| 10.000 | 13–33 ms | ~900 | 26 MB | 50 / 50 |
| 100.000 | 94–280 ms | ~860 | 259 MB | 50 / 50 |

Método, comparativas y experimentos descartados: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Límites conocidos

- **Busca palabras, no significado.** "¿Qué base de datos elegimos?" no encontrará una nota que solo diga "SQLite". Están planificados embeddings opcionales.
- **Una palabra presente en casi todas las memorias es lenta a tamaños muy grandes** (unos 280 ms con 100.000 memorias). Las consultas normales son mucho más rápidas.
- **Sin sincronización entre equipos.** Comparte con `noah export` y Git.
- Las cifras salen de datos sintéticos; con notas reales variarán.

## Desarrollo

```sh
go vet ./... && go test ./...
NOAH_SCALE=1000,10000 go test ./internal/search -run TestScale -v    # benchmark de escala
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./cmd/noah           # compilación cruzada
```

## Licencia

Apache License 2.0 — Copyright 2026 DICA POWER OÜ. Ver [LICENSE](LICENSE) y [NOTICE](NOTICE).
