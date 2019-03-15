//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package pilot

import (
	"fmt"
	"github.com/hashicorp/go-multierror"
	"io"
	"istio.io/istio/pkg/test/framework2/components/environment/kube"
	"istio.io/istio/pkg/test/framework2/components/istio"
	"istio.io/istio/pkg/test/framework2/core"
	"net"

	testKube "istio.io/istio/pkg/test/kube"
)

const (
	pilotService = "istio-pilot"
	grpcPortName = "grpc-xds"
)

var (
	_ Instance = &kubeComponent{}
	_ io.Closer        = &kubeComponent{}
)

func newKube(ctx core.Context, cfg Config) (Instance, error) {
	c := &kubeComponent{

	}
	c.id = ctx.TrackResource(c)

	env := ctx.Environment().(*kube.Environment)

	// TODO: This should be obtained from an Istio deployment.
	icfg, err := istio.DefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	ns := icfg.SystemNamespace

	fetchFn := env.NewSinglePodFetch(ns, "istio=pilot")
	if err := env.WaitUntilPodsAreReady(fetchFn); err != nil {
		return nil, err
	}
	pods, err := fetchFn()
	if err != nil {
		return nil, err
	}
	pod := pods[0]

	port, err := getGrpcPort(env, ns)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			_ = c.Close()
		}
	}()

	// Start port-forwarding for pilot.
	options := &testKube.PodSelectOptions{
		PodNamespace: pod.Namespace,
		PodName:      pod.Name,
	}

	c.forwarder, err = env.NewPortForwarder(options, 0, port)
	if err != nil {
		return nil, err
	}
	if err = c.forwarder.Start(); err != nil {
		return nil, err
	}

	var addr *net.TCPAddr
	addr, err = net.ResolveTCPAddr("tcp", c.forwarder.Address())
	if err != nil {
		return nil, err
	}

	c.client, err = newClient(addr)
	if err != nil {
		return nil, err
	}

	return c, nil
}

type kubeComponent struct {
	id core.ResourceID

	*client

	forwarder testKube.PortForwarder
}

func (c *kubeComponent) ID() core.ResourceID {
	return c.id
}

//func (c *kubeComponent) Start(ctx core.Context) (err error) {
//
//
//}

// Close stops the kube pilot server.
func (c *kubeComponent) Close() (err error) {
	if c.client != nil {
		err = multierror.Append(err, c.client.Close()).ErrorOrNil()
		c.client = nil
	}

	if c.forwarder != nil {
		err = multierror.Append(err, c.forwarder.Close()).ErrorOrNil()
		c.forwarder = nil
	}
	return
}

func getGrpcPort(e *kube.Environment, ns string) (uint16, error) {
	svc, err := e.Accessor.GetService(ns, pilotService)
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve service %s: %v", pilotService, err)
	}
	for _, portInfo := range svc.Spec.Ports {
		if portInfo.Name == grpcPortName {
			return uint16(portInfo.TargetPort.IntValue()), nil
		}
	}
	return 0, fmt.Errorf("failed to get target port in service %s", pilotService)
}
