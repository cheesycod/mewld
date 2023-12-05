// Process manager for mewld
package proc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cheesycod/mewld/config"
	"github.com/cheesycod/mewld/utils"

	"github.com/redis/go-redis/v9"
	log "github.com/sirupsen/logrus"
)

var (
	RollRestartChannel = make(chan int)
	DiagChannel        = make(chan DiagResponse)
	PingCheckStop      = make(chan int) // Channel to stop the ping checker
	ErrTimeout         = errors.New("timeoutError")
	ErrLockedInstance  = errors.New("lockedInstanceError")
)

// Internal loader data, to make mewld embeddable and more extendible
type LoaderData struct {
	Start     func(l *InstanceList, i *Instance, cm *ClusterMap) error                                         // Start function
	OnReshard func(l *InstanceList, i *Instance, cm *ClusterMap, oldShards []uint64, newShards []uint64) error // OnReshard function is called when the bot is resharded by mewld
}

// Gets gateway information from discord
func GetGatewayBot(c *config.CoreConfig) (*GatewayBot, error) {
	url := "https://discord.com/api/gateway/bot"

	req, err := http.NewRequest("GET", url, nil)

	req.Header.Add("Authorization", "Bot "+c.Token)
	req.Header.Add("User-Agent", "MewBot/1.0")
	req.Header.Add("Content-Type", "application/json")

	if err != nil {
		return nil, err
	}

	client := http.Client{Timeout: 10 * time.Second}

	res, err := client.Do(req)

	if err != nil {
		return nil, err
	}

	defer res.Body.Close()

	log.Println("Shard count status:", res.Status)

	if res.StatusCode != 200 {
		log.Fatal("Shard count status code not 200. Invalid token?")
	}

	var gb GatewayBot

	bodyBytes, err := io.ReadAll(res.Body)

	if err != nil {
		log.Fatal(err)
	}

	err = json.Unmarshal(bodyBytes, &gb)

	if err != nil {
		return nil, err
	}

	return &gb, nil
}

// Given a shard count, return the shards for each cluster (128 would be [[0, 1, ..., 9], [10, 11, ..., 19]])
// However, if the shard count is not a multiple of the number of clusters, the last cluster will have fewer shards etc.
// So, 1 would mean [[0]]
func GetClusterList(clusterNames []string, shards uint64, perCluster uint64) []ClusterMap {
	var clusterMap []ClusterMap

	var shardArr []uint64
	var cid int = -1 // We start at -1 because we increment it before we use it
	for i := uint64(0); i < shards; i++ {
		if uint64(len(shardArr)) >= perCluster {
			if cid >= len(clusterNames)-3 {
				// Create a new cluster name using random string
				clusterNames = append(clusterNames, utils.RandomString(10))
			}
			cid++
			clusterMap = append(clusterMap, ClusterMap{ID: cid, Name: clusterNames[cid], Shards: shardArr})
			shardArr = []uint64{}
		}

		shardArr = append(shardArr, i)
	}

	if len(shardArr) > 0 {
		cid++
		clusterMap = append(clusterMap, ClusterMap{ID: cid, Name: clusterNames[cid], Shards: shardArr})
	}

	return clusterMap
}

// Represents a "cluster" of instances.
type ClusterMap struct {
	ID     int      // The clusters ID
	Name   string   // The friendly name of the cluster
	Shards []uint64 // The shard numbers/IDs of the cluster
}

type SessionStartLimit struct {
	Total          uint64 `json:"total"`           // Total number of session starts the current user is allowed
	Remaining      uint64 `json:"remaining"`       // Remaining number of session starts the current user is allowed
	ResetAfter     uint64 `json:"reset_after"`     // Number of milliseconds after which the limit resets
	MaxConcurrency uint64 `json:"max_concurrency"` // Number of identify requests allowed per 5 seconds
}

// Represents a response from the 'Get Gateway Bot' API
type GatewayBot struct {
	Url               string            `json:"url"`
	Shards            uint64            `json:"shards"`
	SessionStartLimit SessionStartLimit `json:"session_start_limit"`
}

