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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type QueryResolverTestSuite struct {
	GraphTestSuite
}

func (suite *QueryResolverTestSuite) TestAppsV1DaemonSetsGet() {
	// build query
	query := `
		{
			appsV1DaemonSetsGet(namespace: "ns", name: "x") {
				metadata {
					name
				}
			}
		}
	`

	// check not-found
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(1, len(resp.Errors))
		suite.Equal("daemonsets.apps \"x\" not found", resp.Errors[0].Message)
	}

	// add data
	obj := appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	suite.resolver.TestClientset.AppsV1().DaemonSets("ns").Create(context.Background(), &obj, metav1.CreateOptions{})

	// check found
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		data := struct {
			AppsV1DaemonSetsGet struct {
				Metadata struct {
					Name string
				}
			}
		}{}
		suite.MustUnpack(resp.Data, &data)
		suite.Equal("x", data.AppsV1DaemonSetsGet.Metadata.Name)
	}
}

func (suite *QueryResolverTestSuite) TestAppsV1DaemonSetsList() {
	// build query
	query := `
		{
			appsV1DaemonSetsList(namespace: "ns") {
				items {
					metadata {
						name
					}
				}
			}
		}
	`

	type Data struct {
		AppsV1DaemonSetsList struct {
			Items []struct {
				Metadata struct {
					Name string
				}
			}
		}
	}

	// check empty
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		var data Data
		suite.MustUnpack(resp.Data, &data)
		suite.Equal(0, len(data.AppsV1DaemonSetsList.Items))
	}

	// add data
	obj1 := appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "x1"}}
	suite.resolver.TestClientset.AppsV1().DaemonSets("ns").Create(context.Background(), &obj1, metav1.CreateOptions{})

	obj2 := appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "x2"}}
	suite.resolver.TestClientset.AppsV1().DaemonSets("ns").Create(context.Background(), &obj2, metav1.CreateOptions{})

	// check not empty
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		var data Data
		suite.MustUnpack(resp.Data, &data)
		suite.Equal(2, len(data.AppsV1DaemonSetsList.Items))
		suite.Equal("x1", data.AppsV1DaemonSetsList.Items[0].Metadata.Name)
		suite.Equal("x2", data.AppsV1DaemonSetsList.Items[1].Metadata.Name)
	}
}

func (suite *QueryResolverTestSuite) TestAppsV1DeploymentsGet() {
	// build query
	query := `
		{
			appsV1DeploymentsGet(namespace: "ns", name: "x") {
				metadata {
					name
				}
			}
		}
	`

	// check not-found
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(1, len(resp.Errors))
		suite.Equal("deployments.apps \"x\" not found", resp.Errors[0].Message)
	}

	// add data
	obj := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	suite.resolver.TestClientset.AppsV1().Deployments("ns").Create(context.Background(), &obj, metav1.CreateOptions{})

	// check found
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		data := struct {
			AppsV1DeploymentsGet struct {
				Metadata struct {
					Name string
				}
			}
		}{}
		suite.MustUnpack(resp.Data, &data)
		suite.Equal("x", data.AppsV1DeploymentsGet.Metadata.Name)
	}
}

func (suite *QueryResolverTestSuite) TestAppsV1DeploymentsList() {
	// build query
	query := `
		{
			appsV1DeploymentsList(namespace: "ns") {
				items {
					metadata {
						name
					}
				}
			}
		}
	`

	type Data struct {
		AppsV1DeploymentsList struct {
			Items []struct {
				Metadata struct {
					Name string
				}
			}
		}
	}

	// check empty
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		var data Data
		suite.MustUnpack(resp.Data, &data)
		suite.Equal(0, len(data.AppsV1DeploymentsList.Items))
	}

	// add data
	obj1 := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "x1"}}
	suite.resolver.TestClientset.AppsV1().Deployments("ns").Create(context.Background(), &obj1, metav1.CreateOptions{})

	obj2 := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "x2"}}
	suite.resolver.TestClientset.AppsV1().Deployments("ns").Create(context.Background(), &obj2, metav1.CreateOptions{})

	// check not empty
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		var data Data
		suite.MustUnpack(resp.Data, &data)
		suite.Equal(2, len(data.AppsV1DeploymentsList.Items))
		suite.Equal("x1", data.AppsV1DeploymentsList.Items[0].Metadata.Name)
		suite.Equal("x2", data.AppsV1DeploymentsList.Items[1].Metadata.Name)
	}
}

