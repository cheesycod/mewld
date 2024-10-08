package loader

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/cheesycod/mewld/config"
	"github.com/cheesycod/mewld/ipc"
	"github.com/cheesycod/mewld/ipchandler"
	"github.com/cheesycod/mewld/proc"
	"github.com/cheesycod/mewld/utils"
	"github.com/cheesycod/mewld/web"

	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
)

func Load(config *config.CoreConfig, loaderData *proc.LoaderData, ipc ipc.Ipc) (*proc.InstanceList, error) {
	var err error
	if len(config.Env) > 0 {
		err = godotenv.Load(config.Env...)

		if err != nil {
			return nil, fmt.Errorf("error loading env files: %w", err)
		}

		log.Println("Env files loaded")
	}

	if config.MinimumSafeSessionsRemaining == nil {
		config.MinimumSafeSessionsRemaining = utils.Pointer[uint64](5)
	}

	mssr := *config.MinimumSafeSessionsRemaining

	gb, err := proc.GetGatewayBot(config)

	if err != nil {
		return nil, fmt.Errorf("error getting gateway bot: %w", err)
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
		return nil, fmt.Errorf("error getting directory: %w", err)
	}

	if loaderData == nil {
		loaderData = &proc.LoaderData{
			Start: proc.DefaultStart,
		}
	}

	il := &proc.InstanceList{
		Ctx:        context.Background(),
		Config:     config,
		LoaderData: loaderData,
		Dir:        dir,
		Map:        clusterMap,
		Instances:  []*proc.Instance{},
		ShardCount: gb.Shards,
		GatewayBot: *gb,
		IPC:        ipc,
	}

	// Start the IPC handler
	ipch := ipchandler.IpcHandler{
		Ctx:          il.Ctx,
		InstanceList: il,
	}

	go ipch.Start(il)

	for _, cMap := range clusterMap {
		log.Info("Cluster ", cMap.Name, "("+strconv.Itoa(cMap.ID)+"): ", utils.ToPyListUInt64(cMap.Shards))
		il.Instances = append(il.Instances, &proc.Instance{
			SessionID: utils.RandomString(16),
			ClusterID: cMap.ID,
			Shards:    cMap.Shards,
		})
	}

	if !config.UseCustomWebUI {
		go func() {
			srv := web.StartWebserver(web.WebData{
				InstanceList: il,
			})

			err := srv.ListenAndServe()

			if err != nil {
				log.Error("Error starting webserver: ", err)
			}
		}()
	}

	if gb.SessionStartLimit.Remaining < mssr {
		log.Error("Sessions remaining is less than config.minimum_safe_sessions_remaining. Waiting for SessionStartLimit.ResetAfter seconds...")
		time.Sleep(time.Millisecond * time.Duration(gb.SessionStartLimit.ResetAfter))
	}

	// We now start the first cluster, this cluster will then alert us over redis when to start cluster 2 (todo: timeout?)
	go func() {
		err = il.Start(il.Instances[0])

		if err != nil {
			log.Error("Error starting cluster 1: ", err)
		}
	}()

	return il, nil
}
