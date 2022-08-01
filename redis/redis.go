// Redis manager for mewld
package redis

import (
	"context"
	"encoding/json"
	"mewld/config"
	"mewld/proc"

	log "github.com/sirupsen/logrus"

	"github.com/go-redis/redis/v9"
)

// A Handler for redis
type RedisHandler struct {
	Ctx   context.Context
	Redis *redis.Client
}

func CreateHandler(config config.CoreConfig) RedisHandler {
	ctx := context.Background()

	rdb := redis.NewClient(&redis.Options{
		Addr:        config.Redis,
		Password:    "", // no password set
		DB:          0,  // use default DB
		ReadTimeout: -1,
	})

	status := rdb.Ping(ctx)

	if status.Err() != nil {
		log.Fatal("Redis error: ", status.Err())
	}

	return RedisHandler{
		Ctx:   ctx,
		Redis: rdb,
	}
}

type LauncherCmd struct {
	Scope  string         `json:"scope"`
	Action string         `json:"action"`
	Args   map[string]any `json:"args,omitempty"`
}

func (r *RedisHandler) Start(il *proc.InstanceList) {
	// Start pubsub
	pubsub := r.Redis.Subscribe(r.Ctx, il.Config.RedisChannel)

	defer pubsub.Close()

	// Start listening for messages
	for msg := range pubsub.Channel() {
		log.Info("Got redis message: ", msg.Payload)

		var cmd LauncherCmd

		err := json.Unmarshal([]byte(msg.Payload), &cmd)

		if err != nil {
			log.Error("Could not unmarshal message: ", err, ": ", msg.Payload)
			continue
		}

		if cmd.Scope != "launcher" {
			continue
		}

		if cmd.Action == "launch_next" {
			il.StartNext()
		}
	}
}
