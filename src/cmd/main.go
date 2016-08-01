package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/fsouza/go-dockerclient"
)

func httpHandler(u *url.URL) http.HandlerFunc {
	var reverseProxy = httputil.NewSingleHostReverseProxy(u)
	return func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("http: %s\n", r.URL)
		reverseProxy.ServeHTTP(w, r)
	}
}

func websocketHandler(target string) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d, err := net.Dial("tcp", target)
		if err != nil {
			http.Error(w, "Error contacting backend server.", 500)
			log.Printf("Error dialing websocket backend %s: %v", target, err)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "Not a hijacker?", 500)
			return
		}
		nc, _, err := hj.Hijack()
		if err != nil {
			log.Printf("Hijack error: %v", err)
			return
		}
		defer nc.Close()
		defer d.Close()

		err = r.Write(d)
		if err != nil {
			log.Printf("Error copying request to target: %v", err)
			return
		}

		errc := make(chan error, 2)
		cp := func(dst io.Writer, src io.Reader) {
			_, err := io.Copy(dst, src)
			errc <- err
		}
		go cp(d, nc)
		go cp(nc, d)
		<-errc
	})
}

func StartEngine(id, ipaddr, port string) {
	var httpBackend *url.URL
	var err error

	httpBackend, err = url.Parse(fmt.Sprintf("http://%s:%s", ipaddr, port))
	if err != nil {
		return
	}

	wsBackend := fmt.Sprintf("%s:%s", ipaddr, port)

	http.HandleFunc("/api/kernels/", websocketHandler(fmt.Sprintf("%s:%s", ipaddr, port)))
	http.HandleFunc("/", httpHandler(httpBackend))
	http.ListenAndServe(":3000", nil)
}

func StartDocker(client *docker.Client, image string) (id, ipaddr, port string, err error) {
	c, err := client.CreateContainer(docker.CreateContainerOptions{
		Config: &docker.Config{Image: image},
	})
	if err != nil {
		return
	}

	err = client.StartContainer(c.ID, nil)
	if err != nil {
		return
	}

	c, err = client.InspectContainer(c.ID)
	if err != nil {
		return
	}

	ipaddr = c.NetworkSettings.IPAddress
	for k, _ := range c.NetworkSettings.Ports {
		port = string(k)
		if strings.HasSuffix(port, "/tcp") {
			port = port[:len(port)-4]
		}
		break
	}
	id = c.ID

	return
}

func main() {
	// var image = "7da29f069ae6"
	var image = "gibiansky/ihaskell:latest"
	client, _ := docker.NewClient("unix:///var/run/docker.sock")
	id, ipaddr, port, err := StartDocker(client, image)
	fmt.Println(id, ipaddr, port, err)
	if err == nil {
		StartEngine(id, ipaddr, port)
	}
}
