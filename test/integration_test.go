package test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mule-app-analyser/internal/analysis"
	"mule-app-analyser/internal/discovery"
	"mule-app-analyser/internal/linker"
	"mule-app-analyser/internal/report"
	"mule-app-analyser/internal/resources"
)

const exampleAppsDir = "../example-mule-apps"

func loadDict(t *testing.T) *analysis.Dictionary {
	t.Helper()
	dict, err := analysis.LoadDictionary(resources.DictionaryJSON)
	if err != nil {
		t.Fatalf("LoadDictionary: %v", err)
	}
	return dict
}

// TestDiscoverApps verifies that all 6 example apps are detected.
func TestDiscoverApps(t *testing.T) {
	apps, err := discovery.DiscoverApps(exampleAppsDir)
	if err != nil {
		t.Fatalf("DiscoverApps: %v", err)
	}
	if len(apps) != 6 {
		t.Errorf("expected 6 apps, got %d", len(apps))
	}
	for _, app := range apps {
		if app.MuleVersion != 4 {
			t.Errorf("app %s: expected Mule 4, got %d", app.Name, app.MuleVersion)
		}
		if app.XMLPath == "" {
			t.Errorf("app %s: XMLPath is empty", app.Name)
		}
	}
}

// TestDetectAppNegative ensures a non-Mule directory is not misidentified.
func TestDetectAppNegative(t *testing.T) {
	_, ok := discovery.DetectApp(".")
	if ok {
		t.Error("repo root should not be detected as a Mule app")
	}
}

// TestAnalyseSapPapi checks the sap-papi app produces expected analysis fields.
func TestAnalyseSapPapi(t *testing.T) {
	dict := loadDict(t)
	app, ok := discovery.DetectApp(filepath.Join(exampleAppsDir, "sap-papi"))
	if !ok {
		t.Fatal("sap-papi not detected as a Mule app")
	}

	result, err := analysis.AnalyseApp(app, dict)
	if err != nil {
		t.Fatalf("AnalyseApp sap-papi: %v", err)
	}

	// V1 fields preserved
	if result.MuleProjectName != "sap-papi" {
		t.Errorf("MuleProjectName = %q, want sap-papi", result.MuleProjectName)
	}
	if result.MuleVersion != "4.4.0" {
		t.Errorf("MuleVersion = %q, want 4.4.0", result.MuleVersion)
	}
	if len(result.FlowManifest) == 0 {
		t.Error("FlowManifest is empty")
	}
	if len(result.ComponentList) == 0 {
		t.Error("ComponentList is empty")
	}
	if result.Triggers.Count == 0 {
		t.Error("expected at least one trigger")
	}

	// V2 metadata from pom.xml
	if result.AppMetadata.GroupID != "com.mycompany" {
		t.Errorf("GroupID = %q, want com.mycompany", result.AppMetadata.GroupID)
	}
	if result.AppMetadata.ArtifactID != "sap-papi" {
		t.Errorf("ArtifactID = %q, want sap-papi", result.AppMetadata.ArtifactID)
	}
	if result.AppMetadata.Version != "1.0.0-SNAPSHOT" {
		t.Errorf("Version = %q, want 1.0.0-SNAPSHOT", result.AppMetadata.Version)
	}

	// V2 interfaces — sap-papi has JMS listeners and HTTP requests
	if len(result.InboundInterfaces) == 0 {
		t.Error("expected inbound interfaces")
	}
	if len(result.OutboundInterfaces) == 0 {
		t.Error("expected outbound interfaces")
	}

	// Unresolved tokens expected (no .properties files in example apps)
	if len(result.UnresolvedTokens) == 0 {
		t.Error("expected unresolved tokens since no .properties files are present")
	}
}

