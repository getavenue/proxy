package directory

import (
	"context"
	_ "embed" // to allow embedding files.
	"errors"
	"fmt"
	"os"
	"path/filepath"

	bootstrapv3 "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/fsnotify/fsnotify"
	"github.com/segmentio/ksuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"sigs.k8s.io/yaml"

	"github.com/dio/proxy/internal/xds/config"
	xdsserver "github.com/dio/proxy/internal/xds/server"
)

//go:embed templates/sample-v3.yaml
var sampleV3 []byte

func New(c *config.Bootstrap, updater xdsserver.SnaphotUpdater) *DirectoryWatcher {
	return &DirectoryWatcher{
		c:       c,
		updater: updater,
	}
}

type DirectoryWatcher struct {
	c       *config.Bootstrap
	watcher *fsnotify.Watcher
	updater xdsserver.SnaphotUpdater
}

func (w *DirectoryWatcher) Run(ctx context.Context) error {
	if !isDir(w.c.Resources) {
		return errors.New("resources must be a directory")
	}

	// First-time read.
	err := w.update()
	if err != nil {
		return err
	}

	w.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	err = w.watcher.Add(w.c.Resources)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-w.watcher.Events:
			if !ok {
				continue
			}
			_ = w.update()
		case err := <-w.watcher.Errors:
			fmt.Println(err)
		}
	}
}

func (w *DirectoryWatcher) update() error {
	nodes := make(map[string][]*bootstrapv3.Bootstrap)

	if err := filepath.Walk(w.c.Resources,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}

			_, ok := nodes[filepath.Dir(path)]
			if !ok {
				nodes[filepath.Dir(path)] = make([]*bootstrapv3.Bootstrap, 0)
			}
			b, err := os.ReadFile(filepath.Clean(path))
			if err != nil {
				return err
			}

			j, err := yaml.YAMLToJSON(b)
			if err != nil {
				return err
			}

			var resource bootstrapv3.Bootstrap
			err = protojson.Unmarshal(j, &resource)
			if err != nil {
				return err
			}
			nodes[filepath.Dir(path)] = append(nodes[filepath.Dir(path)], &resource)
			return nil
		}); err != nil {
		return err
	}

	for nodeID, resources := range nodes {
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
			continue
		}
		err = w.updater.UpdateSnaphot(context.Background(), nodeID, snap)
		if err != nil {
			continue
		}
	}
	return nil
}

func isDir(path string) bool {
	if path == "" {
		return false
	}

	if _, err := os.Stat(filepath.Clean(path)); errors.Is(err, os.ErrNotExist) {
		// When the requested directory doesn't exist, create one and put a sample config.
		if err := os.MkdirAll(filepath.Clean(path), os.ModePerm); err != nil {
			return false
		}
		if err := os.WriteFile(filepath.Join(filepath.Clean(path), "sample.yaml"), sampleV3, os.ModePerm); err != nil {
			return false
		}
	}

	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return false
	}
	fileInfo, err := file.Stat()
	if err != nil {
		return false
	}
	_ = file.Close()
	return fileInfo.IsDir()
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
