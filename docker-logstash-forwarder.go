package main

import (
	"flag"
	"log"
	"os"
	"sync"
	"time"

	"github.com/digital-wonderland/docker-logstash-forwarder/forwarder"
	docker "github.com/fsouza/go-dockerclient"
)

var (
	client           *docker.Client
	configFile       string
	dockerEndPoint   string
	laziness         int
	logstashEndPoint string
	refresh          configRefresh
	wg               sync.WaitGroup
)

type configRefresh struct {
	mu          sync.Mutex
	isTriggered bool
	timer       *time.Timer
}

func listenToDockerEvents(client *docker.Client) {
	wg.Add(1)
	defer wg.Done()

	events := make(chan *docker.APIEvents)
	defer close(events)

	err := client.AddEventListener((chan<- *docker.APIEvents)(events))
	if err != nil {
		log.Fatalf("Unable to add docker event listener: %s", err)
	}
	defer client.RemoveEventListener(events)

	log.Println("Listening to docker events...")
	for {
		event := <-events

		if event == nil {
			continue
		}

		if event.Status == "start" || event.Status == "stop" || event.Status == "die" {
			log.Printf("Received event %s for container %s", event.Status, event.ID[:12])

			refresh.mu.Lock()
			if !refresh.isTriggered {
				log.Printf("Triggering refresh in %d seconds", laziness)
				refresh.timer = time.AfterFunc(time.Duration(laziness)*time.Second, triggerRefresh)
				refresh.isTriggered = true
			}
			refresh.mu.Unlock()
		}
	}
}

func triggerRefresh() {
	refresh.mu.Lock()
	refresh.isTriggered = false
	refresh.mu.Unlock()
	forwarder.GenerateConfig(client, getLogstashEndpoint(), configFile)
}

func initFlags() {
	flag.StringVar(&dockerEndPoint, "docker", "", "docker api endpoint - defaults to $DOCKER_HOST or unix:///var/run/docker.sock")
	flag.IntVar(&laziness, "lazyness", 5, "number of seconds to wait after an event for events to accumulate")
	flag.StringVar(&logstashEndPoint, "logstash", "", "logstash endpoint - defaults to $LOGSTASH_HOST or logstash:5043")
	flag.StringVar(&configFile, "config", "", "logstash-forwarder config")
	flag.Parse()
}

func main() {
	log.Printf("Starting up")

	initFlags()

	endpoint := getDockerEndpoint()

	d, err := docker.NewClient(endpoint)
	if err != nil {
		log.Fatalf("Unable to connect to docker at %s: %s", endpoint, err)
	}
	client = d
	version, err := client.Version()
	if err != nil {
		log.Fatalf("Unable to retrieve version information from docker: %s", err)
	}
	log.Printf("Connected to docker at %s (v%s)", endpoint, version.Get("Version"))

	triggerRefresh()
	listenToDockerEvents(client)
	wg.Wait()

	log.Println("done")
}

func getDockerEndpoint() string {
	return getEndPoint("unix:///var/run/docker.sock", dockerEndPoint, "DOCKER_HOST")
}

func getLogstashEndpoint() string {
	return getEndPoint("logstash:5043", logstashEndPoint, "LOGSTASH_HOST")
}

func getEndPoint(sensibleDefault string, flag string, envVar string) string {
	if flag != "" {
		return flag
	} else if os.Getenv(envVar) != "" {
		return os.Getenv(envVar)
	} else {
		return sensibleDefault
	}
}