// TestWorkspaceAnalysis exercises the full workspace pipeline end-to-end.
func TestWorkspaceAnalysis(t *testing.T) {
	dict := loadDict(t)
	apps, err := discovery.DiscoverApps(exampleAppsDir)
	if err != nil {
		t.Fatalf("DiscoverApps: %v", err)
	}

	outputRoot := t.TempDir()
	var succeeded []report.AppResult
	var failed []report.AppFailedEntry

	for _, app := range apps {
		result, err := analysis.AnalyseApp(app, dict)
		if err != nil {
			failed = append(failed, report.AppFailedEntry{Name: app.Name, Folder: app.Dir, Reason: err.Error()})
			continue
		}
		paths, err := report.WritePerAppReports(result, outputRoot, true)
		if err != nil {
			t.Errorf("WritePerAppReports %s: %v", app.Name, err)
			continue
		}
		succeeded = append(succeeded, report.AppResult{Analysis: result, OutputPaths: paths})
	}

	if len(failed) > 0 {
		t.Errorf("apps failed analysis: %v", failed)
	}
	if len(succeeded) != 6 {
		t.Errorf("expected 6 succeeded, got %d", len(succeeded))
	}

	// Verify per-app file layout: json/apps/<name>/raw-analysis.json etc.
	for _, r := range succeeded {
		name := r.Analysis.AppMetadata.ArtifactID
		for _, format := range []string{"json", "md", "html"} {
			if _, err := os.Stat(r.OutputPaths[format]); err != nil {
				t.Errorf("app %s: missing %s output at %s", name, format, r.OutputPaths[format])
			}
		}
	}

	// Write and verify index.json
	absRoot, _ := filepath.Abs(exampleAppsDir)
	idx := report.NewWorkspaceIndex(absRoot, succeeded, failed)
	indexPath, err := report.WriteWorkspaceIndex(idx, outputRoot)
	if err != nil {
		t.Fatalf("WriteWorkspaceIndex: %v", err)
	}
	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("index.json not written at %s", indexPath)
	}

	// Parse index.json and validate fields
	indexBytes, _ := os.ReadFile(indexPath)
	var parsed report.WorkspaceIndex
	if err := json.Unmarshal(indexBytes, &parsed); err != nil {
		t.Fatalf("index.json is not valid JSON: %v", err)
	}
	if parsed.AnalyserVersion != "2.0.0" {
		t.Errorf("index analyserVersion = %q, want 2.0.0", parsed.AnalyserVersion)
	}
	if len(parsed.AppsAnalyzed) != 6 {
		t.Errorf("index appsAnalyzed = %d, want 6", len(parsed.AppsAnalyzed))
	}
	if len(parsed.AppsFailed) != 0 {
		t.Errorf("index appsFailed = %d, want 0", len(parsed.AppsFailed))
	}
}

