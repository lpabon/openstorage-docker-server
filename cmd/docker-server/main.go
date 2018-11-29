package main

import (
	"fmt"
	"os"

	"github.com/libopenstorage/openstorage/volume"
	"github.com/lpabon/openstorage-docker-server/pkg/server"
)

func main() {
	d := "fake2"
	mgmtPort := 2376
	pluginPort := 2377

	sdksocket := fmt.Sprintf("/var/lib/osd/driver/%s-sdk.sock", d)
	if err := server.StartPluginAPI(
		d, sdksocket,
		volume.DriverAPIBase,
		volume.PluginAPIBase,
		uint16(mgmtPort),
		uint16(pluginPort),
	); err != nil {
		fmt.Println("Failed to start server")
		os.Exit(1)
	}

	select {}
}
