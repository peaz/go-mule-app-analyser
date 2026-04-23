package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/parser"
	"mule-app-analyser/internal/analysis"
	"mule-app-analyser/internal/linker"
	"mule-app-analyser/internal/resources"
)

const analyserVersion = "2.0.0"

const htmlCSSStyle = `@media print{*,:after,:before{background:0 0!important;color:#000!important;box-shadow:none!important;text-shadow:none!important}a,a:visited{text-decoration:underline}a[href]:after{content:" (" attr(href) ")"}abbr[title]:after{content:" (" attr(title) ")"}a[href^="#"]:after,a[href^="javascript:"]:after{content:""}blockquote,pre{border:1px solid #999;page-break-inside:avoid}thead{display:table-header-group}img,tr{page-break-inside:avoid}img{max-width:100%!important}h2,h3,p{orphans:3;widows:3}h2,h3{page-break-after:avoid}}code,pre{font-family:Menlo,Monaco,"Courier New",monospace}pre{padding:.5rem;line-height:1.25;overflow-x:scroll}a,a:visited{color:#3498db}a:active,a:focus,a:hover{color:#2980b9}.modest-no-decoration{text-decoration:none}html{font-size:12px}@media screen and (min-width:32rem) and (max-width:48rem){html{font-size:15px}}@media screen and (min-width:48rem){html{font-size:16px}}body{line-height:1.85}.modest-p,p{font-size:1rem;margin-bottom:1.3rem}.modest-h1,.modest-h2,.modest-h3,.modest-h4,h1,h2,h3,h4{margin:1.414rem 0 .5rem;font-weight:inherit;line-height:1.42}.modest-h1,h1{margin-top:0;font-size:3.998rem;font-weight:500}.modest-h2,h2{font-size:2.827rem}.modest-h3,h3{font-size:1.999rem}.modest-h4,h4{font-size:1.414rem}.modest-h5,h5{font-size:1.121rem}.modest-h6,h6{font-size:.88rem}.modest-small,small{font-size:.707em}canvas,iframe,img,select,svg,textarea,video{max-width:100%}html{font-size:18px;max-width:100%}body{color:#444;font-family:Poppins,sans-serif;font-weight:300;margin:0 auto;max-width:60rem;line-height:1.45;padding:.25rem}h1,h2,h3,h4,h5,h6{font-family:Poppins,Helvetica,sans-serif}h1,h2,h3{border-bottom:2px solid #fafafa;margin-bottom:1.15rem;padding-bottom:.5rem;text-align:left}blockquote{border-left:8px solid #fafafa;padding:1rem}code,pre{background-color:#fafafa;font-size:small}table{border-collapse:collapse;margin:25px 0;min-width:400px;box-shadow:0 0 20px rgba(0,0,0,.15)}thead tr{background-color:#3ea2a8;color:#fff}td,th{padding:12px 15px}tbody tr{border-bottom:1px solid #ddd}tbody tr:nth-of-type(even){background-color:#f3f3f3}tbody tr:last-of-type{border-bottom:2px solid #009879}hr{border:.5px solid #3ea2a8;margin:auto}`

const htmlHeader = `<!DOCTYPE html><html><head><style>` + htmlCSSStyle + `</style></head><body>`
const htmlFooter = `</body></html>`

// mermaidScript is injected into workspace HTML so diagrams render in-browser.
const mermaidScript = `<script type="module">import mermaid from 'https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.esm.min.mjs';mermaid.initialize({startOnLoad:true});</script>`

// OutputPaths holds the file paths written for a single app or workspace report.
type OutputPaths map[string]string // format ("json"|"md"|"html") → absolute path

// AppResult pairs an analysis with the paths written for it.
type AppResult struct {
	Analysis    *analysis.AppAnalysis
	OutputPaths OutputPaths
	Err         error
}

