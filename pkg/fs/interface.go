package fs

type FileSystem interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, content []byte) error
}
type FileSystemManager struct {
	fs FileSystem
}

func NewFileSystemManager(fs FileSystem) *FileSystemManager {
	return &FileSystemManager{fs: fs}
}

func (manager FileSystemManager) ReadFile(path string) ([]byte, error) {
	return manager.fs.ReadFile(path)
}

func (manager FileSystemManager) CreateFile(path string, content []byte) error {
	return manager.fs.WriteFile(path, content)
}
