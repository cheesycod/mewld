package main

import (
	"mewld/coreutils"
	"mewld/proc"
	"mewld/utils"
	"mewld/web"
	"os"
	"strconv"

	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
)

func main() {
	dirname, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}

	err = os.Chdir(dirname + "/mewbot")

	if err != nil {
		log.Fatal(err)
	}

	err = godotenv.Load("env/bot.env", "env/mongo.env", "env/postgres.env", "env/voting.env")

	if err != nil {
		log.Fatal(err)
	}

	log.Println("Env files loaded")

	shardCount := web.GetShardCount()

	log.Println("Recommended shard count:", shardCount.Shards)

	if os.Getenv("SHARD_COUNT") != "" {
		shardCount.Shards = coreutils.ParseUint64(os.Getenv("SHARD_COUNT"))
	}

	// Get cluster names from assets/data/names.txt
	clusterNames, err := utils.ReadLines("assets/data/names.txt")

	if err != nil {
		log.Fatal(err)
	}

	var perCluster uint64 = 10

	if os.Getenv("PER_CLUSTER") != "" {
		perCluster = coreutils.ParseUint64(os.Getenv("PER_CLUSTER"))
	}

	log.Println("Cluster names:", clusterNames)

	clusterMap := utils.GetClusterList(clusterNames, shardCount.Shards, perCluster)

	il := proc.InstanceList{Map: clusterMap, Instances: []*proc.Instance{}, ShardCount: shardCount.Shards}

	for _, cMap := range clusterMap {
		log.Info("Cluster ", cMap.Name, "("+strconv.Itoa(cMap.ID)+"): ", coreutils.ToPyListUInt64(cMap.Shards))
		il.Instances = append(il.Instances, &proc.Instance{
			SessionID: utils.RandomString(16),
			ClusterID: cMap.ID,
			Shards:    cMap.Shards,
		})
	}

	// Start the signal handler
	go il.OsSignalHandle()

	// We now start the first cluster, this cluster will then alert us over redis when to start cluster 2 (todo: timeout?)
	il.Start(il.Instances[0])
}
