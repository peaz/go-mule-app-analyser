// Package linker implements deterministic cross-app dependency detection.
// It operates purely on the extracted AppAnalysis data from each app with no AI.
package linker

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"mule-app-analyser/internal/analysis"
)

// --- types ---

// Evidence is one piece of supporting information for a dependency edge.
type Evidence struct {
	Rule        string `json:"rule"`
	Description string `json:"description"`
	Weight      int    `json:"weight"`
	FromValue   string `json:"fromValue"`
	ToValue     string `json:"toValue"`
}

// Edge is a directed dependency between two apps.
type Edge struct {
	FromApp         string     `json:"fromApp"`
	ToApp           string     `json:"toApp"`
	Protocol        string     `json:"protocol"`
	Confidence      int        `json:"confidence"`
	ConfidenceLevel string     `json:"confidenceLevel"`
	Evidence        []Evidence `json:"evidence"`
}

// DependencyGraph is the workspace-level cross-app dependency output.
type DependencyGraph struct {
	GeneratedAt   string `json:"generatedAt"`
	WorkspaceRoot string `json:"workspaceRoot"`
	MinConfidence int    `json:"minConfidence"`
	// Edges contains every dependency at or above MinConfidence.
	Edges []Edge `json:"edges"`
	// IgnoredEdges contains potential connections that scored below MinConfidence.
	IgnoredEdges []Edge `json:"ignoredEdges,omitempty"`
}

// --- scoring helpers ---

func scoreEvidence(ev []Evidence) int {
	total := 0
	for _, e := range ev {
		total += e.Weight
	}
	if total > 100 {
		total = 100
	}
	return total
}

func confidenceLevel(score int) string {
	switch {
	case score >= 80:
		return "high"
	case score >= 50:
		return "medium"
	case score >= 20:
		return "low"
	default:
		return "ignored"
	}
}

// deduplicateEvidence keeps only the highest-scoring entry per rule name.
// Rules that express structural identity (naming, RAML) should only count
// once regardless of how many outbound interfaces fired them.
// Rules expressing concrete value matches (exact-match, same-token) are
// deduplicated on (rule, fromValue) so each unique endpoint counts.
var structuralRules = map[string]bool{
	// Naming/identity rules: fire at most once per edge regardless of how many
	// interfaces trigger them — having 30 paths with the same naming pattern
	// does not make the connection 30× more certain.
	"http.config-ref-naming":     true,
	"http.path-token-app-match":  true,
	"http.raml-baseuri":          true,
	"http.raml-title":            true,
	"jms.shared-config-ref":      true,
	"sqs.shared-config-ref":      true,
	"kafka.shared-config-ref":    true,
	"amqp.shared-config-ref":     true,
	"vm.shared-config-ref":       true,
}

func deduplicateEvidence(ev []Evidence) []Evidence {
	// For structural rules: best score per rule.
	// For content rules: best score per (rule, fromValue).
	bestStructural := make(map[string]Evidence)  // rule → best
	bestContent := make(map[string]Evidence)     // rule|fromValue → best

	for _, e := range ev {
		if structuralRules[e.Rule] {
			if existing, ok := bestStructural[e.Rule]; !ok || e.Weight > existing.Weight {
				bestStructural[e.Rule] = e
			}
		} else {
			key := e.Rule + "|" + e.FromValue
			if existing, ok := bestContent[key]; !ok || e.Weight > existing.Weight {
				bestContent[key] = e
			}
		}
	}

	result := make([]Evidence, 0, len(bestStructural)+len(bestContent))
	for _, e := range bestStructural {
		result = append(result, e)
	}
	for _, e := range bestContent {
		result = append(result, e)
	}
	return result
}

// --- appContext ---

// appContext pre-processes one app's data for efficient matching.
type appContext struct {
	app         *analysis.AppAnalysis
	configIndex map[string]analysis.Config // config name → Config (case-insensitive key)
	nameTokens  []string                   // meaningful tokens extracted from artifactId
}

