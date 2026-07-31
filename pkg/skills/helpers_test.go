package skills

import (
	"os"

	"github.com/leviantech/go-agentic-sdk/pkg/tools"
)

func newTestRegistry() *tools.Registry {
	return tools.NewRegistry()
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
