package utils

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/cheesycod/mewld/config"
	"github.com/cheesycod/mewld/coreutils"
	"github.com/cheesycod/mewld/proc"

	log "github.com/sirupsen/logrus"
)

func ReadLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

// Given a shard count, return the shards for each cluster (128 would be [[0, 1, ..., 9], [10, 11, ..., 19]])
// However, if the shard count is not a multiple of the number of clusters, the last cluster will have fewer shards etc.
// So, 1 would mean [[0]]
func GetClusterList(clusterNames []string, shards uint64, perCluster uint64) []proc.ClusterMap {
	var clusterMap []proc.ClusterMap

	var shardArr []uint64
	var cid int = -1 // We start at -1 because we increment it before we use it
	for i := uint64(0); i < shards; i++ {
		if uint64(len(shardArr)) >= perCluster {
			if cid >= len(clusterNames)-3 {
				// Create a new cluster name using random string
				clusterNames = append(clusterNames, coreutils.RandomString(10))
			}
			cid++
			clusterMap = append(clusterMap, proc.ClusterMap{ID: cid, Name: clusterNames[cid], Shards: shardArr})
			shardArr = []uint64{}
		}

		shardArr = append(shardArr, i)
	}

	if len(shardArr) > 0 {
		cid++
		clusterMap = append(clusterMap, proc.ClusterMap{ID: cid, Name: clusterNames[cid], Shards: shardArr})
	}

	return clusterMap
}

// Given a config, return the directory to use
func ConfigGetDirectory(config *config.CoreConfig) (string, error) {
	var dir string
	var err error
	if config.OverrideDir != "" {
		dir = config.OverrideDir
	} else {
		var dirname string
		if config.UseCurrentDirectory {
			dirname, err = os.Getwd()

			if err != nil {
				return "", fmt.Errorf("could not find current directory: %w", err)
			}
		} else {
			dirname, err = os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("could not find home directory: %w", err)
			}
		}

		dir = dirname + "/" + config.Dir
	}

	return dir, nil
}

type SessionStartLimit struct {
	Total          uint64 `json:"total"`
	Remaining      uint64 `json:"remaining"`
	ResetAfter     uint64 `json:"reset_after"`
	MaxConcurrency uint64 `json:"max_concurrency"`
}

type ShardCount struct {
	Shards            uint64            `json:"shards"`
	SessionStartLimit SessionStartLimit `json:"session_start_limit"`
}

func GetShardCount() ShardCount {
	url := "https://discord.com/api/gateway/bot"

	req, err := http.NewRequest("GET", url, nil)

	req.Header.Add("Authorization", "Bot "+os.Getenv("MTOKEN"))
	req.Header.Add("User-Agent", "MewBot/1.0")
	req.Header.Add("Content-Type", "application/json")

	if err != nil {
		log.Fatal(err)
	}

	client := http.Client{Timeout: 10 * time.Second}

	res, err := client.Do(req)

	if err != nil {
		log.Fatal(err)
	}

	defer res.Body.Close()

	log.Println("Shard count status:", res.Status)

	if res.StatusCode != 200 {
		log.Fatal("Shard count status code not 200. Invalid token?")
	}

	var shardCount ShardCount

	bodyBytes, err := io.ReadAll(res.Body)

	if err != nil {
		log.Fatal(err)
	}

	err = json.Unmarshal(bodyBytes, &shardCount)

	if err != nil {
		log.Fatal(err)
	}

	if shardCount.SessionStartLimit.Remaining < 10 {
		log.Fatal("Shard count remaining is less than safe value of 10")
	}

	return shardCount
}
