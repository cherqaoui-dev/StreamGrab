package platform

type Disk struct{}

func (Disk) FreeBytes(path string) (uint64, error) {
	return FreeBytes(path)
}
