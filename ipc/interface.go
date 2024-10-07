package ipc

// A simple abstraction for a mewld ipc connection
type Ipc interface {
	Connect() error
	Disconnect() error
	Read() chan []byte
	Write([]byte) error
	GetKey(key string) ([]byte, error)
	StoreKey(key string, value []byte) error
	GetKey_Array(key string) ([][]byte, error)
	StoreKey_Array(key string, value []byte) error
}
