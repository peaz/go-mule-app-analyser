package analysis

import "strings"

type ifacePattern struct {
	tag       string // substring to match in the component tag (lowercase)
	direction string // "inbound" or "outbound"
	protocol  string // e.g. "http", "jms", "vm", "file", "sftp", "sqs", "kafka", "amqp"
	keyAttr   string // primary identifying attribute name
}

// ifacePatterns maps component tag substrings to their interface roles.
// Order matters: more specific matches should appear before broader ones.
var ifacePatterns = []ifacePattern{
	// HTTP
	{"http:listener", "inbound", "http", "path"},
	{"http:request", "outbound", "http", "path"},
	// JMS
	{"jms:listener", "inbound", "jms", "destination"},
	{"jms:publish", "outbound", "jms", "destination"},
	// VM
	{"vm:listener", "inbound", "vm", "queueName"},
	{"vm:publish", "outbound", "vm", "queueName"},
	// File
	{"file:listener", "inbound", "file", "directory"},
	{"file:write", "outbound", "file", "path"},
	// SFTP
	{"sftp:listener", "inbound", "sftp", "directory"},
	{"sftp:write", "outbound", "sftp", "path"},
	// SQS
	{"sqs:receive-messages", "inbound", "sqs", "queueUrl"},
	{"sqs:send-message", "outbound", "sqs", "queueUrl"},
	// Kafka
	{"kafka:message-listener", "inbound", "kafka", "topics"},
	{"kafka:publish", "outbound", "kafka", "topic"},
	// AMQP
	{"amqp:listener", "inbound", "amqp", "queueName"},
	{"amqp:publish", "outbound", "amqp", "routingKey"},
}

// extractInterfaces scans the flow manifest and returns inbound and outbound
// interfaces with property tokens resolved where possible.
// It also returns the list of unique token keys that could not be resolved.
func (a *analyser) extractInterfaces() (inbound []Interface, outbound []Interface, unresolved []string) {
	seenKey := make(map[string]bool)
	unresolvedSet := make(map[string]bool)

	for _, f := range a.flowManifest {
		for _, fc := range f.FlowComponents {
			// Normalise the component label stored during analysis
			tag := strings.ToLower(fc.Component)
			if strings.HasPrefix(tag, "trigger: ") {
				tag = tag[9:]
			}
			// strip "flow-ref -> [name]" style suffixes
			if idx := strings.Index(tag, " ->"); idx > 0 {
				tag = tag[:idx]
			}

			for _, pat := range ifacePatterns {
				if !strings.Contains(tag, pat.tag) {
					continue
				}

				rawVal := attrValue(fc.Attributes, pat.keyAttr)

				// deduplicate by direction+protocol+rawValue+flow
				dedupeKey := pat.direction + "|" + pat.protocol + "|" + rawVal + "|" + f.Name
				if seenKey[dedupeKey] {
					break
				}
				seenKey[dedupeKey] = true

				resolved, isResolved := resolveToken(rawVal, a.properties)
				if !isResolved {
					for _, tok := range unresolvedTokensIn(rawVal, a.properties) {
						unresolvedSet[tok] = true
					}
				}

				iface := Interface{
					Protocol:   pat.protocol,
					Direction:  pat.direction,
					FlowName:   f.Name,
					RawValue:   rawVal,
					Resolved:   resolved,
					IsResolved: isResolved,
					Source:     f.SourceXML,
					Attributes: fc.Attributes,
				}

				if pat.direction == "inbound" {
					inbound = append(inbound, iface)
				} else {
					outbound = append(outbound, iface)
				}
				break
			}
		}
	}

	for tok := range unresolvedSet {
		unresolved = append(unresolved, tok)
	}
	return inbound, outbound, unresolved
}

// attrValue returns the value for keyAttr from the attributes map,
// falling back to a case-insensitive contains search on the key names.
func attrValue(attrs map[string]string, keyAttr string) string {
	if attrs == nil {
		return ""
	}
	if v, ok := attrs[keyAttr]; ok {
		return v
	}
	lower := strings.ToLower(keyAttr)
	for k, v := range attrs {
		if strings.Contains(strings.ToLower(k), lower) {
			return v
		}
	}
	return ""
}
