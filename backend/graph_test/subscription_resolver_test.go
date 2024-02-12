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

package graph_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	k8stesting "k8s.io/client-go/testing"
)

type SubscriptionResolverTestSuite struct {
	GraphTestSuite
}

func (suite *SubscriptionResolverTestSuite) TestAppsV1DaemonSetsWatch() {
	// build query
	query := `
		subscription {
			appsV1DaemonSetsWatch {
				type
				object {
					metadata {
						name
					}
				}
			}
		}
	`

	// init reactor
	watcher := watch.NewFake()
	defer watcher.Stop()
	suite.resolver.TestClientset.PrependWatchReactor("daemonsets", k8stesting.DefaultWatchReactor(watcher, nil))

	// init subscription
	sub := suite.MustSubscribe(GraphQLRequest{Query: query}, nil)
	defer sub.Unsubscribe()

	// add data
	obj := appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	watcher.Add(&obj)

	// listen for new message
	data := struct {
		AppsV1DaemonSetsWatch struct {
			Type   string
			Object struct {
				Metadata struct {
					Name string
				}
			}
		}
	}{}
	sub.MustNextMsg(suite.T(), 1*time.Second, &data)
	suite.Equal("ADDED", data.AppsV1DaemonSetsWatch.Type)
	suite.Equal("x", data.AppsV1DaemonSetsWatch.Object.Metadata.Name)
}

func (suite *SubscriptionResolverTestSuite) TestAppsV1DeploymentsWatch() {
	// build query
	query := `
		subscription {
			appsV1DeploymentsWatch {
				type
				object {
					metadata {
						name
					}
				}
			}
		}
	`

	// init reactor
	watcher := watch.NewFake()
	defer watcher.Stop()
	suite.resolver.TestClientset.PrependWatchReactor("deployments", k8stesting.DefaultWatchReactor(watcher, nil))

	// init subscription
	sub := suite.MustSubscribe(GraphQLRequest{Query: query}, nil)
	defer sub.Unsubscribe()

	// add data
	obj := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	watcher.Add(&obj)

	// listen for new message
	data := struct {
		AppsV1DeploymentsWatch struct {
			Type   string
			Object struct {
				Metadata struct {
					Name string
				}
			}
		}
	}{}
	sub.MustNextMsg(suite.T(), 1*time.Second, &data)
	suite.Equal("ADDED", data.AppsV1DeploymentsWatch.Type)
	suite.Equal("x", data.AppsV1DeploymentsWatch.Object.Metadata.Name)
}

func (suite *SubscriptionResolverTestSuite) TestAppsV1ReplicaSetsWatch() {
	// build query
	query := `
		subscription {
			appsV1ReplicaSetsWatch {
				type
				object {
					metadata {
						name
					}
				}
			}
		}
	`

	// init reactor
	watcher := watch.NewFake()
	defer watcher.Stop()
	suite.resolver.TestClientset.PrependWatchReactor("replicasets", k8stesting.DefaultWatchReactor(watcher, nil))

	// init subscription
	sub := suite.MustSubscribe(GraphQLRequest{Query: query}, nil)
	defer sub.Unsubscribe()

	// add data
	obj := appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	watcher.Add(&obj)

	// listen for new message
	data := struct {
		AppsV1ReplicaSetsWatch struct {
			Type   string
			Object struct {
				Metadata struct {
					Name string
				}
			}
		}
	}{}
	sub.MustNextMsg(suite.T(), 1*time.Second, &data)
	suite.Equal("ADDED", data.AppsV1ReplicaSetsWatch.Type)
	suite.Equal("x", data.AppsV1ReplicaSetsWatch.Object.Metadata.Name)
}

func (suite *SubscriptionResolverTestSuite) TestAppsV1StatefulSetsWatch() {
	// build query
	query := `
		subscription {
			appsV1StatefulSetsWatch {
				type
				object {
					metadata {
						name
					}
				}
			}
		}
	`

	// init reactor
	watcher := watch.NewFake()
	defer watcher.Stop()
	suite.resolver.TestClientset.PrependWatchReactor("statefulsets", k8stesting.DefaultWatchReactor(watcher, nil))

	// init subscription
	sub := suite.MustSubscribe(GraphQLRequest{Query: query}, nil)
	defer sub.Unsubscribe()

	// add data
	obj := appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	watcher.Add(&obj)

	// listen for new message
	data := struct {
		AppsV1StatefulSetsWatch struct {
			Type   string
			Object struct {
				Metadata struct {
					Name string
				}
			}
		}
	}{}
	sub.MustNextMsg(suite.T(), 1*time.Second, &data)
	suite.Equal("ADDED", data.AppsV1StatefulSetsWatch.Type)
	suite.Equal("x", data.AppsV1StatefulSetsWatch.Object.Metadata.Name)
}

func (suite *SubscriptionResolverTestSuite) TestBatchV1CronJobsWatch() {
	// build query
	query := `
		subscription {
			batchV1CronJobsWatch {
				type
				object {
					metadata {
						name
					}
				}
			}
		}
	`

	// init reactor
	watcher := watch.NewFake()
	defer watcher.Stop()
	suite.resolver.TestClientset.PrependWatchReactor("cronjobs", k8stesting.DefaultWatchReactor(watcher, nil))

	// init subscription
	sub := suite.MustSubscribe(GraphQLRequest{Query: query}, nil)
	defer sub.Unsubscribe()

	// add data
	obj := batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	watcher.Add(&obj)

	// listen for new message
	data := struct {
		BatchV1CronJobsWatch struct {
			Type   string
			Object struct {
				Metadata struct {
					Name string
				}
			}
		}
	}{}
	sub.MustNextMsg(suite.T(), 1*time.Second, &data)
	suite.Equal("ADDED", data.BatchV1CronJobsWatch.Type)
	suite.Equal("x", data.BatchV1CronJobsWatch.Object.Metadata.Name)
}