// The final store of the ClusterMap list as well as a instance store
type InstanceList struct {
	LastClusterStartedAt time.Time          `json:"LastClusterStartedAt"`
	Map                  []ClusterMap       `json:"Map"`            // The list of clusters (ClusterMap) which defines how mewld will start clusters
	Instances            []*Instance        `json:"Instances"`      // The list of instances (Instance) which are running
	ShardCount           uint64             `json:"ShardCount"`     // The number of shards in ``mewld``
	GatewayBot           *GatewayBot        `json:"GetGatewayBot"`  // The response from Get Gateway Bot
	Config               *config.CoreConfig `json:"-"`              // The configuration for ``mewld`` ANTIRAID-SPECIFIC: Don't marshal this into JSON
	LoaderData           *LoaderData        `json:"-"`              // Internal loader data, to make mewld embeddable
	Dir                  string             `json:"Dir"`            // The base directory instances will use when loading clusters
	Redis                *redis.Client      `json:"-"`              // Core redis instance
	Ctx                  context.Context    `json:"-"`              // Context for redis
	StartMutex           *sync.Mutex        `json:"-"`              // Internal mutex to prevent multiple instances from starting at the same time
	RollRestarting       bool               `json:"RollRestarting"` // whether or not we are roll restarting (rolling restart)
	FullyUp              bool               `json:"FullyUp"`        // whether or not we are fully up
}

// Represents a instance of a cluster
type Instance struct {
	StartedAt        time.Time     `json:"StartedAt"`        // The time the instance was last started
	SessionID        string        `json:"SessionID"`        // Internally used to identify the instance
	ClusterID        int           `json:"ClusterID"`        // ClusterID from clustermap
	Shards           []uint64      `json:"Shards"`           // Shards that this instance is responsible for currently, should be equal to clustermap
	Command          *exec.Cmd     `json:"-"`                // Command that is running on the instance
	Active           bool          `json:"Active"`           // Whether or not this instance is active
	ClusterHealth    []ShardHealth `json:"ClusterHealth"`    // Cache of shard health from a ping
	CurrentlyKilling bool          `json:"CurrentlyKilling"` // Whether or not we are currently killing this instance
	LockClusterTime  *time.Time    `json:"LockClusterTime"`  // Time at which we last locked the cluster
	LaunchedFully    bool          `json:"LaunchedFully"`    // Whether or not we have launched the instance fully (till launch_next)
	LastChecked      time.Time     `json:"LastChecked"`      // The last time the shard was checked for health.
}

type ShardHealth struct {
	ShardID uint64  `json:"shard_id"` // The shard ID
	Up      bool    `json:"up"`       // Whether or not the shard is up
	Latency float64 `json:"latency"`  // Latency of the shard (optional, send if possible)
	Guilds  uint64  `json:"guilds"`   // The number of guilds in the shard. Is optional
	Users   uint64  `json:"users"`    // The number of users in the shard. Is optional
}

type DiagResponse struct {
	Nonce string        // Random nonce used to validate that a nonce comes from a specific diag request
	Data  []ShardHealth // The shard health data
}

// Returns true if the cluster is locked, otherwise false
func (i *Instance) Locked() bool {
	if i.LockClusterTime == nil || time.Since(*i.LockClusterTime) < time.Second*60 {
		return false
	}

	return true
}

// Attempts to lock the cluster from observing actions (such as shutdown/startup/rolling restart etc.)
func (i *Instance) Lock(l *InstanceList, subsystem string, critical bool) error {
	if i.Locked() && !critical {
		log.Error("Instance is already locked")
		go l.ActionLog(map[string]any{
			"event":     "instance_locked_error",
			"subsystem": subsystem,
		})
		return ErrLockedInstance
	}
	lt := time.Now()

	i.LockClusterTime = &lt

	return nil
}

func (i *Instance) Unlock() {
	i.LockClusterTime = nil
}

// Waits for the instance lock to end (if any), then returns thus acquiring the lock
func (i *Instance) AcquireLock() {
	for {
		if i.Locked() {
			time.Sleep(time.Millisecond * 100)
			continue
		}
		break
	}
}

// Acquires the lock and then locks it
func (i *Instance) AcquireLockAndLock(l *InstanceList, subsystem string) {
	i.AcquireLock()
	i.Lock(l, subsystem, false)
}

// Internal payload for diagnostics
type diagPayload struct {
	ClusterID int    `json:"id"`    // The cluster ID
	Nonce     string `json:"nonce"` // Random nonce sent that is used to validate that a nonce comes from a specific diag request
	Diag      bool   `json:"diag"`  // Whether or not this is a diag request, is always true in this struct
}

