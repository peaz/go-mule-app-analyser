package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/beevik/etree"
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/parser"
)

// declare constants
const analyserVersion = `1.0.1`
const htmlCSSStyle = `@media print{*,:after,:before{background:0 0!important;color:#000!important;box-shadow:none!important;text-shadow:none!important}a,a:visited{text-decoration:underline}a[href]:after{content:" (" attr(href) ")"}abbr[title]:after{content:" (" attr(title) ")"}a[href^="#"]:after,a[href^="javascript:"]:after{content:""}blockquote,pre{border:1px solid #999;page-break-inside:avoid}thead{display:table-header-group}img,tr{page-break-inside:avoid}img{max-width:100%!important}h2,h3,p{orphans:3;widows:3}h2,h3{page-break-after:avoid}}code,pre{font-family:Menlo,Monaco,"Courier New",monospace}pre{padding:.5rem;line-height:1.25;overflow-x:scroll}a,a:visited{color:#3498db}a:active,a:focus,a:hover{color:#2980b9}.modest-no-decoration{text-decoration:none}html{font-size:12px}@media screen and (min-width:32rem) and (max-width:48rem){html{font-size:15px}}@media screen and (min-width:48rem){html{font-size:16px}}body{line-height:1.85}.modest-p,p{font-size:1rem;margin-bottom:1.3rem}.modest-h1,.modest-h2,.modest-h3,.modest-h4,h1,h2,h3,h4{margin:1.414rem 0 .5rem;font-weight:inherit;line-height:1.42}.modest-h1,h1{margin-top:0;font-size:3.998rem;font-weight:500}.modest-h2,h2{font-size:2.827rem}.modest-h3,h3{font-size:1.999rem}.modest-h4,h4{font-size:1.414rem}.modest-h5,h5{font-size:1.121rem}.modest-h6,h6{font-size:.88rem}.modest-small,small{font-size:.707em}canvas,iframe,img,select,svg,textarea,video{max-width:100%}html{font-size:18px;max-width:100%}body{color:#444;font-family:Poppins,sans-serif;font-weight:300;margin:0 auto;max-width:60rem;line-height:1.45;padding:.25rem}h1,h2,h3,h4,h5,h6{font-family:Poppins,Helvetica,sans-serif}h1,h2,h3{border-bottom:2px solid #fafafa;margin-bottom:1.15rem;padding-bottom:.5rem;text-align:left}blockquote{border-left:8px solid #fafafa;padding:1rem}code,pre{background-color:#fafafa;font-size:small}table{border-collapse:collapse;margin:25px 0;min-width:400px;box-shadow:0 0 20px rgba(0,0,0,.15)}thead tr{background-color:#3ea2a8;color:#fff}td,th{padding:12px 15px}tbody tr{border-bottom:1px solid #ddd}tbody tr:nth-of-type(even){background-color:#f3f3f3}tbody tr:last-of-type{border-bottom:2px solid #009879}hr{border:.5px solid #3ea2a8;margin:auto}`
const htmlHeader = `<!DOCTYPE html><html><head><style>` + htmlCSSStyle + `</style></head><body>`
const htmlFooter = `</body></html>`

const dictionaryFile = `resources/dictionary.json`
const mdTemplateFile = `resources/md.tmpl`
const mule3AppPath = `src/main/app/`
const mule4AppPath = `src/main/mule/`

// const winMule3AppPath = `src\\main\\app\\`
// const winMule4AppPath = `src\\main\\mule\\`

const simpleMediumStepsThreshold = 5
const mediumComplexStepsThreshold = 10
const simpleMediumTransformsThreshold = 2
const mediumComplexTransformsThreshold = 5

var triggerKeywordList = []string{
	"receive",
	"streaming",
	"listener",
	"poll",
	"scheduler",
	"inbound",
}

var drilldownComponentList = []string{
	"job",
	"choice",
	"when",
	"otherwise",
	"catch-exception-strategy",
	"batch",
	"scatter-gather",
	"async",
	"cache",
	"foreach",
	"enricher",
	"poll",
	"request-reply",
	"until-successful",
	"transactional",
	"try",
	"error-handler",
	"on-error",
	"first-successful",
	"route",
	"round-robin",
	"until-successful",
	"process-records",
	"step",
	"on-complete",
}

