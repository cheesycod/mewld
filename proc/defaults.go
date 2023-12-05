package proc

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/cheesycod/mewld/coreutils"
	log "github.com/sirupsen/logrus"
)

func DefaultStart(l *InstanceList, i *Instance, cm *ClusterMap) error {
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
			cm.Name,
			loggingCode,
			l.Dir,
		)
	} else {
		cmd = exec.Command(
			l.Config.Module, // If no interpreter, we use the full module as the executable path
			coreutils.ToPyListUInt64(i.Shards),
			coreutils.UInt64ToString(l.ShardCount),
			strconv.Itoa(i.ClusterID),
			cm.Name,
			loggingCode,
			l.Dir,
		)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = l.Dir

	env := os.Environ()

	env = append(env, "MEWLD_CHANNEL="+l.Config.RedisChannel)

	cmd.Env = env

	i.Command = cmd

	// Spawn process
	return cmd.Start()
}

func DefaultOnReshard(l *InstanceList, i *Instance, cm *ClusterMap, oldShards []uint64, newShards []uint64) error {
	log.Info(fmt.Sprintf("Resharding cluster %s (%d) from %s to %s", cm.Name, cm.ID, coreutils.ToPyListUInt64(oldShards), coreutils.ToPyListUInt64(newShards)))
	return nil
}