func newAppContext(app *analysis.AppAnalysis) *appContext {
	ctx := &appContext{
		app:         app,
		configIndex: make(map[string]analysis.Config),
	}
	for _, cfg := range app.Configs {
		ctx.configIndex[strings.ToLower(cfg.Name)] = cfg
	}
	ctx.nameTokens = tokeniseAppID(app.AppMetadata.ArtifactID)
	return ctx
}

// tokeniseAppID splits "sap-sapi" → ["sap", "sapi", "sap-sapi"].
// Short tokens (<3 chars) are excluded to avoid noise.
func tokeniseAppID(id string) []string {
	id = strings.ToLower(id)
	parts := regexp.MustCompile(`[-_]`).Split(id, -1)
	seen := make(map[string]bool)
	var tokens []string
	for _, p := range parts {
		if len(p) >= 3 && !seen[p] {
			seen[p] = true
			tokens = append(tokens, p)
		}
	}
	// also include the full id if it differs from parts
	if !seen[id] && len(id) >= 3 {
		tokens = append(tokens, id)
	}
	return tokens
}

// lookupConfig returns a config by its name (case-insensitive).
func (ctx *appContext) lookupConfig(name string) (analysis.Config, bool) {
	cfg, ok := ctx.configIndex[strings.ToLower(name)]
	return cfg, ok
}

// resolveConnAttr resolves a connection attribute value using the app's properties.
func (ctx *appContext) resolveConnAttr(val string) string {
	resolved, _ := resolveToken(val, ctx.app.Properties)
	return resolved
}

// --- matching helpers ---

var tokenRe = regexp.MustCompile(`\$\{([^}]+)\}`)

// extractTokenKeys returns all ${token} key names present in a string.
func extractTokenKeys(s string) []string {
	var keys []string
	for _, m := range tokenRe.FindAllStringSubmatch(s, -1) {
		keys = append(keys, m[1])
	}
	return keys
}

// resolveToken is a local shim so linker doesn't import properties.go internals.
func resolveToken(value string, props map[string]string) (string, bool) {
	if !strings.Contains(value, "${") {
		return value, true
	}
	allResolved := true
	result := tokenRe.ReplaceAllStringFunc(value, func(match string) string {
		key := match[2 : len(match)-1]
		if v, ok := props[key]; ok {
			return v
		}
		allResolved = false
		return match
	})
	return result, allResolved
}

// containsAsWord returns true if target appears in s as a whole word, where
// words are delimited by underscores, hyphens, dots, or string boundaries.
// This prevents "sap-sapi" from matching inside "nsap-sapi".
func containsAsWord(s, target string) bool {
	if target == "" {
		return false
	}
	isSep := func(b byte) bool {
		return b == '_' || b == '-' || b == '.' || b == '/'
	}
	offset := 0
	for {
		pos := strings.Index(s[offset:], target)
		if pos < 0 {
			return false
		}
		abs := offset + pos
		leftOK := abs == 0 || isSep(s[abs-1])
		end := abs + len(target)
		rightOK := end == len(s) || isSep(s[end])
		if leftOK && rightOK {
			return true
		}
		offset = abs + 1
	}
}

// configRefMatchScore returns the evidence weight and description if configRef
// contains any word-bounded token that identifies the target app.
// Returns weight 0 if no match.
func configRefMatchScore(configRef string, to *appContext) (int, string) {
	lower := strings.ToLower(configRef)
	appID := strings.ToLower(to.app.AppMetadata.ArtifactID)

	// Exact full app-id as a whole word (strongest)
	if containsAsWord(lower, appID) {
		return 30, fmt.Sprintf("config-ref %q contains exact app-id %q", configRef, to.app.AppMetadata.ArtifactID)
	}
	// Any significant name token as a whole word
	for _, tok := range to.nameTokens {
		if containsAsWord(lower, tok) {
			return 20, fmt.Sprintf("config-ref %q contains app token %q", configRef, tok)
		}
	}
	return 0, ""
}