// data structures
type config struct {
	Name       string            `json:"name"`
	Attributes map[string]string `json:"attributes"`
}

type flow struct {
	Name           string          `json:"name"`
	Type           string          `json:"type"`
	Trigger        string          `json:"trigger"`
	Transforms     int             `json:"transforms"`
	FlowRefs       []string        `json:"flowRefs"`
	StandaloneSize int             `json:"standaloneSize"`
	MergedSize     int             `json:"mergedSize"`
	Complexity     string          `json:"complexity"`
	FlowComponents []flowComponent `json:"flowComponents"`
}

type flowComponent struct {
	IndentLevel int    `json:"indentLevel"`
	Component   string `json:"component"`
}

type triggers struct {
	Count int      `json:"count"`
	List  []string `json:"list"`
}

type flowComplexityCount struct {
	Simple  int `json:"simple"`
	Medium  int `json:"medium"`
	Complex int `json:"complex"`
}

type rawData struct {
	MuleProjectName     string              `json:"muleProjectName"`
	MuleAppPath         string              `json:"muleAppPath"`
	MuleXMLs            []string            `json:"muleXMLs"`
	MuleVersion         string              `json:"muleVersion"`
	Configs             []config            `json:"configs"`
	ComponentList       []string            `json:"componentList"`
	FlowComplexityCount flowComplexityCount `json:"flowComplexityCount"`
	FlowManifest        []flow              `json:"flowManifest"`
	Triggers            triggers            `json:"triggers"`
	Recommendations     []recommendation    `json:"recommendations"`
}

type recommendation struct {
	FlowComponent  string `json:"flowComponent"`
	Component      string `json:"component"`
	Note           string `json:"note"`
	Recommendation string `json:"recommendation"`
}
type recommendations struct {
	Recommendations []recommendation `json:"recommendations"`
}

// variable inits
var (
	muleProjectName    string
	muleAppPath        string
	muleVersion        string
	muleXMLs           = make([]string, 0)
	configs            = make([]config, 0)
	componentList      = make([]string, 0)
	triggerList        = make([]string, 0)
	complexityCount    = flowComplexityCount{Simple: 0, Medium: 0, Complex: 0}
	flowManifest       = make([]flow, 0)
	dictionary         recommendations
	recommendationList = make([]recommendation, 0)
	rawAnalysisData    rawData
	mdReportBuffer     bytes.Buffer
	flagPath           *string
)

/*
** Main Fuction
 */

func main() {
	printGreeting()
	initReportFolder()
	getCmdFlags()
	getMuleAppDetails(*flagPath)
	analyseMuleXMLs()
	tabulateFlowSteps()
	calculateFlowComplexity()
	loadRecommendationDictionary()
	generateRecommendations()
	generateRawData()
	generateMDReport()
	generateHTMLReport()
	printGoodbye()
}

func printGreeting() {
	fmt.Printf("Mule App Analyser\n")
	fmt.Printf("Version: %s\n", analyserVersion)
}

func printGoodbye() {
	fmt.Printf("Analysis completed. The following files have been generated in the reports directory.\n")
	fmt.Printf("1. Raw Data (JSON format)   : rawAnalysisData-%s.json\n", muleProjectName)
	fmt.Printf("2. Report (Markdown format) : report-%s.md\n", muleProjectName)
	fmt.Printf("3. Report (HTML format)     : report-%s.html\n", muleProjectName)
}

func initReportFolder() {
	_, err := os.ReadDir("./reports")
	if errors.Is(err, os.ErrNotExist) {
		os.Mkdir("reports", os.FileMode(0755))
	}
}

func getCmdFlags() {
	flagPath = flag.String("p", "", "Provide the root path of the Mule App project directory")
	// var rFlag = flag.String("r", "true", "Enable recommendation in the report")
	flag.Parse()

	if *flagPath == "" {
		fmt.Println("You need to provide the required paramenters.")
		flag.PrintDefaults()
		os.Exit(1)
	}
}

