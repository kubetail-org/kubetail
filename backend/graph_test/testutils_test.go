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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	gqlclient "github.com/hasura/go-graphql-client"
	"github.com/mitchellh/mapstructure"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/suite"
	"github.com/vektah/gqlparser/v2/gqlerror"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicFake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kubetail-org/kubetail/graph"
)

func init() {
	// disable logging
	log.Logger = zerolog.Nop()
}

type VariableMap map[string]interface{}

type PrepareContextFunc = func(context.Context) context.Context

type GraphQLRequest struct {
	Query     string      `json:"query"`
	Variables VariableMap `json:"variables"`
}

type GraphQLResponse struct {
	Data   interface{}
	Errors gqlerror.List
}

type GraphTestSuite struct {
	suite.Suite
	resolver         *graph.Resolver
	gqlHandler       *handler.Server
	server           *httptest.Server
	prepareContextFn PrepareContextFunc
}

func (suite *GraphTestSuite) SetupSuite() {
	resolver := &graph.Resolver{}
	gqlHandler := graph.NewHandler(resolver, nil)
	server := httptest.NewServer(prepareContextMiddleware(gqlHandler, suite))

	suite.resolver = resolver
	suite.gqlHandler = gqlHandler
	suite.server = server
}

func (suite *GraphTestSuite) TearDownSuite() {
	suite.server.Close()
}

func (suite *GraphTestSuite) SetupTest() {
	// init fake clientset
	suite.resolver.TestClientset = fake.NewSimpleClientset()

	// init fake dynamic client
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := batchv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	suite.resolver.TestDynamicClient = dynamicFake.NewSimpleDynamicClient(scheme)
}

func (suite *GraphTestSuite) PopulateDynamicClient(ns string, objects ...runtime.Object) {
	for _, obj := range objects {
		// get gvr
		gvr, err := graph.GetGVR(obj)
		if err != nil {
			panic(err)
		}

		// initialize unstructured object
		unstrObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			panic(err)
		}
		x := unstructured.Unstructured{Object: unstrObj}

		// create
		_, err = suite.resolver.TestDynamicClient.Resource(gvr).Namespace(ns).Create(context.Background(), &x, metav1.CreateOptions{})
		if err != nil {
			panic(err)
		}
	}
}

func (suite *GraphTestSuite) Post(request GraphQLRequest, prepareContext PrepareContextFunc) (*http.Response, error) {
	// json-encode graphql request
	requestBody, err := json.Marshal(&request)
	if err != nil {
		return nil, err
	}

	// init request
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", bytes.NewReader(requestBody))
	r.Header.Set("Content-Type", "application/json")

	// prepare context
	if prepareContext != nil {
		r = r.WithContext(prepareContext(r.Context()))
	}

	// execute request
	suite.gqlHandler.ServeHTTP(w, r)

	return w.Result(), nil
}

func (suite *GraphTestSuite) MustPost(req GraphQLRequest, prepareContext PrepareContextFunc) GraphQLResponse {
	httpResp, err := suite.Post(req, prepareContext)

	// check http response
	suite.Require().Nil(err)
	suite.Require().Equal(http.StatusOK, httpResp.StatusCode)

	// get body as bytes
	defer httpResp.Body.Close()
	bodyBytes, err := io.ReadAll(httpResp.Body)
	suite.Require().Nil(err)

	// json-decode body
	gqlResp := GraphQLResponse{}
	err = json.Unmarshal(bodyBytes, &gqlResp)
	suite.Require().Nil(err)

	return gqlResp
}

func (suite *GraphTestSuite) Subscribe(request GraphQLRequest, prepareContextFn PrepareContextFunc) (*GraphQLSubscription, error) {
	suite.prepareContextFn = prepareContextFn

	client := gqlclient.NewSubscriptionClient(suite.server.URL).
		WithProtocol(gqlclient.GraphQLWS)

	// init subscription object
	sub := &GraphQLSubscription{
		client: client,
		c:      make(chan []byte, 10),
	}

	// set up graphql request
	id, err := client.Exec(request.Query, request.Variables, func(data []byte, err error) error {
		if err != nil {
			panic(err)
		}
		sub.c <- data // write to subscription data channel
		return nil
	})

	if err != nil {
		return nil, err
	}

	// update subscription object
	sub.subscriptionId = id

	// start connection
	go func() {
		defer client.Close()
		client.Run()
	}()

	return sub, nil
}

func (suite *GraphTestSuite) MustSubscribe(request GraphQLRequest, prepareContextFn PrepareContextFunc) *GraphQLSubscription {
	sub, err := suite.Subscribe(request, prepareContextFn)
	suite.Require().Nil(err, "subscription failed")
	return sub
}

func (suite *GraphTestSuite) MustUnpack(data interface{}, into interface{}) {
	err := unpack(data, into)
	suite.Require().Nil(err, fmt.Errorf("Error while unpacking into %T", into))
}

// GraphQL subscription object
type GraphQLSubscription struct {
	client         *gqlclient.SubscriptionClient
	subscriptionId string
	c              chan []byte
}

func (sub *GraphQLSubscription) NextMsg(timeout time.Duration) ([]byte, error) {
	// init timer
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	// listen for new messages
	select {
	case <-timer.C:
		return nil, errors.New("timeout exceeded")
	case msg := <-sub.c:
		return msg, nil
	}
}

func (sub *GraphQLSubscription) MustNextMsg(t *testing.T, timeout time.Duration, responsePtr interface{}) {
	msg, err := sub.NextMsg(timeout)
	if err != nil {
		t.Fatal(err)
	}

	jsonMap := make(map[string]interface{})
	err = json.Unmarshal(msg, &jsonMap)
	if err != nil {
		t.Fatal(err)
	}

	err = unpack(jsonMap, responsePtr)
	if err != nil {
		t.Fatal(err)
	}
}

func (sub *GraphQLSubscription) Unsubscribe() {
	defer sub.client.Close()
	sub.client.Unsubscribe(sub.subscriptionId)
}

func unpack(data interface{}, into interface{}) error {
	d, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:      into,
		TagName:     "json",
		ErrorUnused: true,
		ZeroFields:  true,
	})
	if err != nil {
		return fmt.Errorf("mapstructure: %w", err)
	}

	return d.Decode(data)
}

func prepareContextMiddleware(next http.Handler, suite *GraphTestSuite) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if suite.prepareContextFn != nil {
			r = r.WithContext(suite.prepareContextFn(r.Context()))
			suite.prepareContextFn = nil
		}
		next.ServeHTTP(w, r)
	})
}
