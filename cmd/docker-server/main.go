package main

import (
	"fmt"

	"github.com/libopenstorage/openstorage/volume"
	"github.com/lpabon/openstorage-docker-server/pkg/server"
)

func main() {
	server.StartGraphAPI("fake", volume.PluginAPIBase)

	fmt.Println("vim-go")
}
