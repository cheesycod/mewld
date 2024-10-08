package redis

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

type RedisHandler struct {
	Ctx          context.Context `json:"-"`
	Redis        *redis.Client   `json:"-"`
	RedisChannel string          `json:"-"`

	msgChan chan []byte
}

func NewWithRedis(ctx context.Context, redisURL string, redisChannel string) (*RedisHandler, error) {
	if !strings.HasPrefix(redisURL, "redis://") {
		// Assume config URL with some sane defaults
		redisURL = "redis://" + redisURL + "/0"
	}

	opts, err := redis.ParseURL(redisURL)

	if err != nil {
		return nil, fmt.Errorf("redis url parse error: %w", err)
	}

	opts.ReadTimeout = -1

	rdb := redis.NewClient(opts)

	status := rdb.Ping(ctx)

	if status.Err() != nil {
		return nil, fmt.Errorf("redis error: %w", status.Err())
	}

	return &RedisHandler{
		Ctx:          ctx,
		Redis:        rdb,
		RedisChannel: redisChannel,
		msgChan:      make(chan []byte, 100),
	}, nil
}

func (r *RedisHandler) Connect() error {
	if r.msgChan != nil {
		close(r.msgChan)
		r.msgChan = make(chan []byte, 100)
	}

	// Start pubsub
	pubsub := r.Redis.Subscribe(r.Ctx, r.RedisChannel)

	defer pubsub.Close()

	go func() {
		// Start listening for messages
		for {
			select {
			case <-r.Ctx.Done():
				return
			case msg := <-pubsub.Channel():
				if msg == nil {
					fmt.Println("nil message")
					continue
				}
				r.msgChan <- []byte(msg.Payload)
			}
		}
	}()

	return nil
}

func (r *RedisHandler) Disconnect() error {
	close(r.msgChan)
	return nil
}

func (r *RedisHandler) Read() chan []byte {
	return r.msgChan
}

func (r *RedisHandler) Write(data []byte) error {
	return r.Redis.Publish(r.Ctx, r.RedisChannel, data).Err()
}

func (r *RedisHandler) GetKey(key string) ([]byte, error) {
	dataStr, err := r.Redis.Get(r.Ctx, r.RedisChannel+"/"+key).Result()

	if err != nil {
		return nil, err
	}

	return []byte(dataStr), nil
}

func (r *RedisHandler) StoreKey(key string, value []byte) error {
	return r.Redis.Set(r.Ctx, r.RedisChannel+"/"+key, value, 0).Err()
}

func (r *RedisHandler) GetKey_Array(key string) ([][]byte, error) {
	dataStr, err := r.Redis.LRange(r.Ctx, r.RedisChannel+"/"+key, 0, -1).Result()

	if err != nil {
		return nil, err
	}

	data := make([][]byte, len(dataStr))

	for i, v := range dataStr {
		data[i] = []byte(v)
	}

	return data, nil
}

func (r *RedisHandler) StoreKey_Array(key string, value []byte) error {
	return r.Redis.RPush(r.Ctx, r.RedisChannel+"/"+key, value).Err()
}