// pathTokenMatchScore checks if any ${token} key in rawPath starts with the
// target app's identifier as a prefix segment (e.g. ${sapi.request.path}).
func pathTokenMatchScore(rawPath string, to *appContext) (int, string) {
	appID := strings.ToLower(to.app.AppMetadata.ArtifactID)
	for _, key := range extractTokenKeys(rawPath) {
		keyLower := strings.ToLower(key)
		// Full app-id prefix: ${sap-sapi.request.path}
		if strings.HasPrefix(keyLower, appID+".") || strings.HasPrefix(keyLower, appID+"-") {
			return 25, fmt.Sprintf("path token ${%s} prefix matches app-id %q", key, to.app.AppMetadata.ArtifactID)
		}
		// Name token prefix: ${sapi.request.path} where "sapi" is a token of the target app
		for _, tok := range to.nameTokens {
			if strings.HasPrefix(keyLower, tok+".") || strings.HasPrefix(keyLower, tok+"-") {
				return 20, fmt.Sprintf("path token ${%s} starts with app token %q", key, tok)
			}
		}
	}
	return 0, ""
}

// ramlBaseURIScore returns evidence weight if the RAML baseUri of the target
// app can be found as a path prefix of the outbound request path.
func ramlBaseURIScore(outRawPath string, to *appContext) (int, string) {
	if to.app.RAMLInfo == nil || to.app.RAMLInfo.BaseURI == "" {
		return 0, ""
	}
	base := to.app.RAMLInfo.BaseURI
	// Strip {version} placeholders so /api/sap/{version} → /api/sap
	base = regexp.MustCompile(`/\{[^}]+\}`).ReplaceAllString(base, "")
	base = strings.TrimRight(base, "/")
	if base == "" || base == "/" {
		return 0, ""
	}

	// Check resolved or raw path
	for _, path := range []string{outRawPath} {
		pathNorm := strings.Split(path, "?")[0]
		if strings.HasPrefix(pathNorm, base) {
			return 10, fmt.Sprintf("outbound path %q starts with RAML baseUri %q", path, base)
		}
	}
	return 0, ""
}

// --- HTTP matching ---

func matchHTTP(out, in analysis.Interface, from, to *appContext) []Evidence {
	var ev []Evidence

	outConfigRef := attrVal(out.Attributes, "config-ref")

	// Rule: exact resolved path match
	outResolved, outOK := resolveToken(out.RawValue, from.app.Properties)
	inResolved, inOK := resolveToken(in.RawValue, to.app.Properties)
	if outOK && inOK && outResolved != "" && inResolved != "" && outResolved == inResolved {
		ev = append(ev, Evidence{
			Rule:        "http.exact-path-match",
			Description: fmt.Sprintf("resolved outbound path %q matches inbound path %q", outResolved, inResolved),
			Weight:      45,
			FromValue:   outResolved,
			ToValue:     inResolved,
		})
	}

	// Rule: config-ref naming match (structural)
	if outConfigRef != "" {
		if w, desc := configRefMatchScore(outConfigRef, to); w > 0 {
			ev = append(ev, Evidence{
				Rule:        "http.config-ref-naming",
				Description: desc,
				Weight:      w,
				FromValue:   outConfigRef,
				ToValue:     to.app.AppMetadata.ArtifactID,
			})
		}
	}

	// Rule: path property token prefix matches target app
	if w, desc := pathTokenMatchScore(out.RawValue, to); w > 0 {
		ev = append(ev, Evidence{
			Rule:        "http.path-token-app-match",
			Description: desc,
			Weight:      w,
			FromValue:   out.RawValue,
			ToValue:     to.app.AppMetadata.ArtifactID,
		})
	}

	// Rule: RAML baseUri match (structural)
	if w, desc := ramlBaseURIScore(out.RawValue, to); w > 0 {
		ev = append(ev, Evidence{
			Rule:        "http.raml-baseuri",
			Description: desc,
			Weight:      w,
			FromValue:   out.RawValue,
			ToValue:     to.app.RAMLInfo.BaseURI,
		})
	}

	return ev
}

// --- Messaging matching (JMS / SQS / Kafka / AMQP / VM) ---