// Scans all shards of a instance using a diag request to get the shard health
func (l *InstanceList) ScanShards(i *Instance) ([]ShardHealth, error) {
	var nonce = utils.RandomString(10)

	var diagPayload = diagPayload{
		ClusterID: i.ClusterID,
		Nonce:     nonce,
		Diag:      true,
	}

	diagBytes, err := json.Marshal(diagPayload)

	if err != nil {
		return nil, err
	}

	err = l.Redis.Publish(l.Ctx, l.Config.RedisChannel, diagBytes).Err()

	if err != nil {
		return nil, err
	}

	// Wait for diagnostic message from channel with timeout
	if l.Config.PingTimeout == nil {
		l.Config.PingTimeout = utils.Pointer(120)
	}

	pt := *l.Config.PingTimeout

	ticker := time.NewTicker(time.Second * time.Duration(pt))

	for {
		select {
		case <-ticker.C:
			ticker.Stop()
			return nil, ErrTimeout
		case diag := <-DiagChannel:
			if diag.Nonce == nonce {
				ticker.Stop()
				i.LastChecked = time.Now()
				return diag.Data, nil
			}
		}
	}
}

// Reshards an instance list
//
// EXPERIMENTAL: Set 'reshard' in experimental_features to enable this
func (l *InstanceList) Reshard() error {
	if !utils.SliceContains(l.Config.ExperimentalFeatures, "reshard") {
		return errors.New("reshard not enabled")
	}

	if l.RollRestarting {
		return fmt.Errorf("cannot reshard during a rolling restart")
	}

	if !l.FullyUp {
		return fmt.Errorf("cannot safely reshard when not fully up")
	}

	// Lock all instances
	for i := range l.Instances {
		if l.Instances[i].Locked() {
			return fmt.Errorf("cluster %d is locked", i)
		}

		l.Instances[i].AcquireLockAndLock(l, "Reshard")
		defer l.Instances[i].Unlock()
	}

	l.FullyUp = false

	log.Println("Cluster names:", l.Config.Names)

	// Get new shard count
	gb, err := GetGatewayBot(l.Config)

	if err != nil {
		return fmt.Errorf("get gateway bot failed: %w", err)
	}

	if l.Config.MinimumSafeSessionsRemaining == nil {
		l.Config.MinimumSafeSessionsRemaining = utils.Pointer[uint64](5)
	}

	mssr := *l.Config.MinimumSafeSessionsRemaining

	if gb.SessionStartLimit.Remaining < mssr {
		return fmt.Errorf("sessions remaining is less than config.minimum_safe_sessions_remaining")
	}

	log.Println("Recommended shard count:", gb.Shards)

	if l.Config.FixedShardCount > 0 {
		gb.Shards = l.Config.FixedShardCount
	}

	// Next set the cluster map correctly and roll restart clusters
	var perCluster uint64 = l.Config.PerCluster

	clusterMap := GetClusterList(l.Config.Names, gb.Shards, perCluster)

	if len(clusterMap) < len(l.Instances) {
		return fmt.Errorf("cannot safely reshard to a smaller cluster size")
	}

	l.GatewayBot = gb
	l.Map = clusterMap
	l.ShardCount = gb.Shards

	errorList := []error{}

	for i, cMap := range clusterMap {
		if i < len(l.Instances) {
			l.Instances[i].ClusterID = cMap.ID // Always update Cluster ID. It doesn't hurt

			if !l.Config.ReshardAll {
				if utils.SlicesEqual(l.Instances[i].Shards, cMap.Shards) {
					log.Info("Cluster ", cMap.Name, "("+strconv.Itoa(cMap.ID)+") UNCHANGED (same shards): ", utils.ToPyListUInt64(cMap.Shards))
					continue // No need to reshard
				}
			}

			// We already have this cluster already, merely grow it, then restart the cluster
			log.Info("Cluster ", cMap.Name, "("+strconv.Itoa(cMap.ID)+") EXPANDED/RESHARDED: ", utils.ToPyListUInt64(cMap.Shards))
			l.Instances[i].Shards = cMap.Shards

			err := l.Stop(l.Instances[i])

			if err == StopCodeNormal {
				l.Start(l.Instances[i])
			} else {
				errorList = append(errorList, fmt.Errorf("cluster %d stop failure with exit code %d", i, err))
			}
		} else {
			// We don't already have this cluster yet, add it
			log.Info("Cluster ", cMap.Name, "("+strconv.Itoa(cMap.ID)+") CREATED: ", utils.ToPyListUInt64(cMap.Shards))
			l.Instances = append(l.Instances, &Instance{
				SessionID: utils.RandomString(16),
				ClusterID: cMap.ID,
				Shards:    cMap.Shards,
			})

			l.Start(l.Instances[i])
		}
	}

	if len(errorList) > 0 {
		var err string

		for _, el := range errorList {
			if el != nil {
				err += el.Error()
			}
		}

		if err != "" {
			return errors.New(err)
		}
	}

	return nil
}