func getMuleAppDetails(path string) {
	//handle OS paths to /
	p := filepath.ToSlash(path)

	//check if the path exist
	_, err := os.ReadDir(p)
	if err != nil {
		fmt.Printf("The path [%s] is invalid or does not exist. Please check and try again.\n", path)
		os.Exit(1)
	}

	//make sure path ends with a /
	if p[len(p)-1] != '/' {
		p += "/"
	}

	//set muleProjectName from the path
	pArray := strings.Split(p, "/")
	muleProjectName = pArray[len(pArray)-2]

	//check for version of the Mule app
	fullMule3path := p + mule3AppPath
	fullMule4path := p + mule4AppPath

	//check if Mule App is version 3
	_, errPath := os.ReadDir(fullMule3path)
	if errPath == nil {
		//Mule 3 project found
		muleAppPath = fullMule3path
		muleProjectXML, err := loadXML(p + "mule-project.xml")
		if err == nil {
			//Get exact version from mule-project.xml
			muleVersion = strings.Replace(muleProjectXML.Root().SelectAttrValue("runtimeId", "3.x"), "org.mule.tooling.server.", "", -1)
			//update project name as set in mule-project.xml
			muleProjectName = muleProjectXML.Root().FindElement("name").Text()
		} else {
			muleVersion = "3.x - Exact version unknown. Missing mule-project.xml file."
		}
	} else {
		//test for Mule App 4
		_, errPath = os.ReadDir(fullMule4path)
		if errPath == nil {
			//Mule 4 project found
			muleAppPath = fullMule4path
			muleArtifactJSON, err := os.Open(p + "mule-artifact.json")
			if err == nil {
				//Get exact target version from mule-artifact.json
				defer muleArtifactJSON.Close()
				byteValue, _ := io.ReadAll(muleArtifactJSON)
				var j map[string]interface{}
				json.Unmarshal([]byte(byteValue), &j)
				muleVersion = j["minMuleVersion"].(string)
			} else {
				muleVersion = "4.x - Exact version unknown. Missing mule-project.xml file."
			}
		} else {
			fmt.Printf("Provided path [%s] is not a valid Mule app project base directory.\n", p)
			os.Exit(1)
		}
	}
}

func analyseMuleXMLs() {

	//get list of Mule XMLs
	// files, err := os.ReadDir(muleAppPath)
	// if err != nil {
	// 	panic(err)
	// }
	// for _, file := range files {
	// 	if strings.Contains(file.Name(), ".xml") {
	// 		muleXMLs = append(muleXMLs, file.Name())
	// 	}
	// }

	err := filepath.Walk(muleAppPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(strings.ToLower(info.Name()), ".xml") {
			muleXMLs = append(muleXMLs, path)
		}

		return nil
	})
	if err != nil {
		panic(err)
	}

	for _, muleXML := range muleXMLs {
		// xmlDoc, err := loadXML(muleAppPath + muleXML)
		xmlDoc, err := loadXML(muleXML)
		if err != nil {
			panic(err)
		}

		muleXMLRoot := xmlDoc.Root()

		for _, component := range muleXMLRoot.ChildElements() {

			// process config related tags
			if strings.Contains(strings.ToLower(component.Tag), "config") {
				configName := component.SelectAttrValue("name", "")
				config := config{Name: configName, Attributes: getAttributes(*component)}
				configs = append(configs, config)
			}

			//process flow tags
			if strings.Contains(strings.ToLower(component.Tag), "flow") || strings.Contains(strings.ToLower(component.Tag), "sub-flow") || strings.Contains(strings.ToLower(component.Tag), "job") {
				//rename job to batchJob
				analyseFlowElements(*component, 0)
			}
		}
	}

}

func getAttributes(component etree.Element) map[string]string {
	attr := make(map[string]string)
	for _, attribute := range component.Attr {
		attr[attribute.Key] = attribute.Value
	}
	return attr
}