// AppIndexEntry is the per-app entry written into index.json.
type AppIndexEntry struct {
	Name        string      `json:"name"`
	Folder      string      `json:"folder"`
	OutputPaths OutputPaths `json:"outputPaths"`
	Warnings    []string    `json:"warnings,omitempty"`
}

// AppFailedEntry records an app that could not be analysed.
type AppFailedEntry struct {
	Name   string `json:"name"`
	Folder string `json:"folder"`
	Reason string `json:"reason"`
}

// WorkspaceIndex is written as reports/json/workspace/index.json.
type WorkspaceIndex struct {
	ScanTimestamp   string           `json:"scanTimestamp"`
	AnalyserVersion string           `json:"analyserVersion"`
	WorkspaceRoot   string           `json:"workspaceRoot"`
	AppsAnalyzed    []AppIndexEntry  `json:"appsAnalyzed"`
	AppsFailed      []AppFailedEntry `json:"appsFailed"`
	OutputPaths     OutputPaths      `json:"outputPaths"` // workspace-level outputs (populated in Phase 4)
	Warnings        []string         `json:"warnings,omitempty"`
}

// WritePerAppReports writes JSON, MD, and HTML reports for one app.
// workspaceMode=true uses format-separated folder structure; false uses V1 flat layout.
func WritePerAppReports(app *analysis.AppAnalysis, outputRoot string, workspaceMode bool) (OutputPaths, error) {
	paths := make(OutputPaths)
	name := app.MuleProjectName

	var jsonPath, mdPath, htmlPath string
	if workspaceMode {
		jsonPath = filepath.Join(outputRoot, "json", "apps", name, "raw-analysis.json")
		mdPath = filepath.Join(outputRoot, "md", "apps", name, "report.md")
		htmlPath = filepath.Join(outputRoot, "html", "apps", name, "report.html")
	} else {
		// V1 backward-compatible flat layout
		jsonPath = filepath.Join(outputRoot, fmt.Sprintf("rawAnalysisData-%s.json", name))
		mdPath = filepath.Join(outputRoot, fmt.Sprintf("report-%s.md", name))
		htmlPath = filepath.Join(outputRoot, fmt.Sprintf("report-%s.html", name))
	}

	if err := ensureDir(filepath.Dir(jsonPath)); err != nil {
		return paths, err
	}
	if err := ensureDir(filepath.Dir(mdPath)); err != nil {
		return paths, err
	}
	if err := ensureDir(filepath.Dir(htmlPath)); err != nil {
		return paths, err
	}

	// JSON
	if err := writeJSON(jsonPath, app); err != nil {
		return paths, fmt.Errorf("writing JSON report: %w", err)
	}
	paths["json"] = jsonPath

	// Markdown (also needed for HTML)
	var mdBuf bytes.Buffer
	if err := renderMarkdown(app, &mdBuf); err != nil {
		return paths, fmt.Errorf("rendering markdown: %w", err)
	}
	if err := os.WriteFile(mdPath, mdBuf.Bytes(), 0644); err != nil {
		return paths, fmt.Errorf("writing MD report: %w", err)
	}
	paths["md"] = mdPath

	// HTML from the markdown buffer
	if err := writeHTML(htmlPath, mdBuf.Bytes()); err != nil {
		return paths, fmt.Errorf("writing HTML report: %w", err)
	}
	paths["html"] = htmlPath

	return paths, nil
}

// WriteWorkspaceIndex writes index.json under reports/json/workspace/.
func WriteWorkspaceIndex(idx *WorkspaceIndex, outputRoot string) (string, error) {
	dir := filepath.Join(outputRoot, "json", "workspace")
	if err := ensureDir(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "index.json")
	if err := writeJSON(path, idx); err != nil {
		return "", fmt.Errorf("writing index.json: %w", err)
	}
	return path, nil
}