func (suite *QueryResolverTestSuite) TestAppsV1ReplicaSetsGet() {
	// build query
	query := `
		{
			appsV1ReplicaSetsGet(namespace: "ns", name: "x") {
				metadata {
					name
				}
			}
		}
	`

	// check not-found
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(1, len(resp.Errors))
		suite.Equal("replicasets.apps \"x\" not found", resp.Errors[0].Message)
	}

	// add data
	obj := appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	suite.resolver.TestClientset.AppsV1().ReplicaSets("ns").Create(context.Background(), &obj, metav1.CreateOptions{})

	// check found
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		data := struct {
			AppsV1ReplicaSetsGet struct {
				Metadata struct {
					Name string
				}
			}
		}{}
		suite.MustUnpack(resp.Data, &data)
		suite.Equal("x", data.AppsV1ReplicaSetsGet.Metadata.Name)
	}
}

func (suite *QueryResolverTestSuite) TestAppsV1ReplicaSetsList() {
	// build query
	query := `
		{
			appsV1ReplicaSetsList(namespace: "ns") {
				items {
					metadata {
						name
					}
				}
			}
		}
	`

	type Data struct {
		AppsV1ReplicaSetsList struct {
			Items []struct {
				Metadata struct {
					Name string
				}
			}
		}
	}

	// check empty
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		var data Data
		suite.MustUnpack(resp.Data, &data)
		suite.Equal(0, len(data.AppsV1ReplicaSetsList.Items))
	}

	// add data
	obj1 := appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "x1"}}
	suite.resolver.TestClientset.AppsV1().ReplicaSets("ns").Create(context.Background(), &obj1, metav1.CreateOptions{})

	obj2 := appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "x2"}}
	suite.resolver.TestClientset.AppsV1().ReplicaSets("ns").Create(context.Background(), &obj2, metav1.CreateOptions{})

	// check not empty
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		var data Data
		suite.MustUnpack(resp.Data, &data)
		suite.Equal(2, len(data.AppsV1ReplicaSetsList.Items))
		suite.Equal("x1", data.AppsV1ReplicaSetsList.Items[0].Metadata.Name)
		suite.Equal("x2", data.AppsV1ReplicaSetsList.Items[1].Metadata.Name)
	}
}

func (suite *QueryResolverTestSuite) TestAppsV1StatefulSetsGet() {
	// build query
	query := `
		{
			appsV1StatefulSetsGet(namespace: "ns", name: "x") {
				metadata {
					name
				}
			}
		}
	`

	// check not-found
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(1, len(resp.Errors))
		suite.Equal("statefulsets.apps \"x\" not found", resp.Errors[0].Message)
	}

	// add data
	obj := appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	suite.resolver.TestClientset.AppsV1().StatefulSets("ns").Create(context.Background(), &obj, metav1.CreateOptions{})

	// check found
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		data := struct {
			AppsV1StatefulSetsGet struct {
				Metadata struct {
					Name string
				}
			}
		}{}
		suite.MustUnpack(resp.Data, &data)
		suite.Equal("x", data.AppsV1StatefulSetsGet.Metadata.Name)
	}
}

func (suite *QueryResolverTestSuite) TestAppsV1StatefulSetsList() {
	// build query
	query := `
		{
			appsV1StatefulSetsList(namespace: "ns") {
				items {
					metadata {
						name
					}
				}
			}
		}
	`

	type Data struct {
		AppsV1StatefulSetsList struct {
			Items []struct {
				Metadata struct {
					Name string
				}
			}
		}
	}

	// check empty
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		var data Data
		suite.MustUnpack(resp.Data, &data)
		suite.Equal(0, len(data.AppsV1StatefulSetsList.Items))
	}

	// add data
	obj1 := appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "x1"}}
	suite.resolver.TestClientset.AppsV1().StatefulSets("ns").Create(context.Background(), &obj1, metav1.CreateOptions{})

	obj2 := appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "x2"}}
	suite.resolver.TestClientset.AppsV1().StatefulSets("ns").Create(context.Background(), &obj2, metav1.CreateOptions{})

	// check not empty
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		var data Data
		suite.MustUnpack(resp.Data, &data)
		suite.Equal(2, len(data.AppsV1StatefulSetsList.Items))
		suite.Equal("x1", data.AppsV1StatefulSetsList.Items[0].Metadata.Name)
		suite.Equal("x2", data.AppsV1StatefulSetsList.Items[1].Metadata.Name)
	}
}

func (suite *QueryResolverTestSuite) TestBatchV1CronJobsGet() {
	// build query
	query := `
		{
			batchV1CronJobsGet(namespace: "ns", name: "x") {
				metadata {
					name
				}
			}
		}
	`

	// check not-found
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(1, len(resp.Errors))
		suite.Equal("cronjobs.batch \"x\" not found", resp.Errors[0].Message)
	}

	// add data
	obj := batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	suite.resolver.TestClientset.BatchV1().CronJobs("ns").Create(context.Background(), &obj, metav1.CreateOptions{})

	// check found
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		data := struct {
			BatchV1CronJobsGet struct {
				Metadata struct {
					Name string
				}
			}
		}{}
		suite.MustUnpack(resp.Data, &data)
		suite.Equal("x", data.BatchV1CronJobsGet.Metadata.Name)
	}
}

