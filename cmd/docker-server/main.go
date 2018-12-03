package main

import (
	"flag"
	"os"

	"github.com/libopenstorage/openstorage/volume"
	"github.com/lpabon/openstorage-docker-server/pkg/server"
	"github.com/sirupsen/logrus"
)

const (
	mgmtPort   = 2376
	pluginPort = 2377
)

var (
	endpoint   string
	pluginName string
	driverName string
)

func init() {
	flag.StringVar(&endpoint, "e", "localhost:9100", "Endpoint for sdksocket")
	flag.StringVar(&pluginName, "p", "osd-gateway", "Name for our plugin")
	flag.StringVar(&driverName, "d", "fake", "Driver we want to use")
}

func main() {
	flag.Parse()

	logrus.Infof("Starting %s with osd sdk: %s (%s driver)", pluginName, endpoint, driverName)
	if err := server.StartPluginAPI(
		pluginName, driverName, endpoint,
		volume.DriverAPIBase,
		volume.PluginAPIBase,
		uint16(mgmtPort),
		uint16(pluginPort),
	); err != nil {
		logrus.Errorf("Failed to start server: %s", err)
		os.Exit(1)
	}

	select {}
}