// TestBuildDependencyGraph verifies the linker produces correct edges and no false positives.
func TestBuildDependencyGraph(t *testing.T) {
	dict := loadDict(t)
	apps, err := discovery.DiscoverApps(exampleAppsDir)
	if err != nil {
		t.Fatalf("DiscoverApps: %v", err)
	}

	var analysed []*analysis.AppAnalysis
	for _, app := range apps {
		result, err := analysis.AnalyseApp(app, dict)
		if err != nil {
			t.Fatalf("AnalyseApp %s: %v", app.Name, err)
		}
		analysed = append(analysed, result)
	}

	graph := linker.BuildGraph(analysed, exampleAppsDir, 20)

	// Build lookup: "fromApp→toApp" → Edge for assertions
	edgeMap := make(map[string]linker.Edge)
	for _, e := range graph.Edges {
		edgeMap[fmt.Sprintf("%s→%s", e.FromApp, e.ToApp)] = e
	}
	for _, e := range graph.IgnoredEdges {
		edgeMap[fmt.Sprintf("%s→%s[ignored]", e.FromApp, e.ToApp)] = e
	}

	// nsap-papi has config-ref="HTTP_Request_nsap-sapi" — exact app-id match → MEDIUM (no resolved paths)
	e, ok := edgeMap["nsap-papi→nsap-sapi"]
	if !ok {
		t.Error("expected edge nsap-papi→nsap-sapi, not found")
	} else if e.Confidence < 50 {
		t.Errorf("nsap-papi→nsap-sapi confidence = %d, want ≥50 (MEDIUM)", e.Confidence)
	}

	// nsap-papi→nsap-sapi must score strictly higher than nsap-papi→sap-sapi
	// because the config-ref contains the exact nsap-sapi app-id, not sap-sapi's
	nsapToNsap := edgeMap["nsap-papi→nsap-sapi"].Confidence
	nsapToSap := 0
	if e2, exists := edgeMap["nsap-papi→sap-sapi"]; exists {
		nsapToSap = e2.Confidence
	}
	if ignored, exists := edgeMap["nsap-papi→sap-sapi[ignored]"]; exists && ignored.Confidence > nsapToSap {
		nsapToSap = ignored.Confidence
	}
	if nsapToNsap <= nsapToSap {
		t.Errorf("nsap-papi→nsap-sapi (%d) should score higher than nsap-papi→sap-sapi (%d)", nsapToNsap, nsapToSap)
	}

	// sap-papi calls sap-sapi via HTTP_Request_config_sapi — expect above threshold
	e, ok = edgeMap["sap-papi→sap-sapi"]
	if !ok {
		t.Error("expected edge sap-papi→sap-sapi, not found in graph edges")
	} else if e.Confidence < 20 {
		t.Errorf("sap-papi→sap-sapi confidence = %d, want ≥20", e.Confidence)
	}

	// False-positive guard: nsap-papi→sap-sapi must not be MEDIUM or above.
	// The config-ref "HTTP_Request_nsap-sapi" contains only the generic token "sapi",
	// not the full app-id "sap-sapi" as a word, so confidence must stay LOW.
	if e, ok := edgeMap["nsap-papi→sap-sapi"]; ok && e.Confidence >= 50 {
		t.Errorf("false positive: nsap-papi→sap-sapi confidence = %d, must be < 50", e.Confidence)
	}
}

// TestWriteOverallReport verifies the overall workspace report is generated correctly.
func TestWriteOverallReport(t *testing.T) {
	dict := loadDict(t)
	apps, err := discovery.DiscoverApps(exampleAppsDir)
	if err != nil {
		t.Fatalf("DiscoverApps: %v", err)
	}

	outputRoot := t.TempDir()
	var succeeded []report.AppResult

	for _, app := range apps {
		result, err := analysis.AnalyseApp(app, dict)
		if err != nil {
			t.Fatalf("AnalyseApp %s: %v", app.Name, err)
		}
		paths, err := report.WritePerAppReports(result, outputRoot, true)
		if err != nil {
			t.Fatalf("WritePerAppReports %s: %v", app.Name, err)
		}
		succeeded = append(succeeded, report.AppResult{Analysis: result, OutputPaths: paths})
	}

	var analysed []*analysis.AppAnalysis
	for _, r := range succeeded {
		analysed = append(analysed, r.Analysis)
	}
	graph := linker.BuildGraph(analysed, exampleAppsDir, 20)

	absRoot, _ := filepath.Abs(exampleAppsDir)
	wr := report.NewWorkspaceReport(absRoot, succeeded, graph)

	if wr.AppCount != 6 {
		t.Errorf("AppCount = %d, want 6", wr.AppCount)
	}
	if wr.TotalFlows == 0 {
		t.Error("TotalFlows is 0")
	}
	if wr.Graph == nil {
		t.Error("Graph is nil in WorkspaceReport")
	}
	if len(wr.Hotspots) != 6 {
		t.Errorf("Hotspots len = %d, want 6", len(wr.Hotspots))
	}

	paths, err := report.WriteOverallReport(wr, outputRoot)
	if err != nil {
		t.Fatalf("WriteOverallReport: %v", err)
	}

	for _, format := range []string{"json", "md", "html"} {
		if _, err := os.Stat(paths[format]); err != nil {
			t.Errorf("overall-report.%s not found at %s", format, paths[format])
		}
	}

	// HTML must contain the Mermaid CDN script and a graph LR block
	htmlBytes, _ := os.ReadFile(paths["html"])
	htmlContent := string(htmlBytes)
	if !strings.Contains(htmlContent, "mermaid") {
		t.Error("HTML report missing Mermaid script")
	}
	mdBytes, _ := os.ReadFile(paths["md"])
	if !strings.Contains(string(mdBytes), "graph LR") {
		t.Error("MD report missing Mermaid graph LR block")
	}
}

