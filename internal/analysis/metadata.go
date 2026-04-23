package analysis

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/beevik/etree"
	"mule-app-analyser/internal/discovery"
)

// loadAppMetadata extracts identity fields from pom.xml and mule-artifact.json.
// Returns the populated AppMetadata, the resolved muleVersion string, and any warnings.
func loadAppMetadata(app discovery.AppRoot) (AppMetadata, string, []string) {
	meta := AppMetadata{Folder: app.Dir}
	var muleVersion string
	var warnings []string

	// pom.xml → artifactId, groupId, version
	pomPath := filepath.Join(app.Dir, "pom.xml")
	if pomDoc, err := loadXMLFile(pomPath); err == nil {
		root := pomDoc.Root()
		if el := root.FindElement("artifactId"); el != nil {
			meta.ArtifactID = strings.TrimSpace(el.Text())
		}
		if el := root.FindElement("groupId"); el != nil {
			meta.GroupID = strings.TrimSpace(el.Text())
		}
		if el := root.FindElement("version"); el != nil {
			meta.Version = strings.TrimSpace(el.Text())
		}
	} else {
		warnings = append(warnings, "pom.xml not found or unreadable; using folder name as artifactId")
	}

	if meta.ArtifactID == "" {
		meta.ArtifactID = app.Name
	}

	// mule version from version-specific files
	switch app.MuleVersion {
	case 4:
		artifactPath := filepath.Join(app.Dir, "mule-artifact.json")
		if f, err := os.Open(artifactPath); err == nil {
			defer f.Close()
			byteValue, _ := io.ReadAll(f)
			var j map[string]interface{}
			if json.Unmarshal(byteValue, &j) == nil {
				if v, ok := j["minMuleVersion"].(string); ok {
					muleVersion = v
				}
			}
		}
		if muleVersion == "" {
			muleVersion = "4.x - exact version unknown; mule-artifact.json missing or incomplete"
		}
	case 3:
		xmlPath := filepath.Join(app.Dir, "mule-project.xml")
		if projectDoc, err := loadXMLFile(xmlPath); err == nil {
			muleVersion = strings.Replace(
				projectDoc.Root().SelectAttrValue("runtimeId", "3.x"),
				"org.mule.tooling.server.", "", -1)
			if nameEl := projectDoc.Root().FindElement("name"); nameEl != nil {
				nameText := strings.TrimSpace(nameEl.Text())
				if nameText != "" {
					meta.ArtifactID = nameText
				}
			}
		} else {
			muleVersion = "3.x - exact version unknown; mule-project.xml missing"
		}
	}

	meta.MuleVersion = muleVersion
	return meta, muleVersion, warnings
}

func loadXMLFile(path string) (etree.Document, error) {
	doc := etree.NewDocument()
	err := doc.ReadFromFile(path)
	return *doc, err
}
