package analysis

// Config represents a Mule global connector configuration element.
type Config struct {
	Name                 string            `json:"name"`
	Attributes           map[string]string `json:"attributes"`
	ConnectionAttributes map[string]string `json:"connectionAttributes,omitempty"`
}

// Flow represents a Mule flow, sub-flow, or batch job.
type Flow struct {
	Name           string          `json:"name"`
	Type           string          `json:"type"`
	Trigger        string          `json:"trigger"`
	Transforms     int             `json:"transforms"`
	FlowRefs       []string        `json:"flowRefs"`
	StandaloneSize int             `json:"standaloneSize"`
	MergedSize     int             `json:"mergedSize"`
	Complexity     string          `json:"complexity"`
	FlowComponents []FlowComponent `json:"flowComponents"`
	SourceXML      string          `json:"sourceXML,omitempty"`
}

// FlowComponent is an individual step inside a flow.
type FlowComponent struct {
	IndentLevel int               `json:"indentLevel"`
	Component   string            `json:"component"`
	Attributes  map[string]string `json:"attributes,omitempty"`
	DataWeave   []DataWeaveScript `json:"dataWeave,omitempty"`
}

// DataWeaveScript is an extracted DataWeave transformation script.
type DataWeaveScript struct {
	Target string `json:"target"`
	Script string `json:"script"`
}

// DataWeaveTransform groups DataWeave scripts by flow and component.
type DataWeaveTransform struct {
	FlowName  string            `json:"flowName"`
	Component string            `json:"component"`
	Scripts   []DataWeaveScript `json:"scripts"`
}

// Triggers is a summary of detected trigger components.
type Triggers struct {
	Count int      `json:"count"`
	List  []string `json:"list"`
}

// FlowComplexityCount is the distribution of simple/medium/complex flows.
type FlowComplexityCount struct {
	Simple  int `json:"simple"`
	Medium  int `json:"medium"`
	Complex int `json:"complex"`
}

// AppMetadata holds identity fields extracted from pom.xml and mule-artifact.json.
type AppMetadata struct {
	Folder      string `json:"folder"`
	ArtifactID  string `json:"artifactId"`
	GroupID     string `json:"groupId"`
	Version     string `json:"version"`
	MuleVersion string `json:"muleVersion"`
}

// Interface is an inbound or outbound integration endpoint found in a flow.
type Interface struct {
	Protocol   string            `json:"protocol"`
	Direction  string            `json:"direction"` // "inbound" or "outbound"
	FlowName   string            `json:"flowName"`
	RawValue   string            `json:"rawValue"`
	Resolved   string            `json:"resolved"`
	IsResolved bool              `json:"isResolved"`
	Source     string            `json:"source,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// Recommendation is a migration suggestion for a detected Mule component.
type Recommendation struct {
	FlowComponent   string            `json:"flowComponent"`
	Component       string            `json:"component"`
	Note            string            `json:"note"`
	Recommendation  string            `json:"recommendation"`
	DetectedContext map[string]string `json:"detectedContext,omitempty"`
}

// DictionaryEntry is a single entry in the migration recommendations dictionary.
type DictionaryEntry struct {
	Component      string `json:"component"`
	Note           string `json:"note"`
	Recommendation string `json:"recommendation"`
}

// Dictionary holds all loaded migration recommendation entries.
type Dictionary struct {
	Recommendations []DictionaryEntry `json:"recommendations"`
}

// AppAnalysis is the complete analysis output for a single Mule app.
// V1-compatible fields are preserved in their original positions.
type AppAnalysis struct {
	// V1 fields (preserved for single-app backward compat)
	MuleProjectName     string               `json:"muleProjectName"`
	MuleAppPath         string               `json:"muleAppPath"`
	MuleXMLs            []string             `json:"muleXMLs"`
	MuleVersion         string               `json:"muleVersion"`
	Configs             []Config             `json:"configs"`
	ComponentList       []string             `json:"componentList"`
	FlowComplexityCount FlowComplexityCount  `json:"flowComplexityCount"`
	FlowManifest        []Flow               `json:"flowManifest"`
	Triggers            Triggers             `json:"triggers"`
	DataWeaveTransforms []DataWeaveTransform `json:"dataWeaveTransforms"`
	Recommendations     []Recommendation     `json:"recommendations"`

	// V2 additions
	AppMetadata        AppMetadata       `json:"appMetadata"`
	RAMLInfo           *RAMLInfo         `json:"ramlInfo,omitempty"`
	Properties         map[string]string `json:"properties,omitempty"`
	UnresolvedTokens   []string          `json:"unresolvedTokens,omitempty"`
	InboundInterfaces  []Interface       `json:"inboundInterfaces,omitempty"`
	OutboundInterfaces []Interface       `json:"outboundInterfaces,omitempty"`
	Warnings           []string          `json:"warnings,omitempty"`
}
