package resources

import _ "embed"

//go:embed dictionary.json
var DictionaryJSON []byte

//go:embed md.tmpl
var MarkdownTemplate []byte

//go:embed workspace.tmpl
var WorkspaceTemplate []byte
