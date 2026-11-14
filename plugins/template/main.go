package main

import (
	"context"
	"log"
	"time"

	"github.com/wenfer/qy-plugins/pkg/pluginsdk"
)

// StarterPlugin demonstrates implementing the pluginsdk.Plugin interface.
type StarterPlugin struct {
	logger pluginsdk.Logger
	cancel context.CancelFunc
}

// Start is called when the UDS socket connection and handshake succeed.
func (p *StarterPlugin) Start(ctx pluginsdk.HostContext) error {
	p.logger = ctx.Log()
	p.logger.Infof("Starter Plugin initialized successfully")

	subCtx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	// Example: Query host for active server list
	servers, err := ctx.GetServerList(subCtx)
	if err != nil {
		p.logger.Warnf("Failed to query servers: %v", err)
	} else {
		p.logger.Infof("Successfully connected to host. Detected %d servers.", len(servers))
	}

	// Example: Background worker loop
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-subCtx.Done():
				return
			case <-ticker.C:
				p.logger.Debugf("Heartbeat tick from starter plugin worker")
			}
		}
	}()

	return nil
}

// Stop is called during graceful shutdown.
func (p *StarterPlugin) Stop() error {
	if p.cancel != nil {
		p.cancel()
	}
	if p.logger != nil {
		p.logger.Infof("Starter Plugin stopped cleanly")
	}
	return nil
}

func main() {
	plugin := &StarterPlugin{}

	// pluginsdk.Serve bootstraps UDS connection, handshake, heartbeat,
	// and invokes plugin.Start(ctx) and plugin.Stop() on host termination.
	if err := pluginsdk.Serve(plugin); err != nil {
		log.Fatalf("[starter-template] Fatal error: %v", err)
	}
}
