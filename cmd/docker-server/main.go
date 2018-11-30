package main

import (
	"flag"
	"os"

	"github.com/libopenstorage/openstorage/volume"
	"github.com/lpabon/openstorage-docker-server/pkg/server"
	"github.com/sirupsen/logrus"
)

const (
	pluginName = "docker-gateway"
	driverName = "fake"
	mgmtPort   = 2376
	pluginPort = 2377
)

var (
	endpoint string
)

func init() {
	flag.StringVar(&endpoint, "e", "localhost:9100", "Endpoint for sdksocket")
	flag.Parse()
}

func main() {
	logrus.Infof("Starting docker gateway with osd sdk: %s", endpoint)
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
