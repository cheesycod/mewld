// Redis manager for mewld
package redis

import (
	"context"
	"encoding/json"
	"mewld/config"
	"mewld/proc"
	"reflect"

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
	Scope     string         `json:"scope"`
	Action    string         `json:"action"`
	Args      map[string]any `json:"args,omitempty"`
	CommandId string         `json:"command_id,omitempty"`
	Output    string         `json:"output,omitempty"`
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

		switch cmd.Action {
		case "launch_next":
			if il.RollRestarting {
				// Get cluster id from args
				typeOfId := reflect.TypeOf(cmd.Args["id"])

				log.Info("Got launch_next command for cluster ", cmd.Args["id"], " (", typeOfId, ")")

				clusterId, ok := cmd.Args["id"].(float64)

				if !ok {
					log.Error("Could not get cluster id from args: ", cmd.Args["id"])
					continue
				}

				// Push to proc.RollRestartChannel
				proc.RollRestartChannel <- int(clusterId)
				continue
			}
			il.StartNext()
		case "rollingrestart":
			go func() {
				il.Acknowledge(cmd.CommandId)
				il.RollingRestart()
			}()
		}
	}
}
