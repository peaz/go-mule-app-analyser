package analysis

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/beevik/etree"
	"mule-app-analyser/internal/discovery"
)

const (
	simpleMediumStepsThreshold      = 5
	mediumComplexStepsThreshold     = 10
	simpleMediumTransformsThreshold = 2
	mediumComplexTransformsThreshold = 5
)

var triggerKeywordList = []string{
	"receive", "streaming", "listener", "poll", "scheduler", "inbound",
}

var drilldownComponentList = []string{
	"job", "choice", "when", "otherwise", "catch-exception-strategy",
	"batch", "scatter-gather", "async", "cache", "foreach",
	"parallel-foreach", "enricher", "poll", "request-reply",
	"until-successful", "transactional", "try", "error-handler",
	"on-error", "on-error-propagate", "on-error-continue",
	"first-successful", "route", "round-robin", "process-records",
	"step", "on-complete", "processor-chain",
}

var skipAttrKeys = []string{"doc:name", "doc:id", "doc:description"}

// analyser holds mutable state for one app analysis run.
type analyser struct {
	app                 discovery.AppRoot
	dict                *Dictionary
	muleProjectName     string
	muleAppPath         string
	muleVersion         string
	muleXMLs            []string
	configs             []Config
	componentList       []string
	componentDetailsMap map[string]map[string]string
	triggerList         []string
	complexityCount     FlowComplexityCount
	flowManifest        []Flow
	recommendationList  []Recommendation
	properties          map[string]string
	warnings            []string
}

// LoadDictionary parses the embedded recommendation dictionary JSON.
func LoadDictionary(data []byte) (*Dictionary, error) {
	var dict Dictionary
	if err := json.Unmarshal(data, &dict); err != nil {
		return nil, fmt.Errorf("loading dictionary: %w", err)
	}
	return &dict, nil
}

// AnalyseApp runs the full analysis pipeline for a single Mule app and returns
// its AppAnalysis. A non-nil error is returned only for unrecoverable failures
// (e.g. no XML files readable); partial results are surfaced via Warnings.
func AnalyseApp(app discovery.AppRoot, dict *Dictionary) (*AppAnalysis, error) {
	a := &analyser{
		app:                 app,
		dict:                dict,
		componentDetailsMap: make(map[string]map[string]string),
		properties:          make(map[string]string),
	}

	meta, muleVersion, metaWarnings := loadAppMetadata(app)
	a.muleProjectName = meta.ArtifactID
	a.muleAppPath = app.XMLPath
	a.muleVersion = muleVersion
	a.warnings = append(a.warnings, metaWarnings...)

	propMap, propWarnings := loadProperties(app.Dir)
	a.properties = propMap
	a.warnings = append(a.warnings, propWarnings...)

	if err := a.parseAllXMLs(); err != nil {
		return nil, err
	}

	a.tabulateFlowSteps()
	a.calculateFlowComplexity()
	a.generateRecommendations()

	inbound, outbound, unresolvedTokens := a.extractInterfaces()

	meta.MuleVersion = muleVersion
	ramlInfo := extractRAML(app.Dir)

	result := &AppAnalysis{
		MuleProjectName:     a.muleProjectName,
		MuleAppPath:         a.muleAppPath,
		MuleXMLs:            a.muleXMLs,
		MuleVersion:         a.muleVersion,
		Configs:             a.configs,
		ComponentList:       a.componentList,
		FlowComplexityCount: a.complexityCount,
		FlowManifest:        a.flowManifest,
		Triggers: Triggers{
			Count: len(a.triggerList),
			List:  a.triggerList,
		},
		DataWeaveTransforms: a.collectDataWeaveTransforms(),
		Recommendations:     a.recommendationList,
		AppMetadata:         meta,
		RAMLInfo:            ramlInfo,
		Properties:          a.properties,
		UnresolvedTokens:    unresolvedTokens,
		InboundInterfaces:   inbound,
		OutboundInterfaces:  outbound,
		Warnings:            a.warnings,
	}

	return result, nil
}

