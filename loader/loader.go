package loader

import (
	"fmt"
	"os"
	"strconv"

	"github.com/cheesycod/mewld/config"
	"github.com/cheesycod/mewld/coreutils"
	"github.com/cheesycod/mewld/proc"
	"github.com/cheesycod/mewld/redis"
	"github.com/cheesycod/mewld/utils"
	"github.com/cheesycod/mewld/web"

	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
)

func Load(config *config.CoreConfig) (*proc.InstanceList, error) {
	var err error
	if len(config.Env) > 0 {
		err = godotenv.Load(config.Env...)

		if err != nil {
			return nil, fmt.Errorf("error loading env files: %w", err)
		}

		log.Println("Env files loaded")
	}

	shardCount := utils.GetShardCount()

	log.Println("Recommended shard count:", shardCount.Shards)

	if os.Getenv("SHARD_COUNT") != "" {
		shardCount.Shards = coreutils.ParseUint64(os.Getenv("SHARD_COUNT"))
	}

	var perCluster uint64 = config.PerCluster

	if os.Getenv("PER_CLUSTER") != "" {
		perCluster = coreutils.ParseUint64(os.Getenv("PER_CLUSTER"))
	}

	log.Println("Cluster names:", config.Names)

	clusterMap := utils.GetClusterList(config.Names, shardCount.Shards, perCluster)

	dir, err := utils.ConfigGetDirectory(config)

	if err != nil {
		return nil, fmt.Errorf("error getting directory: %w", err)
	}

	il := &proc.InstanceList{
		Config:     config,
		Dir:        dir,
		Map:        clusterMap,
		Instances:  []*proc.Instance{},
		ShardCount: shardCount.Shards,
	}

	il.Init()

	for _, cMap := range clusterMap {
		log.Info("Cluster ", cMap.Name, "("+strconv.Itoa(cMap.ID)+"): ", coreutils.ToPyListUInt64(cMap.Shards))
		il.Instances = append(il.Instances, &proc.Instance{
			SessionID: coreutils.RandomString(16),
			ClusterID: cMap.ID,
			Shards:    cMap.Shards,
		})
	}

	// Start the redis handler
	redish := redis.CreateHandler(config)
	go redish.Start(il)

	go web.StartWebserver(web.WebData{
		RedisHandler: &redish,
		InstanceList: il,
	})

	// We now start the first cluster, this cluster will then alert us over redis when to start cluster 2 (todo: timeout?)
	go il.Start(il.Instances[0])

	return il, nil
}