func calculateFlowComplexity() {
	for i := 0; i < len(flowManifest); i++ {
		stepsCount := flowManifest[i].MergedSize
		transformsCount := flowManifest[i].Transforms
		if stepsCount < simpleMediumStepsThreshold && transformsCount < simpleMediumTransformsThreshold {
			complexityCount.Simple += 1
			flowManifest[i].Complexity = "simple"
		} else if stepsCount < simpleMediumStepsThreshold && transformsCount >= simpleMediumTransformsThreshold {
			complexityCount.Simple += 1
			flowManifest[i].Complexity = "medium"
		} else if (simpleMediumStepsThreshold <= stepsCount) && (stepsCount < mediumComplexStepsThreshold) && (transformsCount < mediumComplexTransformsThreshold) {
			complexityCount.Medium += 1
			flowManifest[i].Complexity = "medium"
		} else if (mediumComplexStepsThreshold <= stepsCount) || (transformsCount >= mediumComplexTransformsThreshold) {
			complexityCount.Complex += 1
			flowManifest[i].Complexity = "complex"
		}
	}
}

func loadRecommendationDictionary() {
	f, err := os.ReadFile(dictionaryFile)
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal(f, &dictionary)
	if err != nil {
		panic(err)
	}
}

func searchRecommendation(component string) (bool, recommendation) {
	for _, recommendation := range dictionary.Recommendations {
		if strings.Contains(component, strings.Replace(recommendation.Component, "*", "", -1)) {
			return true, recommendation
		}
	}
	return false, recommendation{}
}

func generateRecommendations() {
	for _, component := range componentList {
		found, recommendation := searchRecommendation(component)
		if found {
			recommendation.FlowComponent = component
			recommendationList = append(recommendationList, recommendation)
		}
	}
}

func generateRawData() {
	rawAnalysisData.MuleProjectName = muleProjectName
	rawAnalysisData.MuleAppPath = muleAppPath
	rawAnalysisData.MuleXMLs = muleXMLs
	rawAnalysisData.MuleVersion = muleVersion
	rawAnalysisData.Configs = configs
	rawAnalysisData.ComponentList = componentList
	rawAnalysisData.FlowComplexityCount = complexityCount
	rawAnalysisData.FlowManifest = flowManifest
	rawAnalysisData.Triggers.Count = len(triggerList)
	rawAnalysisData.Triggers.List = triggerList
	rawAnalysisData.Recommendations = recommendationList
	os.WriteFile("./reports/rawAnalysisData-"+muleProjectName+".json", []byte(encodeJSONString(rawAnalysisData)), os.FileMode(0755))
}

func generateMDReport() {
	funcMap := template.FuncMap{
		// The name "inc" is what the function will be called in the template text.
		"sum": func(i ...int) int {
			result := 0
			for _, v := range i {
				result += v
			}
			return result
		},
		"indentLine": func(spaces int) string {
			spaceLine := ""
			for i := 1; i <= spaces; i++ {
				spaceLine += "   "
			}
			return spaceLine + "|_"
		},
		"getFlowSize": func(flowName string) int {
			size := 1
			for _, flow := range flowManifest {
				if strings.EqualFold(flow.Name, flowName) {
					size = flow.StandaloneSize
				}
			}
			return size
		},
	}
	t, err := template.New("markdownTmpl").Funcs(funcMap).ParseFiles(mdTemplateFile)
	if err != nil {
		panic(err)
	}

	f, err := os.Create("./reports/report-" + muleProjectName + ".md")
	if err != nil {
		panic(err)
	}

	err = t.ExecuteTemplate(&mdReportBuffer, "md.tmpl", rawAnalysisData)
	if err != nil {
		panic(err)
	}

	f.Write(mdReportBuffer.Bytes())
	f.Close()
}

func generateHTMLReport() {
	extensions := parser.CommonExtensions | parser.AutoHeadingIDs
	parser := parser.NewWithExtensions(extensions)

	md := []byte(mdReportBuffer.Bytes())
	htmlStr := htmlHeader + string(markdown.ToHTML(md, parser, nil)) + htmlFooter
	os.WriteFile("./reports/report-"+muleProjectName+".html", []byte(htmlStr), os.FileMode(0755))
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
		if strings.Contains(search, item) {
			return true
		}
	}
	return false
}

func addComponent(componentName string) {
	if !inList(componentName, componentList) {
		componentList = append(componentList, strings.ToLower(componentName))
	}
}

func loadXML(xmlpath string) (etree.Document, error) {
	doc := etree.NewDocument()
	err := doc.ReadFromFile(xmlpath)
	return *doc, err
}

