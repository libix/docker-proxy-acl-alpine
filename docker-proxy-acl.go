package main

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/mux"
	"github.com/namsral/flag"
)

type UpStream struct {
	Name   string
	handle *http.Client
}

func (r UpStream) Pass() func(res http.ResponseWriter, req *http.Request) {
	return func(res http.ResponseWriter, req *http.Request) {
		if req.Method != "GET" {
			http.Error(res, "400 Bad request ; only GET allowed.", 400)
			return
		}
		param := ""
		if len(req.URL.RawQuery) > 0 {
			param = "?" + req.URL.RawQuery
		}
		body, _ := r.Get("http://docker"+req.URL.Path+param, res)
		fmt.Fprintf(res, "%s", body)
	}
}

func (r UpStream) Get(url string, res http.ResponseWriter) ([]byte, error) {
	req, err := r.handle.Get(url)
	if err != nil {
		return nil, err
	}
	defer req.Body.Close()
	contentType := req.Header.Get("Content-type")
	if contentType != "" {
		res.Header().Set("Content-type", contentType)
	}
	return ioutil.ReadAll(req.Body)
}

func newProxySocket(socket string) UpStream {
	stream := UpStream{Name: socket}
	stream.handle = &http.Client{
		Transport: &http.Transport{
			Dial: func(proto, addr string) (net.Conn, error) {
				conn, err := net.Dial("unix", socket)
				return conn, err
			},
		},
	}
	return stream
}

type stringSlice []string

func (s *stringSlice) String() string {
	return fmt.Sprintf("%d", *s)
}

func (s *stringSlice) Set(value string) error {
	fmt.Sprintf("Allowing endpoint: %s\n", value)
	*s = append(*s, value)
	return nil
}

func main() {
	fs := flag.NewFlagSetWithEnvPrefix(os.Args[0], "GO", 0)
	var (
		allowed    stringSlice
		allowedMap map[string]bool = make(map[string]bool)
		filename                   = fs.String("filename", "/tmp/docker-proxy-acl/docker.sock", "Location of socket file")
	)
	fs.Var(&allowed, "a", "Allowed location pattern prefix")
	fs.Parse(os.Args[1:])

	if len(allowed) < 1 {
		fmt.Println("Need at least 1 argument for -a: [containers, networks, version, info, ping]")
		os.Exit(0)
	}

	for _, s := range allowed {
		allowedMap[s] = true
	}

	var routers [2]*mux.Router
	routers[0] = mux.NewRouter()
	routers[1] = routers[0].PathPrefix("/{version:[v][0-9]+[.][0-9]+}").Subrouter()

	upstream := newProxySocket("/var/run/docker.sock")

	if allowedMap["containers"] {
		fmt.Printf("Registering container handlers\n")
		for _, m := range routers {
			containers := m.PathPrefix("/containers").Subrouter()
			containers.HandleFunc("/json", upstream.Pass())
			containers.HandleFunc("/{name}/json", upstream.Pass())
		}
	}

	if allowedMap["images"] {
		fmt.Printf("Registering images handlers\n")
		for _, m := range routers {
			containers := m.PathPrefix("/images").Subrouter()
			containers.HandleFunc("/json", upstream.Pass())
			containers.HandleFunc("/{name}/json", upstream.Pass())
			containers.HandleFunc("/{name}/history", upstream.Pass())
		}
	}

	if allowedMap["volumes"] {
		fmt.Printf("Registering volumes handlers\n")
		for _, m := range routers {
			m.HandleFunc("/volumes", upstream.Pass())
			m.HandleFunc("/volumes/{name}", upstream.Pass())
		}
	}

	if allowedMap["networks"] {
		fmt.Printf("Registering networks handlers\n")
		for _, m := range routers {
			m.HandleFunc("/networks", upstream.Pass())
			m.HandleFunc("/networks/{name}", upstream.Pass())
		}
	}

	if allowedMap["services"] {
		fmt.Printf("Registering services handlers\n")
		for _, m := range routers {
			m.HandleFunc("/services", upstream.Pass())
			m.HandleFunc("/services/{name}", upstream.Pass())
		}
	}

	if allowedMap["tasks"] {
		fmt.Printf("Registering tasks handlers\n")
		for _, m := range routers {
			m.HandleFunc("/tasks", upstream.Pass())
			m.HandleFunc("/tasks/{name}", upstream.Pass())
		}
	}

	if allowedMap["events"] {
		fmt.Printf("Registering events handlers\n")
		for _, m := range routers {
			m.HandleFunc("/events", upstream.Pass())
		}
	}

	if allowedMap["version"] {
		fmt.Printf("Registering version handlers\n")
		for _, m := range routers {
			m.HandleFunc("/version", upstream.Pass())
		}
	}

	if allowedMap["info"] {
		fmt.Printf("Registering info handlers\n")
		for _, m := range routers {
			m.HandleFunc("/info", upstream.Pass())
		}
	}

	if allowedMap["ping"] {
		fmt.Printf("Registering ping handlers\n")
		for _, m := range routers {
			m.HandleFunc("/_ping", upstream.Pass())
		}
	}

	http.Handle("/", routers[0])

	l, err := net.Listen("unix", *filename)
	os.Chmod(*filename, 0666)
	// Looking up group ids coming up for Go 1.7
	// https://github.com/golang/go/issues/2617

	fmt.Println("[docker-proxy-acl] Listening on " + *filename)

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, os.Kill, syscall.SIGTERM)
	go func(c chan os.Signal) {
		sig := <-c
		fmt.Printf("[docker-proxy-acl] Caught signal %s: shutting down.\n", sig)
		l.Close()
		os.Exit(0)
	}(sigc)

	if err != nil {
		panic(err)
	} else {
		err := http.Serve(l, nil)
		if err != nil {
			panic(err)
		}
	}
}
