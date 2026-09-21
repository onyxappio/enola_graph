package swiftextractor

import (
	"crypto/sha256"
	"fmt"

	"path"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DeltaContext mirrors root-only XcodeGen includes and the exact shallow iOS
// marker probe. Directory markers are not represented in engine AllNames.
func (e *SwiftExtractor) DeltaContext(repoPath string) string {
	digest, _ := e.observedContext(repoPath)
	return digest
}

// ObservedContextPaths includes missing and case-folded XcodeGen include inputs.
// Name/directory changes still require reconciliation of the shallow iOS probe.
func (e *SwiftExtractor) ObservedContextPaths(repoPath string) []string {
	_, paths := e.observedContext(repoPath)
	return paths
}

func (e *SwiftExtractor) observedContext(repoPath string) (string, []string) {
	inputScope := e.inputScope
	var paths []string
	h := sha256.New()
	read := func(rel string) ([]byte, error) {
		if inputScope.Allowed(filepath.Join(repoPath, rel), false) {
			paths = append(paths, rel)
		}
		b, err := inputScope.ReadFile(filepath.Join(repoPath, rel))
		fmt.Fprintf(h, "%q:%d:%v\n", rel, len(b), err)
		h.Write(b)
		return b, err
	}
	var root xcodeFile
	rootBytes, rootErr := read(projectManifestName)
	if rootErr == nil && yaml.Unmarshal(rootBytes, &root) == nil {
		for _, inc := range root.Include {
			if !inc.Enable || inc.Path == "" {
				continue
			}
			rel := path.Join(".", filepath.ToSlash(inc.Path))
			b, readErr := read(rel)
			var f xcodeFile
			// The parser retries case-insensitive lookup on read OR parse failure.
			if readErr != nil || yaml.Unmarshal(b, &f) != nil {
				if resolved, ok := resolveCaseInsensitive(repoPath, rel, inputScope); ok {
					read(resolved)
				}
			}
		}
	}
	fmt.Fprintf(h, "ios:%t", detectiOSProject(repoPath, inputScope))
	return fmt.Sprintf("swift-v1:%x", h.Sum(nil)), paths
}
