package main

import (
	"flag"
	"github.com/haveachin/infrared"
	"github.com/haveachin/infrared/cmd/infrared/command"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"io"
	"os"
	"strconv"
	"time"
)

const (
	envPrefix     = "INFRARED_"
	envDebug      = envPrefix + "DEBUG"
	envColor      = envPrefix + "COLOR"
	envConfigPath = envPrefix + "CONFIG_PATH"
)

const (
	clfDebug      = "debug"
	clfColor      = "color"
	clfConfigPath = "config-path"
)

var (
	debug      = false
	color      = true
	configPath = "./configs"
)

func envBool(name string, value bool) bool {
	envString := os.Getenv(name)
	if envString == "" {
		return value
	}

	envBool, err := strconv.ParseBool(envString)
	if err != nil {
		return value
	}

	return envBool
}

func envString(name string, value string) string {
	envString := os.Getenv(name)
	if envString == "" {
		return value
	}

	return envString
}

func initEnv() {
	debug = envBool(envDebug, debug)
	color = envBool(envColor, color)
	configPath = envString(envConfigPath, configPath)
}

func initFlags() {
	flag.BoolVar(&debug, clfDebug, debug, "starts infrared in debug mode")
	flag.BoolVar(&color, clfColor, color, "enables color in console logs")
	flag.StringVar(&configPath, clfConfigPath, configPath, "path of all proxy configs")
	flag.Parse()
}

func init() {
	initEnv()
	initFlags()

	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}
}

func main() {
	defaultConsoleWriter := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
		NoColor:    !color,
	}

	log.Logger = log.Output(defaultConsoleWriter)
	_ = startGateway(defaultConsoleWriter)
	command.Read(os.Stdin)
}

func startGateway(out io.Writer) *infrared.Gateway {
	vprs, err := infrared.ReadAllProxyConfigs(configPath)
	if err != nil {
		log.Fatal().Err(err)
		return nil
	}

	gateway := infrared.NewGateway()
	gateway.AddLoggerOutput(out)

	for _, vpr := range vprs {
		if _, err := gateway.AddProxyByViper(vpr); err != nil {
			log.Err(err)
		}
	}

	go func() {
		if err := gateway.ListenAndServe(); err != nil {
			log.Fatal().Err(err)
		}
	}()

	return &gateway
}
