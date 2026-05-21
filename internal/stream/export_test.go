package stream

import "github.com/RXWatcher/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"

// FindFileForTesting exposes findFile for the hardening test suite.
func FindFileForTesting(files []bookwarehouse.File, idx int) (bookwarehouse.File, bool) {
	return findFile(files, idx)
}
