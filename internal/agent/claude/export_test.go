package claude

import "io/fs"

// SetStat swaps the stat used by the helper ownership check and returns a
// function that restores it.
func SetStat(f func(string) (fs.FileInfo, error)) (restore func()) {
	old := statPath
	statPath = f
	return func() { statPath = old }
}
