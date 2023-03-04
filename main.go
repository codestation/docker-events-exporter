/*
Copyright 2023 codestation

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"

	"github.com/docker/docker/api/types"
	docker "github.com/docker/docker/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var listenAddr = flag.String("listen.addr", ":8000", "Listen address")

func main() {
	flag.Parse()

	cli, err := docker.NewClientWithOpts(docker.FromEnv, docker.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("Failed to connect to Docker: %s", err)
	}

	defer func(cli *docker.Client) {
		err := cli.Close()
		if err != nil {
			log.Printf("Failed to close docker client: %s", err)
		}
	}(cli)

	reg := prometheus.NewRegistry()

	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "docker_events",
		Help: "Number of docker container events",
	}, []string{"type", "action", "scope", "from", "name", "namespace", "service_name", "node_id", "service_id"})

	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		counter,
	)

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))

	httpServer := http.Server{Addr: *listenAddr}
	connClose := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		eventsChan, errChan := cli.Events(ctx, types.EventsOptions{})

		for {
			select {
			case event := <-eventsChan:
				switch event.Type {
				case "container":
					switch event.Action {
					case "die":
						// do not report containers that exited correctly
						if event.Actor.Attributes["exitCode"] == "0" {
							continue
						}
						fallthrough
					case "oom":
						fallthrough
					case "kill":
						counter.WithLabelValues(
							event.Type,
							event.Action,
							event.Scope,
							event.Actor.Attributes["image"],
							event.Actor.Attributes["name"],
							event.Actor.Attributes["com.docker.stack.namespace"],
							event.Actor.Attributes["com.docker.swarm.service.name"],
							event.Actor.Attributes["com.docker.swarm.node.id"],
							event.Actor.Attributes["com.docker.swarm.service.id"],
						).Inc()
					}
				}
			case err := <-errChan:
				log.Printf("Error while reading event: %s", err)
				return
			}
		}
	}()

	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt)
		<-sigint
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Printf("HTTP Server Shutdown failed: %v", err)
		}
		close(connClose)
	}()

	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Printf("HTTP Server failed: %v", err)
	}

	<-connClose
}