// TestSingleAppBackwardCompat verifies V1-compatible flat output paths.
func TestSingleAppBackwardCompat(t *testing.T) {
	dict := loadDict(t)
	app, ok := discovery.DetectApp(filepath.Join(exampleAppsDir, "sfdc-eapi"))
	if !ok {
		t.Fatal("sfdc-eapi not detected")
	}

	result, err := analysis.AnalyseApp(app, dict)
	if err != nil {
		t.Fatalf("AnalyseApp sfdc-eapi: %v", err)
	}

	outputRoot := t.TempDir()
	paths, err := report.WritePerAppReports(result, outputRoot, false)
	if err != nil {
		t.Fatalf("WritePerAppReports: %v", err)
	}

	// V1 flat paths must be used
	wantJSON := filepath.Join(outputRoot, "rawAnalysisData-sfdc-eapi.json")
	wantMD := filepath.Join(outputRoot, "report-sfdc-eapi.md")
	wantHTML := filepath.Join(outputRoot, "report-sfdc-eapi.html")

	if paths["json"] != wantJSON {
		t.Errorf("json path = %q, want %q", paths["json"], wantJSON)
	}
	if paths["md"] != wantMD {
		t.Errorf("md path = %q, want %q", paths["md"], wantMD)
	}
	if paths["html"] != wantHTML {
		t.Errorf("html path = %q, want %q", paths["html"], wantHTML)
	}

	for _, p := range []string{wantJSON, wantMD, wantHTML} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected file not found: %s", p)
		}
	}
}

// makeMule4App creates a minimal Mule 4 app directory structure under dir.
func makeMule4App(t *testing.T, dir, artifactID string) {
	t.Helper()
	muleDir := filepath.Join(dir, "src", "main", "mule")
	if err := os.MkdirAll(muleDir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", muleDir, err)
	}
	os.WriteFile(filepath.Join(dir, "mule-artifact.json"), []byte(`{"minMuleVersion":"4.4.0"}`), 0644)
	os.WriteFile(filepath.Join(dir, "pom.xml"), []byte(fmt.Sprintf(`<?xml version="1.0"?>
<project><groupId>com.test</groupId><artifactId>%s</artifactId><version>1.0.0</version></project>`, artifactID)), 0644)
}

// TestAnalyseMalformedXML verifies that a malformed XML file produces a warning
// rather than an error or panic, and analysis still succeeds.
func TestAnalyseMalformedXML(t *testing.T) {
	dict := loadDict(t)
	dir := t.TempDir()
	makeMule4App(t, dir, "broken-app")

	muleDir := filepath.Join(dir, "src", "main", "mule")
	os.WriteFile(filepath.Join(muleDir, "broken.xml"), []byte(`<?xml version="1.0"?><mule UNCLOSED`), 0644)

	app, ok := discovery.DetectApp(dir)
	if !ok {
		t.Fatal("broken-app not detected as Mule app")
	}

	result, err := analysis.AnalyseApp(app, dict)
	if err != nil {
		t.Fatalf("AnalyseApp returned error for malformed XML: %v", err)
	}

	hasWarning := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "broken.xml") || strings.Contains(w, "malformed") || strings.Contains(w, "unreadable") {
			hasWarning = true
			break
		}
	}
	if !hasWarning {
		t.Errorf("expected warning about broken XML, got warnings: %v", result.Warnings)
	}
}

