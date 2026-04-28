package lint

import "os"

// readBytes is the IO point. Tests can swap readFile in rules.go to inject
// fixtures without touching disk; the production path uses os.ReadFile.
func readBytes(path string) ([]byte, error) {
	return os.ReadFile(path)
}
