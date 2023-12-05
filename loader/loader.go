package loader

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/cheesycod/mewld/config"
	"github.com/cheesycod/mewld/coreutils"
	"github.com/cheesycod/mewld/proc"
	"github.com/cheesycod/mewld/redis"
	"github.com/cheesycod/mewld/utils"
	"github.com/cheesycod/mewld/web"

	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
)

func Load(config *config.CoreConfig, loaderData *proc.LoaderData) (*proc.InstanceList, *redis.RedisHandler, error) {
	var err error
	if len(config.Env) > 0 {
		err = godotenv.Load(config.Env...)

		if err != nil {
			return nil, nil, fmt.Errorf("error loading env files: %w", err)
		}

		log.Println("Env files loaded")
	}

	if os.Getenv("MTOKEN") != "" {
		config.Token = os.Getenv("MTOKEN")
	}

	if config.MinimumSafeSessionsRemaining == nil {
		config.MinimumSafeSessionsRemaining = coreutils.Pointer[uint64](5)
	}

	mssr := *config.MinimumSafeSessionsRemaining

	gb, err := proc.GetGatewayBot(config)

	if err != nil {
		log.Fatal("Failed to get gateway bot", err)
	}

	if config.FixedShardCount > 0 {
		gb.Shards = config.FixedShardCount
	}

	log.Println("Using shard count:", gb.Shards)

	var perCluster uint64 = config.PerCluster

	log.Println("Cluster names:", config.Names)

	clusterMap := proc.GetClusterList(config.Names, gb.Shards, perCluster)

	dir, err := utils.ConfigGetDirectory(config)

	if err != nil {
		return nil, nil, fmt.Errorf("error getting directory: %w", err)
	}

	if loaderData == nil {
		loaderData = &proc.LoaderData{
			Start: proc.DefaultStart,
		}
	}

	il := &proc.InstanceList{
		Config:     config,
		LoaderData: loaderData,
		Dir:        dir,
		Map:        clusterMap,
		Instances:  []*proc.Instance{},
		ShardCount: gb.Shards,
		GatewayBot: gb,
	}

	il.Init()

	// Start the redis handler
	redish := redis.RedisHandler{
		Ctx:          il.Ctx,
		InstanceList: il,
	}

	go redish.Start(il)

	for _, cMap := range clusterMap {
		log.Info("Cluster ", cMap.Name, "("+strconv.Itoa(cMap.ID)+"): ", coreutils.ToPyListUInt64(cMap.Shards))
		il.Instances = append(il.Instances, &proc.Instance{
			SessionID: coreutils.RandomString(16),
			ClusterID: cMap.ID,
			Shards:    cMap.Shards,
		})
	}

	if !config.UseCustomWebUI {
		go web.StartWebserver(web.WebData{
			RedisHandler: &redish,
			InstanceList: il,
		})
	}

	if gb.SessionStartLimit.Remaining < mssr {
		log.Error("Sessions remaining is less than config.minimum_safe_sessions_remaining. Waiting for SessionStartLimit.ResetAfter seconds...")
		time.Sleep(time.Millisecond * time.Duration(gb.SessionStartLimit.ResetAfter))
	}

	// We now start the first cluster, this cluster will then alert us over redis when to start cluster 2 (todo: timeout?)
	go il.Start(il.Instances[0])

	return il, &redish, nil
}
