// Copyright 2024 Andres Morey
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package logmetadata

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/kubetail-org/kubetail/modules/common/agentpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Regex for container log file names
var logfileRegex = regexp.MustCompile(`^(?P<PodName>[^_]+)_(?P<Namespace>[^_]+)_(?P<ContainerName>.+)-(?P<ContainerID>[^-]+)\.log$`)

// Check if client has required pods/log permissions for given namespace+verb
func checkPermission(ctx context.Context, clientset kubernetes.Interface, namespaces []string, verb string) error {
	// ensure namespaces argument is present
	if len(namespaces) < 1 {
		return errors.New("namespaces required")
	}

	// check each namespace individually
	for _, namespace := range namespaces {
		sar := &authv1.SelfSubjectAccessReview{
			Spec: authv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authv1.ResourceAttributes{
					Namespace: namespace,
					Group:     "",
					Verb:      verb,
					Resource:  "pods/log",
				},
			},
		}

		if result, err := clientset.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{}); err != nil {
			return err
		} else if !result.Status.Allowed {
			msg := fmt.Sprintf("permission denied: `%s pods/log` in namespace `%s`", verb, namespace)
			return status.Errorf(codes.Unauthenticated, msg)
		}
	}

	return nil
}

// Get file info for file
func newLogMetadataFileInfo(pathname string) (*agentpb.LogMetadataFileInfo, error) {
	// do stat
	fileInfo, err := os.Stat(pathname)
	if err != nil {
		return nil, err
	}

	// init output
	out := &agentpb.LogMetadataFileInfo{
		Size:           fileInfo.Size(),
		LastModifiedAt: timestamppb.New(fileInfo.ModTime()),
	}

	return out, nil
}

// Get log metadata from file
func newLogMetadataSpec(nodeName string, pathname string) (*agentpb.LogMetadataSpec, error) {
	// parse file name
	matches := logfileRegex.FindStringSubmatch(filepath.Base(pathname))
	if matches == nil {
		return nil, fmt.Errorf("filename format incorrect: %s", pathname)
	}

	spec := &agentpb.LogMetadataSpec{
		NodeName:      nodeName,
		PodName:       matches[1],
		Namespace:     matches[2],
		ContainerName: matches[3],
		ContainerId:   matches[4],
	}

	return spec, nil
}

var errUnhandledOp = errors.New("unhandled event op")

// generate new LogMetadataWatchEvent from an fsnotify event
func newLogMetadataWatchEvent(event fsnotify.Event, nodeName string) (*agentpb.LogMetadataWatchEvent, error) {
	// init spec
	spec, err := newLogMetadataSpec(nodeName, event.Name)
	if err != nil {
		return nil, err
	}

	// init watch event
	watchEv := &agentpb.LogMetadataWatchEvent{
		Object: &agentpb.LogMetadata{
			Id:       spec.ContainerId,
			Spec:     spec,
			FileInfo: &agentpb.LogMetadataFileInfo{},
		},
	}

	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
		watchEv.Type = "ADDED"
		if fileInfo, err := newLogMetadataFileInfo(event.Name); err != nil {
			return nil, err
		} else {
			watchEv.Object.FileInfo = fileInfo
		}
	case event.Op&fsnotify.Write == fsnotify.Write:
		watchEv.Type = "MODIFIED"
		if fileInfo, err := newLogMetadataFileInfo(event.Name); err != nil {
			return nil, err
		} else {
			watchEv.Object.FileInfo = fileInfo
		}
	case event.Op&fsnotify.Remove == fsnotify.Remove:
		watchEv.Type = "DELETED"
		watchEv.Object.FileInfo = &agentpb.LogMetadataFileInfo{}
	default:
		return nil, errUnhandledOp
	}

	return watchEv, nil
}

// check if a container log file is in a given namespace
func isInNamespace(pathname string, namespaces []string) bool {
	// split on underscores
	parts := strings.SplitN(filepath.Base(pathname), "_", 3)
	if len(parts) < 3 {
		return false
	}

	// allow all
	if namespaces[0] == "" {
		return true
	}

	// check if file's namespace is in namespace list
	return slices.Contains(namespaces, parts[1])
}

// Container logs watcher instance
type containerLogsWatcher struct {
	watcher *fsnotify.Watcher
	Events  chan fsnotify.Event
	closed  bool
	mu      sync.Mutex
}

// Close watcher
func (clw *containerLogsWatcher) Close() error {
	clw.mu.Lock()
	defer clw.mu.Unlock()

	if clw.closed {
		return nil
	}

	err := clw.watcher.Close()
	close(clw.Events)
	clw.closed = true
	return err
}

// Close checker
func (clw *containerLogsWatcher) IsClosed() bool {
	clw.mu.Lock()
	defer clw.mu.Unlock()
	return clw.closed
}

func newContainerLogsWatcher(ctx context.Context, containerLogsDir string, namespaces []string) (*containerLogsWatcher, error) {
	if len(namespaces) < 1 {
		return nil, fmt.Errorf("namespaces required")
	}

	// create new watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	symlinkCache := make(map[string]string)

	addTarget := func(pathname string) error {
		// get target
		target, err := os.Readlink(pathname)
		if err != nil {
			return err
		}

		// cache result
		symlinkCache[target] = pathname

		// add target to watcher
		if err := watcher.Add(target); err != nil {
			return err
		}

		return nil
	}

	// add current files to watcher
	err = filepath.Walk(containerLogsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if isInNamespace(path, namespaces) {
			return addTarget(path)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// listen for new files
	if err := watcher.Add(containerLogsDir); err != nil {
		return nil, err
	}

	clw := &containerLogsWatcher{
		watcher: watcher,
		Events:  make(chan fsnotify.Event),
	}

	// handle new files
	go func() {
		defer clw.Close()

		for {
			select {
			case <-ctx.Done():
				// kill goroutine on context cancel
				return
			case <-watcher.Errors:
				// kill goroutine on watcher errors
				return
			case inEv, ok := <-watcher.Events:
				// kill goroutine on watcher close
				if !ok || clw.IsClosed() {
					return
				}

				// handle new files
				if inEv.Op&fsnotify.Create == fsnotify.Create {
					if isInNamespace(inEv.Name, namespaces) {
						addTarget(inEv.Name)
					} else {
						// exit loop if not in namespace
						continue
					}
				} else {
					// check cache
					filename, exists := symlinkCache[inEv.Name]
					if !exists {
						continue
					}
					inEv.Name = filename
				}

				// write to output channel
				clw.mu.Lock()
				select {
				case clw.Events <- inEv:
				case <-ctx.Done(): // Handle case where context is canceled before sending
					clw.mu.Unlock()
					return
				}
				clw.mu.Unlock()
			}
		}
	}()

	return clw, nil
}