func matchMessaging(out, in analysis.Interface, from, to *appContext) []Evidence {
	var ev []Evidence

	// Rule: exact resolved destination match
	outResolved, outOK := resolveToken(out.RawValue, from.app.Properties)
	inResolved, inOK := resolveToken(in.RawValue, to.app.Properties)
	if outOK && inOK && outResolved != "" && inResolved != "" && outResolved == inResolved {
		ev = append(ev, Evidence{
			Rule:        out.Protocol + ".exact-destination-match",
			Description: fmt.Sprintf("resolved destination %q matches", outResolved),
			Weight:      45,
			FromValue:   outResolved,
			ToValue:     inResolved,
		})
	}

	// Rule: same unresolved ${token} (strong signal — same property key → same queue)
	if out.RawValue == in.RawValue && strings.Contains(out.RawValue, "${") {
		ev = append(ev, Evidence{
			Rule:        out.Protocol + ".same-token",
			Description: fmt.Sprintf("both apps reference queue token %q", out.RawValue),
			Weight:      35,
			FromValue:   out.RawValue,
			ToValue:     in.RawValue,
		})
	}

	// Rule: same config-ref (both apps use the same broker config)
	outCfgRef := attrVal(out.Attributes, "config-ref")
	inCfgRef := attrVal(in.Attributes, "config-ref")
	if outCfgRef != "" && inCfgRef != "" && strings.EqualFold(outCfgRef, inCfgRef) {
		ev = append(ev, Evidence{
			Rule:        out.Protocol + ".shared-config-ref",
			Description: fmt.Sprintf("both apps use broker config %q", outCfgRef),
			Weight:      15,
			FromValue:   outCfgRef,
			ToValue:     inCfgRef,
		})
	}

	return ev
}

// attrVal retrieves an attribute from the map using an exact key, then a
// case-insensitive contains fallback.
func attrVal(attrs map[string]string, key string) string {
	if attrs == nil {
		return ""
	}
	if v, ok := attrs[key]; ok {
		return v
	}
	lower := strings.ToLower(key)
	for k, v := range attrs {
		if strings.Contains(strings.ToLower(k), lower) {
			return v
		}
	}
	return ""
}

// --- edge building ---

// matchApps produces all edges from fromApp to toApp, grouped by protocol.
func matchApps(from, to *appContext) []Edge {
	// Accumulate evidence per protocol
	protocolEv := make(map[string][]Evidence)

	for _, out := range from.app.OutboundInterfaces {
		for _, in := range to.app.InboundInterfaces {
			if out.Protocol != in.Protocol {
				continue
			}
			var ev []Evidence
			switch out.Protocol {
			case "http":
				ev = matchHTTP(out, in, from, to)
			case "jms", "sqs", "kafka", "amqp", "vm":
				ev = matchMessaging(out, in, from, to)
			}
			if len(ev) > 0 {
				protocolEv[out.Protocol] = append(protocolEv[out.Protocol], ev...)
			}
		}
	}

	var edges []Edge
	for protocol, ev := range protocolEv {
		ev = deduplicateEvidence(ev)
		confidence := scoreEvidence(ev)
		edges = append(edges, Edge{
			FromApp:         from.app.AppMetadata.ArtifactID,
			ToApp:           to.app.AppMetadata.ArtifactID,
			Protocol:        protocol,
			Confidence:      confidence,
			ConfidenceLevel: confidenceLevel(confidence),
			Evidence:        ev,
		})
	}
	return edges
}

// --- public entry point ---

// BuildGraph runs deterministic dependency matching across all apps and returns
// the workspace dependency graph.  Edges below minConfidence are placed in
// IgnoredEdges rather than Edges.
func BuildGraph(apps []*analysis.AppAnalysis, workspaceRoot string, minConfidence int) *DependencyGraph {
	contexts := make([]*appContext, len(apps))
	for i, app := range apps {
		contexts[i] = newAppContext(app)
	}

	graph := &DependencyGraph{
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		WorkspaceRoot: workspaceRoot,
		MinConfidence: minConfidence,
	}

	for i, from := range contexts {
		for j, to := range contexts {
			if i == j {
				continue
			}
			for _, edge := range matchApps(from, to) {
				if edge.Confidence >= minConfidence {
					graph.Edges = append(graph.Edges, edge)
				} else if edge.Confidence > 0 {
					graph.IgnoredEdges = append(graph.IgnoredEdges, edge)
				}
			}
		}
	}

	if graph.Edges == nil {
		graph.Edges = []Edge{}
	}

	return graph
}