// Creates a new action log for a cluster
func (l *InstanceList) ActionLog(payload map[string]any) {
	payload["ts"] = time.Now().UnixMicro()

	log.Info("Posting action log: ", payload)

	pBytes, err := json.Marshal(payload)

	if err != nil {
		log.Error("Error marshalling action log", err)
	}

	if l.Redis.RPush(l.Ctx, l.Config.RedisChannel+"/actlogs", pBytes).Err() != nil {
		log.Error("Error adding action log", err)
	}
}

// Initializes the instance list and sets needed fields, must be called
//
// This function may update the config URL to include sane defaults
func (l *InstanceList) Init() {
	l.StartMutex = &sync.Mutex{}
	l.GatewayBot = &GatewayBot{} // Ensure this is not nil

	ctx := context.Background()

	if !strings.HasPrefix(l.Config.Redis, "redis://") {
		// Assume config URL with some sane defaults
		l.Config.Redis = "redis://" + l.Config.Redis + "/0"
	}

	opts, err := redis.ParseURL(l.Config.Redis)

	if err != nil {
		log.Fatal("Redis url parse error: ", err)
	}

	opts.ReadTimeout = -1

	rdb := redis.NewClient(opts)

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

// Sends a message to redis
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

// Begins a rolling restart, should be called as a seperate goroutine
func (l *InstanceList) RollingRestart() {
	if !l.FullyUp {
		log.Error("Not fully up, not rolling restart")
		return
	}

	go l.ActionLog(map[string]any{
		"event": "rolling_restart",
	})

	l.RollRestarting = true

	for _, i := range l.Instances {
		log.Info("Rolling restart on cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ")")

		i.AcquireLock()

		i.Lock(l, "RollingRestart", false)

		code := l.Stop(i)

		if code == StopCodeRestartFailed {
			log.Error("Rolling restart failed on cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ")")
			continue
		}

		// Now start cluster again
		l.Start(i)

		i.Unlock()

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

// Starts the next cluster in the instance list if possible
func (l *InstanceList) StartNext() {
	// We are starting a new instance, so we are not fully up yet
	l.FullyUp = false

	// Get next instance to start
	for _, i := range l.Instances {
		if i.Command == nil || i.Command.Process == nil {
			if l.Config.ClusterStartNextDelay == nil {
				l.Config.ClusterStartNextDelay = utils.Pointer(5)
			}

			snd := *l.Config.ClusterStartNextDelay
			log.Info("Going to start *next* cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ") after delay of "+strconv.Itoa(snd)+" seconds due to concurrency")
			time.Sleep(time.Second * time.Duration(snd))
			l.Start(i)
			i.Unlock() // Unlock cluster after starting
			return
		}
	}

	log.Info("No more instances to start. All done!!!")
	l.SendMessage(utils.RandomString(16), "", "bot", "all_clusters_launched")
	l.FullyUp = true // If we get here, we are fully up
}

// Kills all clusters in the instance list
func (l *InstanceList) KillAll() {
	// Kill all instances
	for _, i := range l.Instances {
		if i.Command == nil || i.Command.Process == nil {
			log.Error("Cluster " + l.Cluster(i).Name + " (" + strconv.Itoa(l.Cluster(i).ID) + ") is not running")
			i.Active = false
		} else {
			log.Info("Killing cluster " + l.Cluster(i).Name + " (" + strconv.Itoa(l.Cluster(i).ID) + ")")

			i.AcquireLockAndLock(l, "KillAll")

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

		i.Unlock()
	}
}

// Returns the ClusterMap for a specific instance
func (l *InstanceList) Cluster(i *Instance) *ClusterMap {
	for _, c := range l.Map {
		if c.ID == i.ClusterID {
			return &c
		}
	}
	return nil
}

// Returns a Instance given its cluster ID
func (l *InstanceList) InstanceByID(id int) *Instance {
	for _, c := range l.Instances {
		if c.ClusterID == id {
			return c
		}
	}
	return nil
}

type StopCode int

const (
	StopCodeNormal        StopCode = 0
	StopCodeRestartFailed StopCode = -1
)

// Attempts to stop a instance returning a status code defining whether the cluster could be stopped or not
func (l *InstanceList) Stop(i *Instance) StopCode {
	if i.Command == nil || i.Command.Process == nil {
		log.Error("Cluster " + l.Cluster(i).Name + " (" + strconv.Itoa(l.Cluster(i).ID) + ") is not running. Cannot stop process which isn't running?")
		i.SessionID = "" // Just in case, we set session ID to empty string, this kills observer
		return StopCodeRestartFailed
	}

	log.Info("Stopping cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ")")

	i.Lock(l, "Stop", false)

	i.Command.Process.Kill()

	i.Active = false

	i.SessionID = ""

	i.Unlock()

	log.Info("Cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ") stopped")

	return StopCodeNormal
}

// Starts a instance in the instance list, this locks the cluster if not already locked before unlocking after startup
func (l *InstanceList) Start(i *Instance) {
	// Mutex to prevent multiple instances from starting at the same time
	l.StartMutex.Lock()
	defer l.StartMutex.Unlock()

	if !i.Locked() {
		i.Lock(l, "Start", false)
	}

	i.StartedAt = time.Now()
	l.LastClusterStartedAt = time.Now()
	i.SessionID = utils.RandomString(32)
	i.LastChecked = time.Now()

	log.Info("Starting cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ") in directory ", l.Dir)

	cluster := l.Cluster(i)

	if cluster == nil {
		log.Fatal("Cluster not found")
	}

	err := l.LoaderData.Start(l, i, cluster)

	i.Unlock()

	if err != nil {
		log.Error("Cluster "+cluster.Name+"("+strconv.Itoa(cluster.ID)+") failed to start", err)
	}

	i.Active = true

	go l.Observe(i, i.SessionID)

	go l.PingCheck(i, i.SessionID)
}

// Pings a cluster every “ping_interval“ to check for responsiveness, restarts dead clusters if not responding to “diag“ ping checks
func (l *InstanceList) PingCheck(i *Instance, sid string) {
	ticker := time.NewTicker(time.Second * time.Duration(l.Config.PingInterval))

	currentlyKilling := false

	for {
		select {
		case <-ticker.C:
			if i.SessionID == "" || sid != i.SessionID {
				log.Info("Cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ") is no longer eligible for ping checks from this goroutine")
				return // Stop observer if instance is stopped
			}

			log.Info("Pinging cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ") [automated ping check] at time: ", time.Now())
			if !i.Active {
				log.Info("Cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ") is not active. Stopping ping check.")
				PingCheckStop <- i.ClusterID
				return
			}

			if i.Command == nil {
				log.Error("Cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ") is not running. Stopping ping check.")
				PingCheckStop <- i.ClusterID
				return
			}

			// Get cluster health
			clusterHealth, err := l.ScanShards(i)

			if err == ErrTimeout {
				// Cluster is not responding, restart it

				// Log to action logs
				go l.ActionLog(map[string]any{
					"event": "ping_failure",
					"id":    i.ClusterID,
				})

				if i.Locked() {
					// Oops, we have a locked observer
					log.Error("Cluster locked, cannot restart ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ")")
					continue
				}

				log.Error("Cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ") is not responding. Restarting.")

				i.Lock(l, "PingCheck", false)

				currentlyKilling = true
				time.Sleep(time.Second * 1)
				l.Stop(i)
				time.Sleep(time.Second * 3)
				l.Start(i)
				currentlyKilling = false

				i.Unlock()

				return
			}

			if err != nil {
				log.Error("Ping error on cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, "): ", err)
			}

			i.ClusterHealth = clusterHealth
		case c := <-PingCheckStop:
			if currentlyKilling {
				// Currently killing, don't stop
				continue
			}

			if c == i.ClusterID {
				log.Info("Recieved request to end ping checks for cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ")")
				return
			}
		}
	}
}

// Observes a cluster and restarts it if necessary (unexpected death of the cluster)
func (l *InstanceList) Observe(i *Instance, sid string) {
	if err := i.Command.Wait(); err != nil {
		if i.SessionID == "" || sid != i.SessionID {
			return // Stop observer if instance is stopped
		}

		if i.Locked() {
			log.Error("Cluster locked, cannot restart ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ")")
			return
		}

		if l.RollRestarting {
			log.Error("Roll restart is in progress, ignoring restart on cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ")")
			return
		}

		i.Active = false
		i.Lock(l, "Observe", true)

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

		i.Unlock()
	}
}