func initFlowObj(flowObj *flow, flowName string, flowType string) {
	flowObj.Name = flowName
	flowObj.Type = flowType
	flowObj.Complexity = ""
	flowObj.Transforms = 0
	flowObj.Trigger = ""
	flowObj.FlowRefs = make([]string, 0)
	flowObj.FlowComponents = make([]flowComponent, 0)
	flowObj.StandaloneSize = 0
	flowObj.MergedSize = 0
}

func analyseFlowElements(flowElement etree.Element, indentLevel int) {
	flowObj, flowName, flowType := flow{}, flowElement.SelectAttrValue("name", ""), flowElement.FullTag()
	initFlowObj(&flowObj, flowName, flowType)

	for _, component := range flowElement.ChildElements() {
		analyseComponent(&flowObj, *component, indentLevel)
	}
	flowObj.StandaloneSize = len(flowObj.FlowComponents)

	//append new flow in FlowManifest
	flowManifest = append(flowManifest, flowObj)
}

func analyseComponent(flowObj *flow, component etree.Element, indentLevel int) {
	componentTag := component.FullTag()

	//add unique component to componentList
	addComponent(componentTag)

	//handle specifc type of components
	if inListContains(componentTag, triggerKeywordList) {
		flowObj.Trigger = componentTag
		triggerList = append(triggerList, componentTag)
		flowObj.FlowComponents = append(flowObj.FlowComponents, flowComponent{IndentLevel: indentLevel, Component: fmt.Sprintf("Trigger: %s", componentTag)})
	} else if componentTag == "flow-ref" || componentTag == "execute" {
		flowRefName := ""
		for _, attrib := range component.Attr {
			if strings.EqualFold(attrib.FullKey(), "name") {
				flowRefName = attrib.Value
			}
		}
		flowObj.FlowRefs = append(flowObj.FlowRefs, flowRefName)
		flowObj.FlowComponents = append(flowObj.FlowComponents, flowComponent{IndentLevel: indentLevel, Component: fmt.Sprintf("%s -> [%s]", componentTag, flowRefName)})
	} else if componentTag == "when" {
		flowObj.FlowComponents = append(flowObj.FlowComponents, flowComponent{IndentLevel: indentLevel, Component: fmt.Sprintf("%s %s", componentTag, component.SelectAttrValue("expression", ""))})
	} else if strings.Contains(componentTag, "dw") || strings.Contains(componentTag, "transform") {
		flowObj.Transforms += 1
		flowObj.FlowComponents = append(flowObj.FlowComponents, flowComponent{IndentLevel: indentLevel, Component: componentTag})
	} else {
		flowObj.FlowComponents = append(flowObj.FlowComponents, flowComponent{IndentLevel: indentLevel, Component: componentTag})
	}

	//check if the we need to drill down the component
	if inList(componentTag, drilldownComponentList) {
		for _, subComponent := range component.ChildElements() {
			analyseComponent(flowObj, *subComponent, indentLevel+1)
		}
	} else {
		return
	}
}

func tabulateFlowSteps() {
	for i := 0; i < len(flowManifest); i++ {
		flowManifest[i].MergedSize = recurseFlowRefSteps(flowManifest[i].Name)
	}
}

func recurseFlowRefSteps(flowName string) int {
	flowExist, flowObj, steps := ifFlowExistReturnSteps(flowName)
	if flowExist {
		for _, flowRef := range flowObj.FlowRefs {
			steps += recurseFlowRefSteps(flowRef)
		}
	} else {
		steps = 1
	}
	return steps
}

func ifFlowExistReturnSteps(flowName string) (bool, flow, int) {
	exist, flowObj, steps := false, flow{}, 1
	for _, flow := range flowManifest {
		if flow.Name == flowName {
			exist = true
			steps = flow.StandaloneSize
			flowObj = flow
			break
		}
	}
	return exist, flowObj, steps
}

func encodeJSONString(i interface{}) string {
	bf := bytes.NewBuffer([]byte{})
	jsonEncoder := json.NewEncoder(bf)
	jsonEncoder.SetEscapeHTML(false)
	jsonEncoder.SetIndent("", "  ")
	jsonEncoder.Encode(i)
	return bf.String()
}