// TestAnalyseEmptyApp verifies that an app with a valid but empty XML
// (no flows, no configs) succeeds with zero complexity counts.
func TestAnalyseEmptyApp(t *testing.T) {
	dict := loadDict(t)
	dir := t.TempDir()
	makeMule4App(t, dir, "empty-app")

	emptyXML := `<?xml version="1.0" encoding="UTF-8"?>
<mule xmlns="http://www.mulesoft.org/schema/mule/core"></mule>`
	os.WriteFile(filepath.Join(dir, "src", "main", "mule", "empty.xml"), []byte(emptyXML), 0644)

	app, ok := discovery.DetectApp(dir)
	if !ok {
		t.Fatal("empty-app not detected as Mule app")
	}

	result, err := analysis.AnalyseApp(app, dict)
	if err != nil {
		t.Fatalf("AnalyseApp returned error for empty app: %v", err)
	}
	if len(result.FlowManifest) != 0 {
		t.Errorf("FlowManifest = %d, want 0", len(result.FlowManifest))
	}
	if result.FlowComplexityCount.Simple+result.FlowComplexityCount.Medium+result.FlowComplexityCount.Complex != 0 {
		t.Errorf("expected zero complexity counts, got %+v", result.FlowComplexityCount)
	}
}

// TestNilDictionary verifies AnalyseApp does not panic when dict is nil.
func TestNilDictionary(t *testing.T) {
	dir := t.TempDir()
	makeMule4App(t, dir, "nil-dict-app")

	emptyXML := `<?xml version="1.0" encoding="UTF-8"?>
<mule xmlns="http://www.mulesoft.org/schema/mule/core"></mule>`
	os.WriteFile(filepath.Join(dir, "src", "main", "mule", "main.xml"), []byte(emptyXML), 0644)

	app, ok := discovery.DetectApp(dir)
	if !ok {
		t.Fatal("nil-dict-app not detected")
	}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("AnalyseApp panicked with nil dict: %v", r)
		}
	}()
	_, err := analysis.AnalyseApp(app, nil)
	if err != nil {
		t.Fatalf("AnalyseApp returned error with nil dict: %v", err)
	}
}

// TestPartialWorkspaceFailure verifies that a workspace with one broken app
// still produces results for all valid apps.
func TestPartialWorkspaceFailure(t *testing.T) {
	dict := loadDict(t)

	// Use a temp workspace containing one broken app alongside the real ones.
	// We simulate failure by passing a non-existent path to AnalyseApp directly.
	apps, err := discovery.DiscoverApps(exampleAppsDir)
	if err != nil {
		t.Fatalf("DiscoverApps: %v", err)
	}

	outputRoot := t.TempDir()
	var succeeded []report.AppResult
	var failed []report.AppFailedEntry

	for i, app := range apps {
		var result *analysis.AppAnalysis
		if i == 0 {
			// Simulate failure for the first app by passing a broken app root
			brokenDir := t.TempDir()
			makeMule4App(t, brokenDir, "broken-sim")
			os.WriteFile(filepath.Join(brokenDir, "src", "main", "mule", "bad.xml"),
				[]byte(`<?xml INVALID`), 0644)
			brokenApp, _ := discovery.DetectApp(brokenDir)
			result, err = analysis.AnalyseApp(brokenApp, dict)
		} else {
			result, err = analysis.AnalyseApp(app, dict)
		}

		if err != nil {
			failed = append(failed, report.AppFailedEntry{Name: app.Name, Folder: app.Dir, Reason: err.Error()})
			continue
		}
		paths, err := report.WritePerAppReports(result, outputRoot, true)
		if err != nil {
			failed = append(failed, report.AppFailedEntry{Name: app.Name, Folder: app.Dir, Reason: err.Error()})
			continue
		}
		succeeded = append(succeeded, report.AppResult{Analysis: result, OutputPaths: paths})
	}

	// All 6 apps should "succeed" (broken XML produces warnings, not errors)
	// so this test validates that malformed XML doesn't kill the workspace run.
	if len(succeeded)+len(failed) != len(apps) {
		t.Errorf("expected %d total results, got succeeded=%d failed=%d", len(apps), len(succeeded), len(failed))
	}
	if len(succeeded) < len(apps)-1 {
		t.Errorf("too many failures: succeeded=%d, failed=%d", len(succeeded), len(failed))
	}
}
