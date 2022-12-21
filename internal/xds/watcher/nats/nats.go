package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	bootstrapv3 "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/nats-io/nats.go"
	"github.com/segmentio/ksuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"sigs.k8s.io/yaml"

	"github.com/dio/proxy/internal/xds/config"
	xdsserver "github.com/dio/proxy/internal/xds/server"
)

func New(c *config.Bootstrap, updater xdsserver.SnaphotUpdater) *NatsWatcher {
	return &NatsWatcher{
		c:       c,
		updater: updater,
		proxyConfig: ProxyConfig{
			GatewayConfigs: make(map[string]GatewayConfig),
		},
	}
}

type NatsWatcher struct {
	c           *config.Bootstrap
	updater     xdsserver.SnaphotUpdater
	proxyConfig ProxyConfig
}

func (w *NatsWatcher) Run(ctx context.Context) error {
	configStream := "CONFIG"
	stateStream := "STATE"

	// First-time read.
	err := w.update(w.c.NodeID)
	if err != nil {
		return err
	}

	// Connect to NATS
	nc, err := nats.Connect(w.c.NatsURL)
	if err != nil {
		return err
	}

	// Create JetStream Context
	js, err := nc.JetStream(nats.PublishAsyncMaxPending(256))
	if err != nil {
		return err
	}

	// Create consumer for this proxy
	js.AddConsumer(configStream, &nats.ConsumerConfig{
		Durable:   w.c.NodeID,
		Name:      w.c.NodeID,
		AckPolicy: nats.AckExplicitPolicy,
	})

	// Create subscriber
	sub, err := js.PullSubscribe(configStream+"."+w.c.NodeID, w.c.NodeID, nats.BindStream(configStream))
	if err != nil {
		return err
	}

	// Config loop
	go func() {
		for {
			m, _ := sub.Fetch(1)
			if len(m) > 0 {
				lastConfig := &ProxyConfig{}
				err = json.Unmarshal(m[0].Data, lastConfig)
				if err != nil {
					continue
				}
				m[0].Ack()

				// add or update cached gateway config
				for k, v := range lastConfig.GatewayConfigs {
					w.proxyConfig.GatewayConfigs[k] = v
				}

				// update proxy snapshot
				_ = w.update(w.c.NodeID)
			}
		}
	}()

	// State loop
	go func() {
		for {
			if len(w.proxyConfig.GatewayConfigs) > 0 {
				ps := &ProxyState{
					ProxyVersion: fmt.Sprintf("%s (commit: %s)\n", w.c.Version, w.c.Commit),
					EnvoyVersion: w.c.EnvoyVersion,
				}
				ps.GatewayStates = make(map[string]string)

				for k, v := range w.proxyConfig.GatewayConfigs {
					ps.GatewayStates[k] = v.EnvoyConfigHash
				}

				byteData, err := json.Marshal(ps)
				if err != nil {
					log.Println(err)
				}

				_, err = js.Publish(stateStream+"."+w.c.NodeID, byteData)
				if err != nil {
					log.Println(err)
				}
			}
			// hardcoded every 60 seconds
			time.Sleep(60 * time.Second)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		}
	}
}

func (w *NatsWatcher) update(nodeID string) error {
	resources := make([]*bootstrapv3.Bootstrap, 0)
	for _, v := range w.proxyConfig.GatewayConfigs {
		j, err := yaml.YAMLToJSON([]byte(v.EnvoyConfig))
		if err != nil {
			return err
		}

		var resource bootstrapv3.Bootstrap
		err = protojson.Unmarshal(j, &resource)
		if err != nil {
			return err
		}
		resources = append(resources, &resource)
	}

	var merged bootstrapv3.Bootstrap_StaticResources
	for _, r := range resources {
		if r.StaticResources == nil {
			continue
		}
		proto.Merge(&merged, r.StaticResources)
	}

	snap, err := cache.NewSnapshot(nodeID+"~"+ksuid.New().String(), map[resource.Type][]types.Resource{
		resource.ClusterType:  clustersToResources(merged.Clusters),
		resource.ListenerType: listenersToResources(merged.Listeners),
	})
	if err != nil {
		return err
	}
	err = w.updater.UpdateSnaphot(context.Background(), nodeID, snap)
	if err != nil {
		return err
	}

	return nil
}

func clustersToResources(clusters []*clusterv3.Cluster) []types.Resource {
	messages := make([]types.Resource, 0, len(clusters))
	for _, cluster := range clusters {
		messages = append(messages, cluster)
	}
	return messages
}

func listenersToResources(listeners []*listenerv3.Listener) []types.Resource {
	messages := make([]types.Resource, 0, len(listeners)) // TODO(dio): Extract Routes.
	for _, listener := range listeners {
		messages = append(messages, listener)
	}
	return messages
}
