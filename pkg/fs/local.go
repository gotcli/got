package fs

import "os"

/*
*
TL;DR: Working with local file.
*
*/
type LocalFS struct{}

func (fs LocalFS) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (fs LocalFS) WriteFile(path string, content []byte) error {
	return os.WriteFile(path, content, 0644)
}

func FileExist(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
