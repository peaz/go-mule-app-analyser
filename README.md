# Mule App Analyser

A command-line tool that analyses MuleSoft application projects and generates migration assessment reports for moving to [Workato](https://www.workato.com).

It parses Mule 3 and Mule 4 XML configuration files, extracts flows, components, DataWeave transformation scripts, and connector configurations, then maps them to equivalent Workato capabilities with specific migration recommendations.

## Output

Three report files are generated in a `reports/` directory:

| File | Format | Use |
|---|---|---|
| `rawAnalysisData-{project}.json` | JSON | Machine-readable data for further processing |
| `report-{project}.md` | Markdown | Human-readable assessment |
| `report-{project}.html` | HTML | Print/share-ready formatted report |

### Report Contents

- **Flow manifest** — all flows, sub-flows, and batch jobs with step counts and complexity ratings
- **Flow details** — component hierarchy with extracted configuration attributes and DataWeave transform targets shown inline
- **DataWeave Transformations** — all `%dw 2.0` scripts extracted verbatim, organised by flow and output target (`payload`, `variable:name`)
- **Component list** — every unique Mule component detected across all XML files
- **Configurations** — all global connector configurations (`*-config` elements) and their attributes
- **Migration recommendations** — per-component Workato guidance, including a **Detected Config** row showing the actual configuration found (cron expression, SQL, retry settings, etc.)

## Usage

```
./mule-app-analyser-{version}-{platform} -p /path/to/mule/project/root
```

The tool auto-detects the Mule version by checking for `src/main/app/` (Mule 3) or `src/main/mule/` (Mule 4).

### Example

```bash
# macOS ARM
./releases/v1.1.0/mule-app-analyser-v1.1.0-osx-arm64 -p ~/projects/my-mule-api/

# macOS Intel
./releases/v1.1.0/mule-app-analyser-v1.1.0-osx-amd64 -p ~/projects/my-mule-api/

# Linux
./releases/v1.1.0/mule-app-analyser-v1.1.0-linux -p ~/projects/my-mule-api/

# Windows
mule-app-analyser-v1.1.0-win.exe -p C:\projects\my-mule-api\
```

The binary must be run from the directory that contains the `resources/` folder (i.e. the release directory).

## Flow Complexity Rating

| Rating | Criteria |
|---|---|
| Simple | < 5 steps and < 2 DataWeave transforms |
| Medium | 5–10 steps, or 2–4 transforms |
| Complex | > 10 steps or ≥ 5 transforms |

Step counts include all steps from referenced sub-flows (merged count), so complexity reflects the true execution depth.

## What Gets Extracted

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
git tag v1.x.x

# Build all platforms
bash build.sh
```

Binaries and a copy of `resources/` are placed in `releases/v1.x.x/`.

### Dependencies

| Package | Purpose |
|---|---|
| `github.com/beevik/etree` | XML parsing |
| `github.com/gomarkdown/markdown` | Markdown to HTML conversion |

## Project Structure

```
go-mule-app-analyser/
├── src/mule-app-analyser/
│   ├── main.go                  # Core analysis and report generation
│   └── resources/
│       ├── dictionary.json      # Mule component → Workato recommendation mappings
│       └── md.tmpl              # Markdown report template
├── releases/
│   └── v1.x.x/                 # Built binaries + resources copy
├── example-mule-apps/           # Sample Mule 4 projects for testing
├── build.sh                     # Cross-platform build script
├── go.mod
└── go.sum
```

## Changelog

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
