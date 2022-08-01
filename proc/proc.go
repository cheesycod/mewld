// Process manager for mewld
package proc

import (
	"mewld/config"
	"mewld/coreutils"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
)

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
	StartMutex           *sync.Mutex
}

type Instance struct {
	StartedAt time.Time
	SessionID string
	ClusterID int      // ClusterID from clustermap
	Shards    []uint64 // Shards that this instance is responsible for currently, should be equal to clustermap
	Command   *exec.Cmd
}

func (l *InstanceList) StartNext() {
	// Mutex to prevent multiple instances from starting at the same time
	l.StartMutex.Lock()
	defer l.StartMutex.Unlock()

	// Get next instance to start
	for _, i := range l.Instances {
		if i.Command == nil || i.Command.Process == nil {
			log.Info("Going to start *next* cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ")")
			l.Start(i)
			return
		}
	}
}

func (l *InstanceList) KillAll() {
	// Kill all instances
	for _, i := range l.Instances {
		if i.Command == nil {
			log.Error("Cluster " + l.Cluster(i).Name + " (" + strconv.Itoa(l.Cluster(i).ID) + ") is not running")
		} else {
			log.Info("Killing cluster " + l.Cluster(i).Name + " (" + strconv.Itoa(l.Cluster(i).ID) + ")")
			i.Command.Process.Kill()
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
		return StopCodeRestartFailed
	}

	log.Info("Stopping cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ")")

	i.Command.Process.Kill()

	return StopCodeNormal
}

func (l *InstanceList) Start(i *Instance) {
	i.StartedAt = time.Now()
	l.LastClusterStartedAt = time.Now()

	// Get python interpreter
	pyInterp := "/usr/bin/python3.10"

	if os.Getenv("PYTHON_INTERP") != "" {
		pyInterp = os.Getenv("PYTHON_INTERP")
	}

	dir, err := os.Getwd()

	log.Info("Starting cluster ", l.Cluster(i).Name, " (", l.Cluster(i).ID, ") in directory ", dir)

	if err != nil {
		log.Fatal(err)
	}

	cluster := l.Cluster(i)

	if cluster == nil {
		panic("Cluster not found")
	}

	//dQ := "\""

	// Deprecated (logging code, set 0 for now)
	loggingCode := "0"

	cmd := exec.Command(
		pyInterp,
		l.Dir+"/"+l.Config.Module,
		coreutils.ToPyListUInt64(i.Shards),
		coreutils.UInt64ToString(l.ShardCount),
		strconv.Itoa(i.ClusterID),
		cluster.Name,
		loggingCode,
		dir,
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	i.Command = cmd

	// Spawn process
	err = cmd.Start()

	if err != nil {
		log.Error("Cluster "+cluster.Name+"("+strconv.Itoa(cluster.ID)+") failed to start", err)
	}

	go l.Observe(i)
}

func (l *InstanceList) Observe(i *Instance) {
	if err := i.Command.Wait(); err != nil {
		log.Error("Cluster "+l.Cluster(i).Name+" ("+strconv.Itoa(l.Cluster(i).ID)+") died unexpectedly: ", err)

		if exiterr, ok := err.(*exec.ExitError); ok {
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				log.Infof("Exit Status: %d", status.ExitStatus())
			}
		}

		// Restart process
		time.Sleep(time.Second * 2)
		l.Stop(i)
		l.Start(i)
	}
}
