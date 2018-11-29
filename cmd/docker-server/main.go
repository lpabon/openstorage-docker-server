package main

import (
	"github.com/libopenstorage/openstorage/volume"
	"github.com/lpabon/openstorage-docker-server/pkg/server"
)

func main() {
	server.StartGraphAPI("fake", volume.PluginAPIBase)
}
