package action

import (
	"io"
	"os"
)

// openReadOnly is split out so platform-specific permission handling can
// be added later (e.g. SELinux exempted file paths). The current
// implementation is the same on every platform.
func openReadOnly(path string) (io.ReadCloser, error) {
	return os.Open(path)
}
