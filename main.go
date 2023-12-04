package main

import (
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/cheesycod/mewld/config"
	"github.com/cheesycod/mewld/loader"

	_ "embed"

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

func SetLogLevel() {
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
	SetLogLevel()

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
		log.Fatal("Check config file again: ", err)
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