// parseAllXMLs walks the Mule XML directory and processes every XML file.
func (a *analyser) parseAllXMLs() error {
	err := filepath.Walk(a.muleAppPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(strings.ToLower(info.Name()), ".xml") {
			a.muleXMLs = append(a.muleXMLs, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking XML path %s: %w", a.muleAppPath, err)
	}

	for _, xmlPath := range a.muleXMLs {
		xmlDoc, err := loadXMLFile(xmlPath)
		if err != nil {
			a.warnings = append(a.warnings, fmt.Sprintf("skipping unreadable XML %s: %v", xmlPath, err))
			continue
		}
		a.processXMLRoot(xmlDoc.Root(), xmlPath)
	}

	if len(a.muleXMLs) > 0 && len(a.flowManifest) == 0 && len(a.componentList) == 0 {
		a.warnings = append(a.warnings, "no flows or components found — all XML files may be malformed or empty")
	}

	return nil
}

func (a *analyser) processXMLRoot(root *etree.Element, xmlPath string) {
	for _, component := range root.ChildElements() {
		tag := strings.ToLower(component.Tag)

		if strings.Contains(tag, "config") {
			a.parseConfig(component)
			continue
		}

		if strings.Contains(tag, "flow") || strings.Contains(tag, "sub-flow") || strings.Contains(tag, "job") {
			a.analyseFlowElements(*component, 0, xmlPath)
		}
	}
}

func (a *analyser) parseConfig(component *etree.Element) {
	configName := component.SelectAttrValue("name", "")
	attrs := getAttributes(*component)

	// Capture connection child element attributes (host, port, basePath, etc.)
	connAttrs := make(map[string]string)
	for _, child := range component.ChildElements() {
		if strings.Contains(strings.ToLower(child.FullTag()), "connection") {
			for k, v := range getAttributes(*child) {
				connAttrs[k] = v
			}
		}
	}

	cfg := Config{Name: configName, Attributes: attrs}
	if len(connAttrs) > 0 {
		cfg.ConnectionAttributes = connAttrs
	}
	a.configs = append(a.configs, cfg)
}

func (a *analyser) analyseFlowElements(flowElement etree.Element, indentLevel int, xmlPath string) {
	var flowObj Flow
	initFlowObj(&flowObj, flowElement.SelectAttrValue("name", ""), flowElement.FullTag())
	flowObj.SourceXML = xmlPath

	for _, component := range flowElement.ChildElements() {
		a.analyseComponent(&flowObj, *component, indentLevel)
	}
	flowObj.StandaloneSize = len(flowObj.FlowComponents)
	a.flowManifest = append(a.flowManifest, flowObj)
}

func (a *analyser) analyseComponent(flowObj *Flow, component etree.Element, indentLevel int) {
	componentTag := component.FullTag()
	attrs := extractAttributes(component)
	a.addComponent(componentTag, attrs)

	var fc FlowComponent
	switch {
	case inListContains(componentTag, triggerKeywordList):
		flowObj.Trigger = componentTag
		a.triggerList = append(a.triggerList, componentTag)
		fc = FlowComponent{IndentLevel: indentLevel, Component: fmt.Sprintf("Trigger: %s", componentTag), Attributes: attrs}

	case componentTag == "flow-ref" || componentTag == "execute":
		flowRefName := ""
		for _, attrib := range component.Attr {
			if strings.EqualFold(attrib.FullKey(), "name") {
				flowRefName = attrib.Value
			}
		}
		flowObj.FlowRefs = append(flowObj.FlowRefs, flowRefName)
		fc = FlowComponent{IndentLevel: indentLevel, Component: fmt.Sprintf("%s -> [%s]", componentTag, flowRefName), Attributes: attrs}

	case componentTag == "when":
		fc = FlowComponent{IndentLevel: indentLevel, Component: fmt.Sprintf("%s %s", componentTag, component.SelectAttrValue("expression", "")), Attributes: attrs}

	case strings.Contains(componentTag, "dw") || strings.Contains(componentTag, "transform"):
		flowObj.Transforms++
		dw := extractDataWeave(component)
		fc = FlowComponent{IndentLevel: indentLevel, Component: componentTag, Attributes: attrs, DataWeave: dw}

	default:
		fc = FlowComponent{IndentLevel: indentLevel, Component: componentTag, Attributes: attrs}
	}

	flowObj.FlowComponents = append(flowObj.FlowComponents, fc)

	if inList(componentTag, drilldownComponentList) {
		for _, sub := range component.ChildElements() {
			a.analyseComponent(flowObj, *sub, indentLevel+1)
		}
	}
}

func (a *analyser) tabulateFlowSteps() {
	for i := range a.flowManifest {
		a.flowManifest[i].MergedSize = a.recurseFlowRefSteps(a.flowManifest[i].Name)
	}
}

func (a *analyser) recurseFlowRefSteps(flowName string) int {
	exists, flowObj, steps := a.findFlowSteps(flowName)
	if exists {
		for _, ref := range flowObj.FlowRefs {
			steps += a.recurseFlowRefSteps(ref)
		}
	} else {
		steps = 1
	}
	return steps
}

func (a *analyser) findFlowSteps(flowName string) (bool, Flow, int) {
	for _, f := range a.flowManifest {
		if f.Name == flowName {
			return true, f, f.StandaloneSize
		}
	}
	return false, Flow{}, 1
}

func (a *analyser) calculateFlowComplexity() {
	for i := range a.flowManifest {
		steps := a.flowManifest[i].MergedSize
		transforms := a.flowManifest[i].Transforms

		switch {
		case steps < simpleMediumStepsThreshold && transforms < simpleMediumTransformsThreshold:
			a.complexityCount.Simple++
			a.flowManifest[i].Complexity = "simple"
		case steps < simpleMediumStepsThreshold:
			a.complexityCount.Medium++
			a.flowManifest[i].Complexity = "medium"
		case steps < mediumComplexStepsThreshold && transforms < mediumComplexTransformsThreshold:
			a.complexityCount.Medium++
			a.flowManifest[i].Complexity = "medium"
		default:
			a.complexityCount.Complex++
			a.flowManifest[i].Complexity = "complex"
		}
	}
}

func (a *analyser) generateRecommendations() {
	if a.dict == nil {
		return
	}
	for _, component := range a.componentList {
		found, rec := searchRecommendation(component, a.dict)
		if found {
			rec.FlowComponent = component
			if attrs, ok := a.componentDetailsMap[component]; ok && len(attrs) > 0 {
				rec.DetectedContext = attrs
			}
			a.recommendationList = append(a.recommendationList, rec)
		}
	}
}

func (a *analyser) collectDataWeaveTransforms() []DataWeaveTransform {
	var transforms []DataWeaveTransform
	for _, f := range a.flowManifest {
		for _, fc := range f.FlowComponents {
			if len(fc.DataWeave) > 0 {
				transforms = append(transforms, DataWeaveTransform{
					FlowName:  f.Name,
					Component: fc.Component,
					Scripts:   fc.DataWeave,
				})
			}
		}
	}
	return transforms
}

func (a *analyser) addComponent(componentName string, attrs map[string]string) {
	lower := strings.ToLower(componentName)
	if !inList(lower, a.componentList) {
		a.componentList = append(a.componentList, lower)
		if len(attrs) > 0 {
			a.componentDetailsMap[lower] = attrs
		}
	}
}

// searchRecommendation looks up the dictionary for a component substring match.
func searchRecommendation(component string, dict *Dictionary) (bool, Recommendation) {
	for _, entry := range dict.Recommendations {
		if strings.Contains(component, strings.ReplaceAll(entry.Component, "*", "")) {
			return true, Recommendation{
				Component:      entry.Component,
				Note:           entry.Note,
				Recommendation: entry.Recommendation,
			}
		}
	}
	return false, Recommendation{}
}

// getAttributes returns all XML attributes of an element as a map.
func getAttributes(component etree.Element) map[string]string {
	attr := make(map[string]string)
	for _, a := range component.Attr {
		attr[a.Key] = a.Value
	}
	return attr
}

// extractAttributes returns functional attributes for a component, skipping
// editor metadata. For scheduler and db components it also digs into child
// elements where the meaningful config is nested.
func extractAttributes(component etree.Element) map[string]string {
	attrs := make(map[string]string)
	tag := strings.ToLower(component.FullTag())

	for _, attr := range component.Attr {
		fullKey := attr.FullKey()
		if !inList(fullKey, skipAttrKeys) && strings.TrimSpace(attr.Value) != "" {
			attrs[fullKey] = attr.Value
		}
	}

	// Scheduler: cron/fixed-frequency nested inside <scheduling-strategy>
	if strings.Contains(tag, "scheduler") {
		for _, child := range component.ChildElements() {
			for _, gc := range child.ChildElements() {
				gcTag := strings.ToLower(gc.FullTag())
				if strings.Contains(gcTag, "cron") {
					if v := gc.SelectAttrValue("expression", ""); v != "" {
						attrs["cron-expression"] = v
					}
					if v := gc.SelectAttrValue("timeZone", ""); v != "" {
						attrs["cron-timezone"] = v
					}
				} else if strings.Contains(gcTag, "fixed-frequency") {
					if v := gc.SelectAttrValue("frequency", ""); v != "" {
						attrs["frequency"] = v
					}
					if v := gc.SelectAttrValue("timeUnit", ""); v != "" {
						attrs["timeUnit"] = v
					}
					if v := gc.SelectAttrValue("startDelay", ""); v != "" {
						attrs["startDelay"] = v
					}
				}
			}
		}
	}

	// DB: SQL text lives inside a <db:sql> child element
	if strings.HasPrefix(tag, "db:") {
		for _, child := range component.ChildElements() {
			cTag := strings.ToLower(child.FullTag())
			if strings.HasSuffix(cTag, ":sql") || cTag == "sql" {
				if sql := strings.TrimSpace(child.Text()); sql != "" {
					if len(sql) > 200 {
						sql = sql[:200] + "..."
					}
					attrs["sql"] = sql
				}
			}
		}
	}

	if len(attrs) == 0 {
		return nil
	}
	return attrs
}

// extractDataWeave walks a transform component's children and collects every
// DataWeave script tagged with its output target.
func extractDataWeave(component etree.Element) []DataWeaveScript {
	var scripts []DataWeaveScript

	for _, child := range component.ChildElements() {
		childTag := child.FullTag()

		// Mule 4: ee:message / ee:variables wrappers
		if childTag == "ee:message" || childTag == "ee:variables" {
			for _, gc := range child.ChildElements() {
				gcTag := gc.FullTag()
				var target string
				switch gcTag {
				case "ee:set-payload":
					target = "payload"
				case "ee:set-variable":
					varName := gc.SelectAttrValue("variableName", "")
					if varName != "" {
						target = "variable:" + varName
					} else {
						target = "variable"
					}
				default:
					target = gcTag
				}
				if script := strings.TrimSpace(gc.Text()); script != "" {
					scripts = append(scripts, DataWeaveScript{Target: target, Script: script})
				}
			}
		}

		// Mule 3: dw:set-payload, dw:set-variable, dw:input-payload
		if strings.HasPrefix(childTag, "dw:") {
			var target string
			switch childTag {
			case "dw:set-payload", "dw:input-payload":
				target = "payload"
			case "dw:set-variable":
				varName := child.SelectAttrValue("variableName", "")
				if varName != "" {
					target = "variable:" + varName
				} else {
					target = "variable"
				}
			default:
				target = childTag
			}
			if script := strings.TrimSpace(child.Text()); script != "" {
				scripts = append(scripts, DataWeaveScript{Target: target, Script: script})
			}
		}
	}

	if len(scripts) == 0 {
		return nil
	}
	return scripts
}

func initFlowObj(f *Flow, name, flowType string) {
	f.Name = name
	f.Type = flowType
	f.Complexity = ""
	f.Transforms = 0
	f.Trigger = ""
	f.FlowRefs = make([]string, 0)
	f.FlowComponents = make([]FlowComponent, 0)
	f.StandaloneSize = 0
	f.MergedSize = 0
}

func inList(search string, list []string) bool {
	for _, item := range list {
		if strings.EqualFold(search, item) {
			return true
		}
	}
	return false
}

func inListContains(search string, list []string) bool {
	for _, item := range list {
		if strings.Contains(strings.ToLower(search), item) {
			return true
		}
	}
	return false
}

// SortedKeys returns the sorted keys of a string map (used by templates).
func SortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