// NewWorkspaceIndex builds a WorkspaceIndex from completed per-app results.
func NewWorkspaceIndex(workspaceRoot string, succeeded []AppResult, failed []AppFailedEntry) *WorkspaceIndex {
	idx := &WorkspaceIndex{
		ScanTimestamp:   time.Now().UTC().Format(time.RFC3339),
		AnalyserVersion: analyserVersion,
		WorkspaceRoot:   workspaceRoot,
		OutputPaths:     make(OutputPaths),
		AppsFailed:      failed,
	}
	if idx.AppsFailed == nil {
		idx.AppsFailed = []AppFailedEntry{}
	}

	for _, r := range succeeded {
		entry := AppIndexEntry{
			Name:        r.Analysis.AppMetadata.ArtifactID,
			Folder:      r.Analysis.AppMetadata.Folder,
			OutputPaths: r.OutputPaths,
			Warnings:    r.Analysis.Warnings,
		}
		idx.AppsAnalyzed = append(idx.AppsAnalyzed, entry)

		// Aggregate unresolved token warnings into the workspace index
		for _, tok := range r.Analysis.UnresolvedTokens {
			idx.Warnings = append(idx.Warnings,
				fmt.Sprintf("%s: unresolved token ${%s}", r.Analysis.AppMetadata.ArtifactID, tok))
		}
	}

	if idx.AppsAnalyzed == nil {
		idx.AppsAnalyzed = []AppIndexEntry{}
	}

	return idx
}

// WriteDependencyGraph writes dependency-graph.json under
// reports/json/workspace/ and returns the written path.
func WriteDependencyGraph(graph *linker.DependencyGraph, outputRoot string) (string, error) {
	dir := filepath.Join(outputRoot, "json", "workspace")
	if err := ensureDir(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "dependency-graph.json")
	if err := writeJSON(path, graph); err != nil {
		return "", fmt.Errorf("writing dependency-graph.json: %w", err)
	}
	return path, nil
}

// EnsureOutputRoot creates the output root directory if it does not exist.
func EnsureOutputRoot(outputRoot string) error {
	return ensureDir(outputRoot)
}

// AnalyserVersion returns the current version string used in reports.
func AnalyserVersion() string {
	return analyserVersion
}

// --- internal helpers ---

func writeJSON(path string, v interface{}) error {
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0644)
}

func renderMarkdown(app *analysis.AppAnalysis, buf *bytes.Buffer) error {
	funcMap := template.FuncMap{
		"sum": func(i ...int) int {
			result := 0
			for _, v := range i {
				result += v
			}
			return result
		},
		"indentLine": func(spaces int) string {
			line := ""
			for i := 1; i <= spaces; i++ {
				line += "   "
			}
			return line + "|_"
		},
		"getFlowSize": func(flowName string) int {
			for _, f := range app.FlowManifest {
				if strings.EqualFold(f.Name, flowName) {
					return f.StandaloneSize
				}
			}
			return 1
		},
		"formatAttrs": func(attrs map[string]string) string {
			if len(attrs) == 0 {
				return ""
			}
			keys := make([]string, 0, len(attrs))
			for k := range attrs {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			parts := make([]string, 0, len(keys))
			for _, k := range keys {
				v := attrs[k]
				if len(v) > 80 {
					v = v[:80] + "..."
				}
				parts = append(parts, fmt.Sprintf("%s: %s", k, v))
			}
			return strings.Join(parts, " | ")
		},
		"dwTargets": func(dws []analysis.DataWeaveScript) string {
			if len(dws) == 0 {
				return ""
			}
			targets := make([]string, 0, len(dws))
			for _, dw := range dws {
				targets = append(targets, dw.Target)
			}
			return " {dw→ " + strings.Join(targets, ", ") + "}"
		},
	}

	tmplContent := string(resources.MarkdownTemplate)
	t, err := template.New("md.tmpl").Funcs(funcMap).Parse(tmplContent)
	if err != nil {
		return fmt.Errorf("parsing markdown template: %w", err)
	}

	return t.Execute(buf, app)
}

func writeHTML(path string, mdBytes []byte) error {
	extensions := parser.CommonExtensions | parser.AutoHeadingIDs
	p := parser.NewWithExtensions(extensions)
	htmlStr := htmlHeader + string(markdown.ToHTML(mdBytes, p, nil)) + htmlFooter
	return os.WriteFile(path, []byte(htmlStr), 0644)
}

func writeHTMLWithMermaid(path string, mdBytes []byte) error {
	extensions := parser.CommonExtensions | parser.AutoHeadingIDs
	p := parser.NewWithExtensions(extensions)
	htmlStr := htmlHeader + mermaidScript + string(markdown.ToHTML(mdBytes, p, nil)) + htmlFooter
	return os.WriteFile(path, []byte(htmlStr), 0644)
}

func ensureDir(dir string) error {
	if dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0755)
}

