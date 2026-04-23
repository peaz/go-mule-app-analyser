package discovery

import (
	"os"
	"path/filepath"
)

const (
	mule3XmlPath    = "src/main/app"
	mule4XmlPath    = "src/main/mule"
	mule3MarkerFile = "mule-project.xml"
	mule4MarkerFile = "mule-artifact.json"
)

// AppRoot represents a discovered Mule application root directory.
type AppRoot struct {
	Dir         string // absolute path to the app root directory
	Name        string // folder name
	MuleVersion int    // 3 or 4
	XMLPath     string // absolute path to the Mule XML source directory
}

// DiscoverApps walks root recursively and returns all Mule app directories.
// Once a directory is identified as a Mule app, it is not descended into further.
func DiscoverApps(root string) ([]AppRoot, error) {
	root = filepath.ToSlash(root)
	var apps []AppRoot
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// skip unreadable entries rather than aborting the whole walk
			return nil
		}
		if !info.IsDir() {
			return nil
		}
		if app, ok := DetectApp(path); ok {
			apps = append(apps, app)
			return filepath.SkipDir
		}
		return nil
	})
	return apps, err
}

// DetectApp checks whether dir is a Mule app root and returns its AppRoot descriptor.
func DetectApp(dir string) (AppRoot, bool) {
	dir = filepath.ToSlash(dir)
	// Mule 4: mule-artifact.json + src/main/mule/
	if fileExists(filepath.Join(dir, mule4MarkerFile)) && dirExists(filepath.Join(dir, mule4XmlPath)) {
		return AppRoot{
			Dir:         dir,
			Name:        filepath.Base(dir),
			MuleVersion: 4,
			XMLPath:     filepath.Join(dir, mule4XmlPath),
		}, true
	}
	// Mule 3: mule-project.xml + src/main/app/
	if fileExists(filepath.Join(dir, mule3MarkerFile)) && dirExists(filepath.Join(dir, mule3XmlPath)) {
		return AppRoot{
			Dir:         dir,
			Name:        filepath.Base(dir),
			MuleVersion: 3,
			XMLPath:     filepath.Join(dir, mule3XmlPath),
		}, true
	}
	return AppRoot{}, false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
