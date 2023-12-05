package main

import (
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/cheesycod/mewld/config"
	"github.com/cheesycod/mewld/loader"
	"github.com/cheesycod/mewld/utils"

	_ "embed"

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

func main() {
	utils.SetLogLevel()

	// Load the config file
	configFile := "config.yaml"

	for i, arg := range os.Args {
		if arg == "--cfg-file" {
			if len(os.Args) <= i+1 {
				log.Fatal("No config file specified")
			}
			configFile = os.Args[i+1]
		}
	}

	f, err := os.Open(configFile)

	if err != nil {
		log.Fatal("Could not open config file: ", err)
	}

	configBytes, err := io.ReadAll(f)

	if err != nil {
		log.Fatal("Could not read config file: ", err)
	}

	var config config.CoreConfig

	err = yaml.Unmarshal(configBytes, &config)

	if err != nil {
		log.Fatal("Config.yaml load failed. Check config file again: ", err)
	}

	if os.Getenv("MTOKEN") != "" {
		config.Token = os.Getenv("MTOKEN")
	}

	il, _, err := loader.Load(&config, nil)

	if err != nil {
		log.Fatal("Error loading instances: ", err)
	}

	// Wait here until we get a signal
	sigs := make(chan os.Signal, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigs

	log.Info("Received signal: ", sig)

	il.KillAll()

	// Exit
	os.Exit(0)
}
