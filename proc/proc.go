package proc

import (
	"mewld/coreutils"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
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
}

type Instance struct {
	StartedAt time.Time
	SessionID string
	ClusterID int      // ClusterID from clustermap
	Shards    []uint64 // Shards that this instance is responsible for currently, should be equal to clustermap
	Command   *exec.Cmd
}

func (l *InstanceList) OsSignalHandle() {
	// Create a channel for signal handling
	sigs := make(chan os.Signal, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	// Wait for a signal in a loop
	for sig := range sigs {
		log.Info("Received signal: ", sig)

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

		// Exit
		os.Exit(0)
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
		dir+"/mew",
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

	l.Observe(i)
}

func (l *InstanceList) Observe(i *Instance) {
	if err := i.Command.Wait(); err != nil {
		log.Error("Cluster "+l.Cluster(i).Name+" ("+strconv.Itoa(l.Cluster(i).ID)+") died unexpectedly: ", err)

		if exiterr, ok := err.(*exec.ExitError); ok {
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				log.Infof("Exit Status: %d", status.ExitStatus())
			}
		}
	}
}
