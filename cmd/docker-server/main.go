package main

import (
	"flag"
	"os"

	"github.com/libopenstorage/openstorage/volume"
	"github.com/lpabon/openstorage-docker-server/pkg/server"
	"github.com/sirupsen/logrus"
)

func main() {
	d := "fake"
	mgmtPort := 2376
	pluginPort := 2377
	e := flag.String("e", "localhost:9100", "Endpoint for sdksocket")

	logrus.Info("Starting docker server with OSD endpoint " + *e)
	if err := server.StartPluginAPI(
		d, *e,
		volume.DriverAPIBase,
		volume.PluginAPIBase,
		uint16(mgmtPort),
		uint16(pluginPort),
	); err != nil {
		logrus.Error("Failed to start server")
		os.Exit(1)
	}

	select {}
}
