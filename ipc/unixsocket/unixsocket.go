package unixsocket

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
)

type UnixSocketHandler struct {
	Ctx      context.Context `json:"-"`
	Filename string          `json:"-"`

	cancel  context.CancelFunc `json:"-"`
	msgChan chan []byte        `json:"-"`
	l       net.Listener       `json:"-"`
	conns   []net.Conn         `json:"-"`

	// Get/Store key
	VMap   Map[string, []byte]   `json:"-"`
	ArrMap Map[string, [][]byte] `json:"-"`
}

func NewWithUnixSocket(ctx context.Context, filename string) (*UnixSocketHandler, error) {
	// Delete stale socket file if it exists
	err := os.Remove(filename)

	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("error deleting stale socket file: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)

	return &UnixSocketHandler{
		Ctx:      ctx,
		Filename: filename,
		cancel:   cancel,
		msgChan:  make(chan []byte, 100),
	}, nil
}

func (r *UnixSocketHandler) Connect() error {
	// Create unix socket file
	var err error
	r.l, err = net.Listen("unix", r.Filename)

	if err != nil {
		return fmt.Errorf("error creating unix socket: %w", err)
	}

	go func() {
		for {
			select {
			case <-r.Ctx.Done():
				return
			default:
			}

			// Accept connection
			conn, err := r.l.Accept()

			if err != nil {
				return
			}

			// Add connection to the list of connections
			// so we can write to it later
			r.conns = append(r.conns, conn)

			// Handle the connection in a separate goroutine.
			go func(conn net.Conn) {
				defer conn.Close()

				// Keep reading data from the connection and sending it to the message channel
				for {
					err = conn.SetReadDeadline(time.Now().Add(60 * time.Second))

					if err != nil {
						log.Debugf("error setting read deadline: %v", err)
						continue
					}

					select {
					case <-r.Ctx.Done():
						return
					default:
					}

					buf := make([]byte, 65535)

					n, err := conn.Read(buf)

					if err != nil {
						log.Debugf("error reading from connection: %v", err)
						continue
					}

					r.msgChan <- buf[:n]

					// Also, dispatch it to the other connections
					for _, c := range r.conns {
						if c != conn {
							_, err = c.Write(buf[:n])

							if err != nil {
								log.Debugf("error writing to connection: %v", err)
								continue
							}
						}
					}
				}
			}(conn)
		}
	}()

	return nil
}

func (r *UnixSocketHandler) Disconnect() error {
	r.cancel()
	r.l.Close()

	return os.Remove(r.Filename)
}

func (r *UnixSocketHandler) Read() chan []byte {
	return r.msgChan
}

func (r *UnixSocketHandler) Write(data []byte) error {
	for _, conn := range r.conns {
		err := conn.SetWriteDeadline(time.Now().Add(3 * time.Second))

		if err != nil {
			return err
		}

		_, err = conn.Write(data)

		if err != nil {
			return err
		}
	}

	return nil
}

func (r *UnixSocketHandler) GetKey(key string) ([]byte, error) {
	if v, ok := r.VMap.Load(key); ok {
		return v, nil
	}

	return []byte{}, nil
}

func (r *UnixSocketHandler) StoreKey(key string, value []byte) error {
	r.VMap.Store(key, value)
	return nil
}

func (r *UnixSocketHandler) GetKey_Array(key string) ([][]byte, error) {
	if v, ok := r.ArrMap.Load(key); ok {
		return v, nil
	}

	return [][]byte{}, nil
}

func (r *UnixSocketHandler) StoreKey_Array(key string, value []byte) error {
	if v, ok := r.ArrMap.Load(key); ok {
		r.ArrMap.Store(key, append(v, value))
	} else {
		r.ArrMap.Store(key, [][]byte{value})
	}

	return nil
}
