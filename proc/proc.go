// Process manager for mewld
package proc

import (
	"context"
	"encoding/json"
	"mewld/config"
	"mewld/coreutils"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/go-redis/redis/v9"
	log "github.com/sirupsen/logrus"
)

var RollRestartChannel = make(chan int)

// Represents a "cluster" of instances.
type ClusterMap struct {
	ID     int
	Name   string
	Shards []uint64
}

// The final store of the ClusterMap list as well as a instance store
type InstanceList struct {
	LastClusterStartedAt time.Time
	Map                  []ClusterMap
	Instances            []*Instance
	ShardCount           uint64
	Config               config.CoreConfig
	Dir                  string
	Redis                *redis.Client   // Redis for publishing new messages, *not* subscribing
	Ctx                  context.Context // Context for redis
	startMutex           *sync.Mutex     // Internal mutex to prevent multiple instances from starting at the same time
	RollRestarting       bool            // whether or not we are roll restarting (rolling restart)
	FullyUp              bool            // whether or not we are fully up
}

type Instance struct {
	StartedAt    time.Time
	SessionID    string   // Internally used to identify the instance
	ClusterID    int      // ClusterID from clustermap
	Shards       []uint64 // Shards that this instance is responsible for currently, should be equal to clustermap
	Command      *exec.Cmd
	Active       bool // Whether or not this instance is active
	LockObserver bool // Whether or not observer should be 'locked'/not process a kill
}

// Very simple status fetcher for "statuses" command
func (i *Instance) Status() string {
	if i.Active {
		return "running"
	} else if i.SessionID != "" {
		return "initialized"
	}
	return "stopped"
}

