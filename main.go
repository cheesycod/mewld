package main

import (
	"mewld/config"
	"mewld/coreutils"
	"mewld/proc"
	"mewld/redis"
	"mewld/utils"
	"mewld/web"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	_ "embed"

	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

//go:embed config.yaml
var configBytes []byte

func init() {
	lvl, ok := os.LookupEnv("LOG_LEVEL")
	// LOG_LEVEL not set, let's default to info
	if !ok {
		lvl = "info"
	}
	// parse string, this is built-in feature of logrus
	ll, err := log.ParseLevel(lvl)
	if err != nil {
		ll = log.InfoLevel
	}
	// set global log level
	log.SetLevel(ll)
}

func main() {
	// Load the config file
	var config config.CoreConfig

	err := yaml.Unmarshal(configBytes, &config)

	if err != nil {
		log.Fatal("Check config file again: ", err)
	}

	var dir string
	if config.OverrideDir != "" {
		dir = config.OverrideDir
	} else {
		dirname, err := os.UserHomeDir()
		if err != nil {
			log.Fatal("Could not find HOME directory: ", err)
		}

		dir = dirname + "/" + config.Dir
	}

	err = os.Chdir(dir)

	if err != nil {
		log.Fatal("Could not change into directory: ", err)
	}

	err = godotenv.Load(config.Env...)

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
	clusterNames, err := utils.ReadLines(config.Names)

	if err != nil {
		log.Fatal(err)
	}

	var perCluster uint64 = config.PerCluster

	if os.Getenv("PER_CLUSTER") != "" {
		perCluster = coreutils.ParseUint64(os.Getenv("PER_CLUSTER"))
	}

	log.Println("Cluster names:", clusterNames)

	clusterMap := utils.GetClusterList(clusterNames, shardCount.Shards, perCluster)

	il := proc.InstanceList{
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
	go redish.Start(&il)

	go web.StartWebserver(web.WebData{
		RedisHandler: &redish,
		InstanceList: &il,
	})

	// Wait here until we get a signal
	sigs := make(chan os.Signal, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	// We now start the first cluster, this cluster will then alert us over redis when to start cluster 2 (todo: timeout?)
	il.Start(il.Instances[0])

	sig := <-sigs

	log.Info("Received signal: ", sig)

	il.KillAll()

	// Exit
	os.Exit(0)
}