func (suite *QueryResolverTestSuite) TestBatchV1CronJobsList() {
	// build query
	query := `
		{
			batchV1CronJobsList(namespace: "ns") {
				items {
					metadata {
						name
					}
				}
			}
		}
	`

	type Data struct {
		BatchV1CronJobsList struct {
			Items []struct {
				Metadata struct {
					Name string
				}
			}
		}
	}

	// check empty
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		var data Data
		suite.MustUnpack(resp.Data, &data)
		suite.Equal(0, len(data.BatchV1CronJobsList.Items))
	}

	// add data
	obj1 := batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "x1"}}
	suite.resolver.TestClientset.BatchV1().CronJobs("ns").Create(context.Background(), &obj1, metav1.CreateOptions{})

	obj2 := batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "x2"}}
	suite.resolver.TestClientset.BatchV1().CronJobs("ns").Create(context.Background(), &obj2, metav1.CreateOptions{})

	// check not empty
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		var data Data
		suite.MustUnpack(resp.Data, &data)
		suite.Equal(2, len(data.BatchV1CronJobsList.Items))
		suite.Equal("x1", data.BatchV1CronJobsList.Items[0].Metadata.Name)
		suite.Equal("x2", data.BatchV1CronJobsList.Items[1].Metadata.Name)
	}
}

func (suite *QueryResolverTestSuite) TestBatchV1JobsGet() {
	// build query
	query := `
		{
			batchV1JobsGet(namespace: "ns", name: "x") {
				metadata {
					name
				}
			}
		}
	`

	// check not-found
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(1, len(resp.Errors))
		suite.Equal("jobs.batch \"x\" not found", resp.Errors[0].Message)
	}

	// add data
	obj := batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	suite.resolver.TestClientset.BatchV1().Jobs("ns").Create(context.Background(), &obj, metav1.CreateOptions{})

	// check found
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		data := struct {
			BatchV1JobsGet struct {
				Metadata struct {
					Name string
				}
			}
		}{}
		suite.MustUnpack(resp.Data, &data)
		suite.Equal("x", data.BatchV1JobsGet.Metadata.Name)
	}
}

func (suite *QueryResolverTestSuite) TestBatchV1JobsList() {
	// build query
	query := `
		{
			batchV1JobsList(namespace: "ns") {
				items {
					metadata {
						name
					}
				}
			}
		}
	`

	type Data struct {
		BatchV1JobsList struct {
			Items []struct {
				Metadata struct {
					Name string
				}
			}
		}
	}

	// check empty
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		var data Data
		suite.MustUnpack(resp.Data, &data)
		suite.Equal(0, len(data.BatchV1JobsList.Items))
	}

	// add data
	obj1 := batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "x1"}}
	suite.resolver.TestClientset.BatchV1().Jobs("ns").Create(context.Background(), &obj1, metav1.CreateOptions{})

	obj2 := batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "x2"}}
	suite.resolver.TestClientset.BatchV1().Jobs("ns").Create(context.Background(), &obj2, metav1.CreateOptions{})

	// check not empty
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		var data Data
		suite.MustUnpack(resp.Data, &data)
		suite.Equal(2, len(data.BatchV1JobsList.Items))
		suite.Equal("x1", data.BatchV1JobsList.Items[0].Metadata.Name)
		suite.Equal("x2", data.BatchV1JobsList.Items[1].Metadata.Name)
	}
}

func (suite *QueryResolverTestSuite) TestCoreV1NamespacesList() {
	// build query
	query := `
		{
			coreV1NamespacesList {
				items {
					metadata {
						name
					}
				}
			}
		}
	`

	type Data struct {
		CoreV1NamespacesList struct {
			Items []struct {
				Metadata struct {
					Name string
				}
			}
		}
	}

	// check empty
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		var data Data
		suite.MustUnpack(resp.Data, &data)
		suite.Equal(0, len(data.CoreV1NamespacesList.Items))
	}

	// add data
	obj1 := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "x1"}}
	suite.resolver.TestClientset.CoreV1().Namespaces().Create(context.Background(), &obj1, metav1.CreateOptions{})

	obj2 := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "x2"}}
	suite.resolver.TestClientset.CoreV1().Namespaces().Create(context.Background(), &obj2, metav1.CreateOptions{})

	// check not empty
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		var data Data
		suite.MustUnpack(resp.Data, &data)
		suite.Equal(2, len(data.CoreV1NamespacesList.Items))
		suite.Equal("x1", data.CoreV1NamespacesList.Items[0].Metadata.Name)
		suite.Equal("x2", data.CoreV1NamespacesList.Items[1].Metadata.Name)
	}
}

