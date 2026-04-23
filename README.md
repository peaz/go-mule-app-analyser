# Mule App Analyser

A command-line tool that analyses MuleSoft application projects and generates migration assessment reports for moving to [Workato](https://www.workato.com).

It parses Mule 3 and Mule 4 XML configuration files, extracts flows, components, DataWeave transformation scripts, and connector configurations, then maps them to equivalent Workato capabilities with specific migration recommendations.

**v2.0.0** adds workspace mode for analysing an entire portfolio of Mule apps at once, with cross-app dependency graph generation.

## Usage

### Single App

```bash
./mule-app-analyser-{version}-{platform} -p /path/to/mule/project/root
```

The tool auto-detects the Mule version by checking for `src/main/app/` (Mule 3) or `src/main/mule/` (Mule 4).

### Workspace (Multi-App)

```bash
./mule-app-analyser-{version}-{platform} -workspace /path/to/workspace/root
```

Recursively discovers all Mule 3 and Mule 4 apps under the workspace root, analyses each one, and generates an aggregated overall report with a cross-app dependency graph.

### Flags

| Flag | Default | Description |
|---|---|---|
| `-p <path>` | — | Single-app mode: path to Mule project root |
| `-workspace <dir>` | — | Workspace mode: path to directory containing multiple Mule apps |
| `-output <dir>` | `./reports` | Root directory for all report output |
| `-dependencies` | `true` | Build cross-app dependency graph (workspace mode only) |
| `-min-confidence <0-100>` | `20` | Minimum confidence score to include a dependency edge |

### Examples

```bash
# Single app — macOS ARM
./releases/v2.0.0/mule-app-analyser-v2.0.0-osx-arm64 -p ~/projects/my-mule-api/

# Single app — macOS Intel
./releases/v2.0.0/mule-app-analyser-v2.0.0-osx-amd64 -p ~/projects/my-mule-api/

# Single app — Linux
./releases/v2.0.0/mule-app-analyser-v2.0.0-linux -p ~/projects/my-mule-api/

# Single app — Windows
mule-app-analyser-v2.0.0-win.exe -p C:\projects\my-mule-api\

# Workspace — analyse all apps in a directory
./releases/v2.0.0/mule-app-analyser-v2.0.0-osx-arm64 -workspace ~/projects/mule-workspace/

# Workspace — higher confidence threshold, custom output dir
./releases/v2.0.0/mule-app-analyser-v2.0.0-osx-arm64 -workspace ~/projects/mule-workspace/ -output ~/reports -min-confidence 50
```

## Output

### Single App

Three report files are generated in the `-output` directory:

| File | Format | Use |
|---|---|---|
| `rawAnalysisData-{project}.json` | JSON | Machine-readable data for further processing |
| `report-{project}.md` | Markdown | Human-readable assessment |
| `report-{project}.html` | HTML | Print/share-ready formatted report |

### Workspace

Reports are organised by format under the output root:

```
reports/
├── json/
│   ├── apps/{app-name}/raw-analysis.json   # Per-app machine-readable data
│   └── workspace/
│       ├── index.json                       # App inventory with status and paths
│       ├── overall-report.json              # Aggregated stats and dependency graph
│       └── dependency-graph.json            # Weighted cross-app dependency edges
├── md/
│   ├── apps/{app-name}/report.md           # Per-app Markdown report
│   └── workspace/overall-report.md         # Workspace summary in Markdown
└── html/
    ├── apps/{app-name}/report.html         # Per-app HTML report
    └── workspace/overall-report.html       # Workspace summary in HTML
```

### Report Contents

**Per-app reports** include:
- **App metadata** — Maven group/artifact/version, Mule version
- **RAML info** — API title, version, and base URI (if present)
- **Flow manifest** — all flows, sub-flows, and batch jobs with step counts and complexity ratings
- **Flow details** — component hierarchy with extracted configuration attributes and DataWeave transform targets shown inline
- **DataWeave Transformations** — all `%dw 2.0` scripts extracted verbatim, organised by flow and output target (`payload`, `variable:name`)
- **Inbound/outbound interfaces** — detected endpoints by protocol (HTTP, JMS, Kafka, SQS, SFTP, AMQP, etc.)
- **Component list** — every unique Mule component detected across all XML files
- **Configurations** — all global connector configurations (`*-config` elements) and their attributes
- **Migration recommendations** — per-component Workato guidance, including a **Detected Config** row showing the actual configuration found

**Workspace overall report** additionally includes:
- Executive summary table across all apps
- Per-app summary with Mule version, flow counts, complexity, and detected protocols
- Dependency graph table showing cross-app connections with confidence scores
- Mermaid diagram for visual dependency relationships
- Hotspot list — apps ranked by total dependency edge count

## Cross-App Dependency Detection

In workspace mode the linker analyses inbound and outbound interfaces across all apps and builds a weighted dependency graph.

Confidence scoring aggregates multiple evidence types:

| Evidence | Examples |
|---|---|
| Config-ref matching | Config reference names containing another app's artifact ID |
| Naming patterns | App tokens from Maven artifact ID matched in endpoints |
| Property resolution | `${property}` placeholders resolved against `.properties` files |
| RAML base URI | Service endpoints matched across apps |
| Protocol inference | HTTP, JMS, VM, SQS, Kafka, AMQP endpoint correlation |

Confidence levels: **low** (20–49), **medium** (50–79), **high** (80+). Edges below `-min-confidence` are excluded from reports but preserved in `dependency-graph.json` as ignored edges for transparency.

## Flow Complexity Rating

| Rating | Criteria |
|---|---|
| Simple | < 5 steps and < 2 DataWeave transforms |
| Medium | 5–10 steps, or 2–4 transforms |
| Complex | > 10 steps or ≥ 5 transforms |

Step counts include all steps from referenced sub-flows (merged count), so complexity reflects true execution depth.

## What Gets Extracted

### App Metadata

From `pom.xml` and `mule-artifact.json` / `mule-project.xml`:
- Maven `groupId`, `artifactId`, `version`
- Mule runtime version

### RAML

From `src/main/resources/api/*.raml`:
- API title, version, `baseUri`

### Properties

All `*.properties` files under `src/main/resources/` are loaded and used to resolve `${token}` placeholders found in component values.

### Inbound / Outbound Interfaces

| Protocol | Inbound | Outbound |
|---|---|---|
| HTTP | `http:listener` | `http:request` |
| JMS | `jms:listener` | `jms:publish` |
| Kafka | `kafka:consumer` | `kafka:publish` |
| SQS | `sqs:receivemessages` | `sqs:send-message` |
| SFTP | `sftp:listener` | `sftp:write` |
| File | `file:listener` | `file:write` |
| AMQP | `amqp:listener` | `amqp:publish` |

### Component Attributes

Key configuration is extracted from each detected component and shown inline in the flow detail view and in the **Detected Config** field of the relevant migration recommendation.

| Component | Extracted Attributes |
|---|---|
| `scheduler` | `cron-expression`, `cron-timezone`, `frequency`, `timeUnit` |
| `foreach` / `parallel-foreach` | `collection`, `maxConcurrency`, `target` |
| `until-successful` | `maxRetries`, `millisBetweenRetries` |
| `try` | `transactionalAction` |
| `http:request` | `method`, `path`, `config-ref` |
| `db:select/insert/update/delete` | `config-ref`, `sql` (from child element, truncated to 200 chars) |
| `salesforce:*` / `sfdc:*` | `objectType`, `externalIdFieldName`, `config-ref` |
| `sftp:list/read/write` | `directoryPath`, `matcher`, `path`, `target` |
| `ee:cache` | `cachingStrategy-ref` |
| `idempotent-message-filter` | `idExpression`, `storePrefix` |
| `on-error-propagate/continue` | `type`, `enableNotifications` |
| `jms:listener` | `destination`, `numberOfConsumers` |
| `raise-error` | `type`, `description` |
| All others | All non-editor attributes (strips `doc:name`, `doc:id`, `doc:description`) |

### DataWeave Scripts

Scripts are extracted from:
- **Mule 4**: `ee:transform` → `ee:message/ee:set-payload` and `ee:variables/ee:set-variable`
- **Mule 3**: `dw:transform-message` → `dw:set-payload` and `dw:set-variable`

Each script is stored with its output target (`payload` or `variable:varName`) and appears in both the raw JSON and the **DataWeave Transformations** report section.

## Migration Recommendations

Recommendations are driven by `resources/dictionary.json`. The analyser matches detected component tags against dictionary entries using substring matching (e.g. `db:` matches `db:select`, `db:insert`, etc.).

Covered Mule components include:

**Connectors**: `db:`, `salesforce:`/`sfdc:`, `http:listener`, `http:request`, `jms:`, `kafka:`, `sqs:`, `s3:`, `amqp:`, `ftp:`, `sftp:`, `file:`, `smtp:`/`email:`, `vm`, `anypoint-mq`, `sap:`, `servicenow:`, `workday:`, `wsc`/`ws`, `cxf:jaxws-service`, `cloudhub`, `os`

**Flow control**: `batch:job`, `scatter-gather`, `foreach`, `parallel-foreach`, `async`, `try`, `until-successful`, `raise-error`, `flow-ref`, `idempotent-message-filter`, `watermark`, `scheduler`

**Transformation**: `dw:transform-message`, `ee:transform`, `*-transformer`

**API**: `apikit:router`, `apikit:console`

**Platform**: `secure-properties:`, `oauth2-provider:`, `validation:`

To add or update a recommendation, edit `resources/dictionary.json` and re-run the analyser.

## Building from Source

Prerequisites: Go 1.20+

```bash
# Tag the version first
git tag v2.x.x

# Build all platforms (uses vendored dependencies)
bash scripts/build.sh
```

Binaries are placed in `releases/v2.x.x/`.

### Dependencies

| Package | Purpose |
|---|---|
| `github.com/beevik/etree` | XML parsing |
| `github.com/gomarkdown/markdown` | Markdown to HTML conversion |

## Changelog

### v2.0.0
- **Workspace mode** (`-workspace`): discover and analyse an entire portfolio of Mule apps in one run
- **Cross-app dependency graph**: linker correlates inbound/outbound interfaces across apps using config-ref matching, naming patterns, property resolution, RAML base URIs, and protocol inference, with a confidence score per edge
- **Per-app interface extraction**: detects inbound and outbound endpoints for HTTP, JMS, Kafka, SQS, SFTP, file, and AMQP connectors
- **App metadata extraction**: reads Maven coordinates from `pom.xml` and Mule version from `mule-artifact.json` / `mule-project.xml`
- **RAML support**: extracts API title, version, and `baseUri` from RAML files
- **Property resolution**: loads `.properties` files and resolves `${token}` placeholders in component values
- **Workspace reports**: overall report with executive summary, per-app table, dependency graph table, Mermaid diagram, and hotspot list
- **Modular architecture**: refactored from a single file into `internal/analysis`, `internal/discovery`, `internal/linker`, and `internal/report` packages
- **Vendored dependencies** and updated build script at `scripts/build.sh`
- Backward compatible: single-app mode (`-p`) unchanged

### v1.1.0
- Extracts functional attributes from each component (SQL from `db:*`, cron from `scheduler`, retry config from `until-successful`, etc.)
- Extracts DataWeave scripts from `ee:transform` / `dw:transform-message` with output targets; adds **DataWeave Transformations** section to reports
- **Detected Config** row added to each recommendation showing the actual configuration found in the app
- Added `parallel-foreach`, `on-error-propagate`, `on-error-continue`, `processor-chain` to drilldown detection
- Fixed duplicate `until-successful` entry in drilldown list
- 20+ new migration recommendation entries covering databases, messaging, file transfer, SAP, flow control scopes, and platform security

### v1.0.1
- Recursive XML traversal to support Mule apps with subdirectories

### v1.0.0
- Initial release
