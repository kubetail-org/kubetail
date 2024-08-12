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

package logmetadata2

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/fsnotify/fsnotify"
	"github.com/kubetail-org/kubetail/backend/common/agentpb"
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

// Check if container log file is in namespace
/*func isInNamespaces(pathname string, namespaces []string) (bool, error) {

}*/

// Container logs watcher instance
type containerLogsWatcher struct {
	watcher *fsnotify.Watcher
	Events  chan *agentpb.LogMetadataWatchEvent
}

// Close watcher
func (w *containerLogsWatcher) Close() error {
	return w.watcher.Close()
}

func newContainerLogsWatcher(ctx context.Context, containerLogsDir string, namespaces []string) (*containerLogsWatcher, error) {
	// create new watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// add current files to watcher
	err = filepath.Walk(containerLogsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// add link targets to watcher
		if info.Mode()&os.ModeSymlink != 0 {
			// TODO
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

	outCh := make(chan *agentpb.LogMetadataWatchEvent)

	// handle new files
	go func() {
		for {
			select {
			case <-ctx.Done():
				// handle context cancel
				return
			case inEv, ok := <-watcher.Events:
				// handle watcher close
				if !ok {
					close(outCh)
					return
				}

				// handle new files
				if inEv.Op&fsnotify.Create == fsnotify.Create {
					// TODO
				} else {
					// handle other events
					fmt.Println(inEv)
				}
			}
		}
	}()

	obj := &containerLogsWatcher{
		watcher: watcher,
		Events:  outCh,
	}

	return obj, nil
}