func (suite *QueryResolverTestSuite) TestCoreV1NodesList() {
	// build query
	query := `
		{
			coreV1NodesList {
				items {
					metadata {
						name
					}
				}
			}
		}
	`

	type Data struct {
		CoreV1NodesList struct {
			Items []struct {
				Metadata struct {
					Name string
				}
			}
		}
	}

	// check empty
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		var data Data
		suite.MustUnpack(resp.Data, &data)
		suite.Equal(0, len(data.CoreV1NodesList.Items))
	}

	// add data
	obj1 := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "x1"}}
	suite.resolver.TestClientset.CoreV1().Nodes().Create(context.Background(), &obj1, metav1.CreateOptions{})

	obj2 := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "x2"}}
	suite.resolver.TestClientset.CoreV1().Nodes().Create(context.Background(), &obj2, metav1.CreateOptions{})

	// check not empty
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		var data Data
		suite.MustUnpack(resp.Data, &data)
		suite.Equal(2, len(data.CoreV1NodesList.Items))
		suite.Equal("x1", data.CoreV1NodesList.Items[0].Metadata.Name)
		suite.Equal("x2", data.CoreV1NodesList.Items[1].Metadata.Name)
	}
}

func (suite *QueryResolverTestSuite) TestCoreV1PodsGet() {
	// build query
	query := `
		{
			coreV1PodsGet(namespace: "ns", name: "x") {
				metadata {
					name
				}
			}
		}
	`

	// check not-found
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(1, len(resp.Errors))
		suite.Equal("pods \"x\" not found", resp.Errors[0].Message)
	}

	// add data
	obj := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	suite.resolver.TestClientset.CoreV1().Pods("ns").Create(context.Background(), &obj, metav1.CreateOptions{})

	// check found
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		data := struct {
			CoreV1PodsGet struct {
				Metadata struct {
					Name string
				}
			}
		}{}
		suite.MustUnpack(resp.Data, &data)
		suite.Equal("x", data.CoreV1PodsGet.Metadata.Name)
	}
}

func (suite *QueryResolverTestSuite) TestCoreV1PodsList() {
	// build query
	query := `
		{
			coreV1PodsList(namespace: "ns") {
				items {
					metadata {
						name
					}
				}
			}
		}
	`

	type Data struct {
		CoreV1PodsList struct {
			Items []struct {
				Metadata struct {
					Name string
				}
			}
		}
	}

	// check empty
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		var data Data
		suite.MustUnpack(resp.Data, &data)
		suite.Equal(0, len(data.CoreV1PodsList.Items))
	}

	// add data
	obj1 := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "x1"}}
	suite.resolver.TestClientset.CoreV1().Pods("ns").Create(context.Background(), &obj1, metav1.CreateOptions{})

	obj2 := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "x2"}}
	suite.resolver.TestClientset.CoreV1().Pods("ns").Create(context.Background(), &obj2, metav1.CreateOptions{})

	// check not empty
	{
		resp := suite.MustPost(GraphQLRequest{Query: query}, nil)
		suite.Equal(0, len(resp.Errors))

		var data Data
		suite.MustUnpack(resp.Data, &data)
		suite.Equal(2, len(data.CoreV1PodsList.Items))
		suite.Equal("x1", data.CoreV1PodsList.Items[0].Metadata.Name)
		suite.Equal("x2", data.CoreV1PodsList.Items[1].Metadata.Name)
	}
}

func (suite *QueryResolverTestSuite) TestCoreV1PodsGetLogs() {
	// build query
	query := `
		{
			coreV1PodsGetLogs(namespace: "ns", name: "x") {
				timestamp
				message
			}
		}
	`

	resp := suite.MustPost(GraphQLRequest{Query: query}, nil)

	// check response
	data := struct {
		CoreV1PodsGetLogs []struct {
			Timestamp string
			Message   string
		}
	}{}
	suite.MustUnpack(resp.Data, &data)
	suite.Equal(1, len(data.CoreV1PodsGetLogs))

	// check record
	record := data.CoreV1PodsGetLogs[0]
	suite.Equal("fake logs", record.Message)
	_, err := time.Parse(time.RFC3339Nano, record.Timestamp)
	suite.Nil(err)
}

// test runner
func TestQueryResolver(t *testing.T) {
	suite.Run(t, new(QueryResolverTestSuite))
}