// --- workspace overall report ---

// AppSummaryEntry is the per-app row in the workspace overall report.
type AppSummaryEntry struct {
	Name          string   `json:"name"`
	MuleVersion   string   `json:"muleVersion"`
	FlowCount     int      `json:"flowCount"`
	Simple        int      `json:"simple"`
	Medium        int      `json:"medium"`
	Complex       int      `json:"complex"`
	TriggerCount  int      `json:"triggerCount"`
	InboundProtos []string `json:"inboundProtocols,omitempty"`
	OutboundProtos []string `json:"outboundProtocols,omitempty"`
	Warnings      int      `json:"warnings,omitempty"`
}

// FlowComplexityTotal aggregates complexity counts across all apps.
type FlowComplexityTotal struct {
	Simple  int `json:"simple"`
	Medium  int `json:"medium"`
	Complex int `json:"complex"`
}

// HotspotEntry ranks apps by number of dependency edges.
type HotspotEntry struct {
	Name     string `json:"name"`
	Outbound int    `json:"outbound"`
	Inbound  int    `json:"inbound"`
}

// WorkspaceReport is the overall workspace-level summary written as
// overall-report.json/md/html under reports/json|md|html/workspace/.
type WorkspaceReport struct {
	ScanTimestamp   string              `json:"scanTimestamp"`
	AnalyserVersion string              `json:"analyserVersion"`
	WorkspaceRoot   string              `json:"workspaceRoot"`
	AppCount        int                 `json:"appCount"`
	TotalFlows      int                 `json:"totalFlows"`
	TotalTransforms int                 `json:"totalTransforms"`
	FlowComplexity  FlowComplexityTotal `json:"flowComplexity"`
	Apps            []AppSummaryEntry   `json:"apps"`
	Graph           *linker.DependencyGraph `json:"dependencyGraph,omitempty"`
	Hotspots        []HotspotEntry      `json:"hotspots,omitempty"`
}

