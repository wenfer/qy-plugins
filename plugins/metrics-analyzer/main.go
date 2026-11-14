package main

import (
	"log"

	"github.com/wenfer/qy-plugins/pkg/pluginsdk"
	"github.com/wenfer/qy-plugins/plugins/metrics-analyzer/analyzer"
)

func main() {
	cfg := analyzer.DefaultConfig()
	plugin := analyzer.New(cfg)

	// pluginsdk.Serve bootstraps UDS connection, handshake, heartbeat,
	// and invokes plugin.Start(hostContext) and plugin.Stop() on shutdown.
	if err := pluginsdk.Serve(plugin); err != nil {
		log.Fatalf("[metrics-analyzer] Fatal runtime error: %v", err)
	}
}
