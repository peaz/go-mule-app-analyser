package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"mule-app-analyser/internal/analysis"
	"mule-app-analyser/internal/discovery"
	"mule-app-analyser/internal/linker"
	"mule-app-analyser/internal/report"
	"mule-app-analyser/internal/resources"
)

func main() {
	flagPath := flag.String("p", "", "Path to a single Mule app root directory (single-app mode)")
	flagWorkspace := flag.String("workspace", "", "Path to workspace root containing multiple Mule apps")
	flagOutput := flag.String("output", "./reports", "Output root directory for generated reports")
	flagDeps := flag.Bool("dependencies", true, "Build cross-app dependency graph (workspace mode only)")
	flagMinConf := flag.Int("min-confidence", 20, "Minimum confidence score (0-100) to include an edge in the graph")
	flag.Parse()

	fmt.Printf("Mule App Analyser\nVersion: %s\n\n", report.AnalyserVersion())

	if *flagPath == "" && *flagWorkspace == "" {
		fmt.Fprintln(os.Stderr, "Error: provide -p <app-path> for single-app mode or -workspace <dir> for workspace mode.")
		flag.PrintDefaults()
		os.Exit(1)
	}
	if *flagPath != "" && *flagWorkspace != "" {
		fmt.Fprintln(os.Stderr, "Error: -p and -workspace are mutually exclusive.")
		flag.PrintDefaults()
		os.Exit(1)
	}

	dict, err := analysis.LoadDictionary(resources.DictionaryJSON)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading dictionary: %v\n", err)
		os.Exit(1)
	}

	outputRoot := *flagOutput
	if err := report.EnsureOutputRoot(outputRoot); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output directory %s: %v\n", outputRoot, err)
		os.Exit(1)
	}

	if *flagPath != "" {
		runSingleApp(*flagPath, outputRoot, dict)
	} else {
		runWorkspace(*flagWorkspace, outputRoot, dict, *flagDeps, *flagMinConf)
	}
}

func runSingleApp(appPath, outputRoot string, dict *analysis.Dictionary) {
	appPath = filepath.ToSlash(appPath)

	app, ok := discovery.DetectApp(appPath)
	if !ok {
		fmt.Fprintf(os.Stderr, "Error: %s is not a valid Mule app directory (missing mule-artifact.json/mule-project.xml or src/main/mule|app).\n", appPath)
		os.Exit(1)
	}

	fmt.Printf("Analysing: %s (Mule %d)\n", app.Name, app.MuleVersion)

	result, err := analysis.AnalyseApp(app, dict)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error analysing %s: %v\n", app.Name, err)
		os.Exit(1)
	}

	printWarnings(result.Warnings)

	paths, err := report.WritePerAppReports(result, outputRoot, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing reports for %s: %v\n", app.Name, err)
		os.Exit(1)
	}

	fmt.Printf("\nAnalysis complete. Reports written to %s:\n", outputRoot)
	fmt.Printf("  JSON : %s\n", paths["json"])
	fmt.Printf("  MD   : %s\n", paths["md"])
	fmt.Printf("  HTML : %s\n", paths["html"])
}

func runWorkspace(workspaceRoot, outputRoot string, dict *analysis.Dictionary, runDeps bool, minConf int) {
	workspaceRoot = filepath.ToSlash(workspaceRoot)

	apps, err := discovery.DiscoverApps(workspaceRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error scanning workspace %s: %v\n", workspaceRoot, err)
		os.Exit(1)
	}
	if len(apps) == 0 {
		fmt.Fprintf(os.Stderr, "No Mule apps found under %s\n", workspaceRoot)
		os.Exit(1)
	}

	fmt.Printf("Discovered %d app(s) in %s\n\n", len(apps), workspaceRoot)

	var succeeded []report.AppResult
	var failed []report.AppFailedEntry
	var analysed []*analysis.AppAnalysis

	for _, app := range apps {
		fmt.Printf("  Analysing: %s (Mule %d)...", app.Name, app.MuleVersion)

		result, err := analysis.AnalyseApp(app, dict)
		if err != nil {
			fmt.Printf(" FAILED\n")
			failed = append(failed, report.AppFailedEntry{Name: app.Name, Folder: app.Dir, Reason: err.Error()})
			continue
		}

		paths, err := report.WritePerAppReports(result, outputRoot, true)
		if err != nil {
			fmt.Printf(" FAILED (report write)\n")
			failed = append(failed, report.AppFailedEntry{Name: app.Name, Folder: app.Dir, Reason: err.Error()})
			continue
		}

		fmt.Printf(" done\n")
		printWarnings(result.Warnings)

		succeeded = append(succeeded, report.AppResult{Analysis: result, OutputPaths: paths})
		analysed = append(analysed, result)
	}

	// Write workspace index
	absWorkspace, _ := filepath.Abs(workspaceRoot)
	idx := report.NewWorkspaceIndex(absWorkspace, succeeded, failed)

	// Dependency graph
	var graph *linker.DependencyGraph
	var graphPath string
	if runDeps && len(analysed) >= 2 {
		fmt.Printf("\nBuilding dependency graph (min-confidence=%d)...\n", minConf)
		graph = linker.BuildGraph(analysed, absWorkspace, minConf)
		graphPath, err = report.WriteDependencyGraph(graph, outputRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not write dependency-graph.json: %v\n", err)
		} else {
			fmt.Printf("  Edges found   : %d (+ %d below threshold)\n", len(graph.Edges), len(graph.IgnoredEdges))
		}
	}

	// Overall workspace report
	wr := report.NewWorkspaceReport(absWorkspace, succeeded, graph)
	overallPaths, err := report.WriteOverallReport(wr, outputRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write overall report: %v\n", err)
	}

	indexPath, err := report.WriteWorkspaceIndex(idx, outputRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write index.json: %v\n", err)
	}

	fmt.Printf("\nWorkspace analysis complete.\n")
	fmt.Printf("  Apps analysed : %d\n", len(succeeded))
	if len(failed) > 0 {
		fmt.Printf("  Apps failed   : %d\n", len(failed))
		for _, f := range failed {
			fmt.Printf("    - %s: %s\n", f.Name, f.Reason)
		}
	}
	fmt.Printf("  Index         : %s\n", indexPath)
	if graphPath != "" {
		fmt.Printf("  Dep graph     : %s\n", graphPath)
	}
	if overallPaths["html"] != "" {
		fmt.Printf("  Overall report: %s\n", overallPaths["html"])
	}
	fmt.Printf("  Reports root  : %s\n", outputRoot)
}

func printWarnings(warnings []string) {
	for _, w := range warnings {
		fmt.Printf("    [warn] %s\n", w)
	}
}