// NewWorkspaceReport builds a WorkspaceReport from per-app results and the dependency graph.
func NewWorkspaceReport(workspaceRoot string, succeeded []AppResult, graph *linker.DependencyGraph) *WorkspaceReport {
	wr := &WorkspaceReport{
		ScanTimestamp:   time.Now().UTC().Format(time.RFC3339),
		AnalyserVersion: analyserVersion,
		WorkspaceRoot:   workspaceRoot,
		AppCount:        len(succeeded),
		Graph:           graph,
	}

	outboundCount := make(map[string]int)
	inboundCount := make(map[string]int)
	if graph != nil {
		for _, e := range graph.Edges {
			outboundCount[e.FromApp]++
			inboundCount[e.ToApp]++
		}
	}

	for _, r := range succeeded {
		a := r.Analysis
		entry := AppSummaryEntry{
			Name:         a.AppMetadata.ArtifactID,
			MuleVersion:  a.MuleVersion,
			FlowCount:    len(a.FlowManifest),
			Simple:       a.FlowComplexityCount.Simple,
			Medium:       a.FlowComplexityCount.Medium,
			Complex:      a.FlowComplexityCount.Complex,
			TriggerCount: a.Triggers.Count,
			Warnings:     len(a.Warnings),
		}
		entry.InboundProtos = uniqueProtocols(a.InboundInterfaces)
		entry.OutboundProtos = uniqueProtocols(a.OutboundInterfaces)

		wr.Apps = append(wr.Apps, entry)
		wr.TotalFlows += len(a.FlowManifest)
		wr.TotalTransforms += len(a.DataWeaveTransforms)
		wr.FlowComplexity.Simple += a.FlowComplexityCount.Simple
		wr.FlowComplexity.Medium += a.FlowComplexityCount.Medium
		wr.FlowComplexity.Complex += a.FlowComplexityCount.Complex
	}

	// Build hotspot list sorted by total edge count descending
	hotspotMap := make(map[string]*HotspotEntry)
	for _, r := range succeeded {
		name := r.Analysis.AppMetadata.ArtifactID
		hotspotMap[name] = &HotspotEntry{Name: name, Outbound: outboundCount[name], Inbound: inboundCount[name]}
	}
	for _, h := range hotspotMap {
		wr.Hotspots = append(wr.Hotspots, *h)
	}
	sort.Slice(wr.Hotspots, func(i, j int) bool {
		ti := wr.Hotspots[i].Outbound + wr.Hotspots[i].Inbound
		tj := wr.Hotspots[j].Outbound + wr.Hotspots[j].Inbound
		if ti != tj {
			return ti > tj
		}
		return wr.Hotspots[i].Name < wr.Hotspots[j].Name
	})

	return wr
}

// WriteOverallReport renders overall-report.{json,md,html} under outputRoot/*/workspace/.
func WriteOverallReport(wr *WorkspaceReport, outputRoot string) (OutputPaths, error) {
	paths := make(OutputPaths)

	jsonPath := filepath.Join(outputRoot, "json", "workspace", "overall-report.json")
	mdPath := filepath.Join(outputRoot, "md", "workspace", "overall-report.md")
	htmlPath := filepath.Join(outputRoot, "html", "workspace", "overall-report.html")

	for _, p := range []string{jsonPath, mdPath, htmlPath} {
		if err := ensureDir(filepath.Dir(p)); err != nil {
			return paths, err
		}
	}

	if err := writeJSON(jsonPath, wr); err != nil {
		return paths, fmt.Errorf("writing overall-report.json: %w", err)
	}
	paths["json"] = jsonPath

	var mdBuf bytes.Buffer
	if err := renderWorkspaceMarkdown(wr, &mdBuf); err != nil {
		return paths, fmt.Errorf("rendering workspace markdown: %w", err)
	}
	if err := os.WriteFile(mdPath, mdBuf.Bytes(), 0644); err != nil {
		return paths, fmt.Errorf("writing overall-report.md: %w", err)
	}
	paths["md"] = mdPath

	if err := writeHTMLWithMermaid(htmlPath, mdBuf.Bytes()); err != nil {
		return paths, fmt.Errorf("writing overall-report.html: %w", err)
	}
	paths["html"] = htmlPath

	return paths, nil
}

func renderWorkspaceMarkdown(wr *WorkspaceReport, buf *bytes.Buffer) error {
	funcMap := template.FuncMap{
		"joinStrings": func(ss []string) string {
			return strings.Join(ss, ", ")
		},
		"mermaidID": func(name string) string {
			return strings.NewReplacer("-", "_", ".", "_").Replace(name)
		},
	}
	t, err := template.New("workspace.tmpl").Funcs(funcMap).Parse(string(resources.WorkspaceTemplate))
	if err != nil {
		return fmt.Errorf("parsing workspace template: %w", err)
	}
	return t.Execute(buf, wr)
}

func uniqueProtocols(ifaces []analysis.Interface) []string {
	seen := make(map[string]bool)
	var out []string
	for _, iface := range ifaces {
		if !seen[iface.Protocol] {
			seen[iface.Protocol] = true
			out = append(out, iface.Protocol)
		}
	}
	sort.Strings(out)
	return out
}
