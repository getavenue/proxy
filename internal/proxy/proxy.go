// Copyright 2022 Dhi Aurrahman
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package proxy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/nats-io/nats.go"
	"github.com/oklog/run"

	"github.com/dio/proxy/config"
	"github.com/dio/proxy/downloader"
	"github.com/dio/proxy/handler"
	xdsconfig "github.com/dio/proxy/internal/xds/config"
	xdsserver "github.com/dio/proxy/internal/xds/server"
	_natsWatcher "github.com/dio/proxy/internal/xds/watcher/nats"
	"github.com/dio/proxy/runner"
)

type ProxyConnectConfig struct {
	NatsURL string `json:"nats_url"`
	NodeID  string `json:"node_id"`
}

// Run runs the main handler.
func Run(ctx context.Context, c *config.Bootstrap) error {
	var g run.Group
	g.Add(run.SignalHandler(ctx, os.Interrupt, syscall.SIGINT, syscall.SIGTERM))

	binaryPath, envoyVersion, err := downloader.Download(ctx, "")
	if err != nil {
		return err
	}

	// Avenue connect must be set
	if c.AvenueConnect == "" {
		return errors.New("Avenue connect must be set")
	}

	// Decode avenueConnect config
	connectConfig, e := base64.StdEncoding.DecodeString(c.AvenueConnect)
	if e != nil {
		return err
	}

	var pc ProxyConnectConfig
	err = json.Unmarshal(connectConfig, &pc)
	if e != nil {
		return err
	}

	xdsBootstrap := &xdsconfig.Bootstrap{
		Resources:     c.XDSResources,
		ListenAddress: fmt.Sprintf(":%d", c.XDSServerPort),
		NatsURL:       pc.NatsURL,
		NodeID:        pc.NodeID,
		Version:       c.Version,
		Commit:        c.Commit,
		EnvoyVersion:  envoyVersion,
		NginxConfig:   c.NginxConfig,
	}
	c.NodeID = pc.NodeID

	xdsServer := xdsserver.New(xdsBootstrap)
	{
		runCtx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			return xdsServer.Run(runCtx)
		}, func(err error) {
			cancel()
		})
	}

	natsWatcher := _natsWatcher.New(xdsBootstrap, xdsServer)
	{
		runCtx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			return natsWatcher.Run(runCtx)
		}, func(err error) {
			cancel()
		})
	}

	// Connect to NATS
	nc, err := nats.Connect(pc.NatsURL)
	if err != nil {
		return err
	}

	// Handle config preparation, config watching, TLS establishment.
	h := handler.New(c)
	args, err := h.Args()
	if err != nil {
		return err
	}
	defer func() {
		_ = args.Cleanup()
	}()

	if c.Output != "" {
		return nil
	}

	{
		r := runner.New(binaryPath, false, nc, c.AdminPort, pc.NodeID)
		runCtx, cancel := context.WithCancel(ctx)
		g.Add(func() error {
			return r.Run(runCtx, args.Values)
		},
			func(err error) {
				cancel()
			})
	}

	if err := g.Run(); err != nil {
		if _, ok := err.(run.SignalError); ok {
			return nil
		}
		return err
	}
	return nil
}