func (suite *SubscriptionResolverTestSuite) TestBatchV1JobsWatch() {
	// build query
	query := `
		subscription {
			batchV1JobsWatch {
				type
				object {
					metadata {
						name
					}
				}
			}
		}
	`

	// init reactor
	watcher := watch.NewFake()
	defer watcher.Stop()
	suite.resolver.TestClientset.PrependWatchReactor("jobs", k8stesting.DefaultWatchReactor(watcher, nil))

	// init subscription
	sub := suite.MustSubscribe(GraphQLRequest{Query: query}, nil)
	defer sub.Unsubscribe()

	// add data
	obj := batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	watcher.Add(&obj)

	// listen for new message
	data := struct {
		BatchV1JobsWatch struct {
			Type   string
			Object struct {
				Metadata struct {
					Name string
				}
			}
		}
	}{}
	sub.MustNextMsg(suite.T(), 1*time.Second, &data)
	suite.Equal("ADDED", data.BatchV1JobsWatch.Type)
	suite.Equal("x", data.BatchV1JobsWatch.Object.Metadata.Name)
}

func (suite *SubscriptionResolverTestSuite) TestCoreV1NamespacesWatch() {
	// build query
	query := `
		subscription {
			coreV1NamespacesWatch {
				type
				object {
					metadata {
						name
					}
				}
			}
		}
	`

	// init reactor
	watcher := watch.NewFake()
	defer watcher.Stop()
	suite.resolver.TestClientset.PrependWatchReactor("namespaces", k8stesting.DefaultWatchReactor(watcher, nil))

	// init subscription
	sub := suite.MustSubscribe(GraphQLRequest{Query: query}, nil)
	defer sub.Unsubscribe()

	// add data
	obj := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	watcher.Add(&obj)

	// listen for new message
	data := struct {
		CoreV1NamespacesWatch struct {
			Type   string
			Object struct {
				Metadata struct {
					Name string
				}
			}
		}
	}{}
	sub.MustNextMsg(suite.T(), 1*time.Second, &data)
	suite.Equal("ADDED", data.CoreV1NamespacesWatch.Type)
	suite.Equal("x", data.CoreV1NamespacesWatch.Object.Metadata.Name)
}

func (suite *SubscriptionResolverTestSuite) TestCoreV1NodesWatch() {
	// build query
	query := `
		subscription {
			coreV1NodesWatch {
				type
				object {
					metadata {
						name
					}
				}
			}
		}
	`

	// init reactor
	watcher := watch.NewFake()
	defer watcher.Stop()
	suite.resolver.TestClientset.PrependWatchReactor("nodes", k8stesting.DefaultWatchReactor(watcher, nil))

	// init subscription
	sub := suite.MustSubscribe(GraphQLRequest{Query: query}, nil)
	defer sub.Unsubscribe()

	// add data
	obj := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	watcher.Add(&obj)

	// listen for new message
	data := struct {
		CoreV1NodesWatch struct {
			Type   string
			Object struct {
				Metadata struct {
					Name string
				}
			}
		}
	}{}
	sub.MustNextMsg(suite.T(), 1*time.Second, &data)
	suite.Equal("ADDED", data.CoreV1NodesWatch.Type)
	suite.Equal("x", data.CoreV1NodesWatch.Object.Metadata.Name)
}

func (suite *SubscriptionResolverTestSuite) TestCoreV1PodsWatch() {
	// build query
	query := `
		subscription {
			coreV1PodsWatch {
				type
				object {
					metadata {
						name
					}
				}
			}
		}
	`

	// init reactor
	watcher := watch.NewFake()
	defer watcher.Stop()
	suite.resolver.TestClientset.PrependWatchReactor("pods", k8stesting.DefaultWatchReactor(watcher, nil))

	// init subscription
	sub := suite.MustSubscribe(GraphQLRequest{Query: query}, nil)
	defer sub.Unsubscribe()

	// add data
	obj := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	watcher.Add(&obj)

	// listen for new message
	data := struct {
		CoreV1PodsWatch struct {
			Type   string
			Object struct {
				Metadata struct {
					Name string
				}
			}
		}
	}{}
	sub.MustNextMsg(suite.T(), 1*time.Second, &data)
	suite.Equal("ADDED", data.CoreV1PodsWatch.Type)
	suite.Equal("x", data.CoreV1PodsWatch.Object.Metadata.Name)
}

func (suite *SubscriptionResolverTestSuite) TestCoreV1PodLogTail() {
	// build query
	query := `
		subscription {
			coreV1PodLogTail(namespace: "ns", name: "x") {
				timestamp
				message
			}
		}
	`

	// init subscription
	sub := suite.MustSubscribe(GraphQLRequest{Query: query}, nil)
	defer sub.Unsubscribe()

	// get log records
	data := struct {
		CoreV1PodLogTail struct {
			Timestamp string
			Message   string
		}
	}{}
	sub.MustNextMsg(suite.T(), 1*time.Second, &data)

	// check record
	record := data.CoreV1PodLogTail
	suite.Equal("fake logs", record.Message)
	_, err := time.Parse(time.RFC3339Nano, record.Timestamp)
	suite.Nil(err)
}

// test runner
func TestSubscriptionResolver(t *testing.T) {
	suite.Run(t, new(SubscriptionResolverTestSuite))
}