func (l *InstanceList) Init() {
	l.startMutex = &sync.Mutex{}

	ctx := context.Background()

	rdb := redis.NewClient(&redis.Options{
		Addr:     l.Config.Redis,
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	status := rdb.Ping(ctx)

	if status.Err() != nil {
		log.Fatal("Redis error: ", status.Err())
	}

	l.Ctx = ctx
	l.Redis = rdb
}

// Acknowledge a published message
func (l *InstanceList) Acknowledge(cmdId string) error {
	return l.SendMessage(cmdId, "ok", "bot", "")
}

// Acknowledge a published message
func (l *InstanceList) SendMessage(cmdId string, payload any, scope string, action string) error {
	msg := map[string]any{
		"command_id": cmdId,
		"output":     payload,
		"scope":      scope,
		"action":     action,
	}

	bytes, err := json.Marshal(msg)

	if err != nil {
		return err
	}

	err = l.Redis.Publish(l.Ctx, l.Config.RedisChannel, bytes).Err()

	return err
}

// Should be called as a seperate goroutine
func (l *InstanceList) RollingRestart() {
	if !l.FullyUp {
		log.Error("Not fully up, not rolling restart")
		return
	}

	l.RollRestarting = true

	for _, i := range l.Instances {
		log.Info("Rolling restart on cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ")")

		code := l.Stop(i)

		if code == StopCodeRestartFailed {
			log.Error("Rolling restart failed on cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ")")
			continue
		}

		// Now start cluster again
		l.Start(i)

		for {
			id := <-RollRestartChannel

			if id != i.ClusterID {
				log.Info("Ignoring restart of cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, "). Waiting for cluster ", id, " to restart")
			} else {
				break
			}
		}
	}

	log.Info("Rolling restart finished")

	l.RollRestarting = false
}

func (l *InstanceList) StartNext() {
	l.FullyUp = false // We are starting a new instance, so we are not fully up yet
	// Get next instance to start
	for _, i := range l.Instances {
		if i.Command == nil || i.Command.Process == nil {
			log.Info("Going to start *next* cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ") after delay of 5 seconds due to concurrency")
			time.Sleep(time.Second * 5)
			l.Start(i)
			return
		}
	}

	log.Info("No more instances to start. All done!!!")
	l.SendMessage(coreutils.RandomString(16), "", "bot", "all_clusters_launched")
	l.FullyUp = true // If we get here, we are fully up
}

func (l *InstanceList) KillAll() {
	// Kill all instances
	for _, i := range l.Instances {
		if i.Command == nil {
			log.Error("Cluster " + l.Cluster(i).Name + " (" + strconv.Itoa(l.Cluster(i).ID) + ") is not running")
		} else {
			log.Info("Killing cluster " + l.Cluster(i).Name + " (" + strconv.Itoa(l.Cluster(i).ID) + ")")
			i.Command.Process.Kill()
			i.Active = false
			i.SessionID = ""
		}
	}

	// Wait for all instances to die
	for _, i := range l.Instances {
		if i.Command == nil {
			continue
		}
		i.Command.Wait()
	}
}

func (l *InstanceList) Cluster(i *Instance) *ClusterMap {
	for _, c := range l.Map {
		if c.ID == i.ClusterID {
			return &c
		}
	}
	return nil
}

type StopCode int

const (
	StopCodeNormal        StopCode = 0
	StopCodeRestartFailed StopCode = -1
)

func (l *InstanceList) Stop(i *Instance) StopCode {
	if i.Command == nil || i.Command.Process == nil {
		log.Error("Cluster " + l.Cluster(i).Name + " (" + strconv.Itoa(l.Cluster(i).ID) + ") is not running. Cannot stop process which isn't running?")
		i.SessionID = "" // Just in case, we set session ID to empty string, this kills observer
		return StopCodeRestartFailed
	}

	log.Info("Stopping cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ")")

	i.Command.Process.Kill()

	i.Active = false

	i.SessionID = ""

	log.Info("Cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ") stopped")

	return StopCodeNormal
}

func (l *InstanceList) Start(i *Instance) {
	// Mutex to prevent multiple instances from starting at the same time
	l.startMutex.Lock()
	defer l.startMutex.Unlock()

	i.StartedAt = time.Now()
	l.LastClusterStartedAt = time.Now()
	i.SessionID = coreutils.RandomString(32)

	dir, err := os.Getwd()

	log.Info("Starting cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ") in directory ", dir)

	if err != nil {
		log.Fatal(err)
	}

	cluster := l.Cluster(i)

	if cluster == nil {
		panic("Cluster not found")
	}

	// Log mode, depends on bot to handle it
	loggingCode := "0"

	// Get interpreter/caller
	var cmd *exec.Cmd
	if l.Config.Interp != "" {
		cmd = exec.Command(
			l.Config.Interp,
			l.Dir+"/"+l.Config.Module,
			coreutils.ToPyListUInt64(i.Shards),
			coreutils.UInt64ToString(l.ShardCount),
			strconv.Itoa(i.ClusterID),
			cluster.Name,
			loggingCode,
			dir,
		)
	} else {
		cmd = exec.Command(
			l.Config.Module, // If no interpreter, we use the full module as the executable path
			coreutils.ToPyListUInt64(i.Shards),
			coreutils.UInt64ToString(l.ShardCount),
			strconv.Itoa(i.ClusterID),
			cluster.Name,
			loggingCode,
			dir,
		)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	i.Command = cmd

	// Spawn process
	err = cmd.Start()

	if err != nil {
		log.Error("Cluster "+cluster.Name+"("+strconv.Itoa(cluster.ID)+") failed to start", err)
	}

	i.Active = true

	go l.Observe(i, i.SessionID)
}

func (l *InstanceList) Observe(i *Instance, sid string) {
	if err := i.Command.Wait(); err != nil {
		if i.SessionID == "" || sid != i.SessionID {
			return // Stop observer if instance is stopped
		}

		if i.LockObserver {
			log.Error("Cluster locked, cannot restart ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ")")
			return
		}

		if l.RollRestarting {
			log.Error("Roll restart is in progress, ignoring restart on cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ")")
			return
		}

		i.Active = false

		log.Error("Cluster "+l.Cluster(i).Name+" ("+strconv.Itoa(l.Cluster(i).ID)+") died unexpectedly: ", err)

		if exiterr, ok := err.(*exec.ExitError); ok {
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				log.Infof("Exit Status: %d", status.ExitStatus())
			}
		}

		// Restart process
		time.Sleep(time.Second * 3)
		l.Stop(i)
		time.Sleep(time.Second * 3)
		l.Start(i)
	}
}
