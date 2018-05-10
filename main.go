package main

import (
	"flag"
	"net"
	"os"
	"net/http"
	"strings"
	"io"
	"fmt"
	"github.com/robfig/cron"
	"os/exec"
	"sync"
)

var (
	Name           = "alive_exporter"
	listenAddress  = flag.String("unix-sock", "/dev/shm/alive_exporter.sock", "Address to listen on for unix sock access and telemetry.")
	metricsPath    = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
)

var g_ret string
var g_lock sync.RWMutex
var doing bool


func process() string {
	cmdStr := fmt.Sprintf("top -c -b -n 1 | sed '1,7d' | sed /^$/d | grep -v top | grep -v grep | grep -v ']' | awk '{print $12}' | awk -F ':' '{print $1}' | sort | awk -F '/' '{print $NF}' | grep -v awk | grep -v sed | grep -v sort | grep -v uniq")
	cmd := exec.Command("/bin/sh", "-c", cmdStr)
	cmd.Wait()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	l := strings.Split(string(out), "\n")
	if len(l) == 0 {
		return ""
	}

	ret := ""
	var psMap map[string]string
	psMap = make(map[string]string)
	for _, i := range l {
		if _, ok := psMap[i]; ok {
			continue
		}
		psMap[i] = i
		ret = ret + fmt.Sprintf("alive{type=\"process\",pname=\"%s\"} 1\n", i)
	}

	return ret
}

func listen() string {
	cmdStr := fmt.Sprintf("ss -txl | sed '1,1d' | sed /^$/d | awk '{print $5}' | awk -F ':' '{print $NF}'")
	cmd := exec.Command("/bin/sh", "-c", cmdStr)
	cmd.Wait()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	l := strings.Split(string(out), "\n")
	if len(l) == 0 {
		return ""
	}

	ret := ""
	var lsMap map[string]string
	lsMap = make(map[string]string)
	for _, i := range l {
		if _, ok := lsMap[i]; ok {
			continue
		}
		lsMap[i] = i
		ret = ret + fmt.Sprintf("alive{type=\"listen\",lname=\"%s\"} 1\n", i)
	}

	return ret
}

func doWork() {
	if doing {
		return
	}
	doing = true

	p := process()
	l := listen()

	g_lock.Lock()
	g_ret = p + l
	g_lock.Unlock()

	doing = false
}

func metrics(w http.ResponseWriter, r *http.Request) {
    g_lock.RLock()
	io.WriteString(w, g_ret)
	g_lock.RUnlock()
}

func main() {
	flag.Parse()

	addr := "/dev/shm/alive_exporter.sock"
	if listenAddress != nil {
		addr = *listenAddress
	}

	doing = false
	c := cron.New()
	c.AddFunc("0 */1 * * * ?", doWork)
	c.Start()

	mux := http.NewServeMux()
	mux.HandleFunc(*metricsPath, metrics)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
             <head><title>Alive Exporter</title></head>
             <body>
             <h1>Alive Exporter</h1>
             <p><a href='` + *metricsPath + `'>Metrics</a></p>
             </body>
             </html>`))
	})
	server := http.Server{
		Handler: mux, // http.DefaultServeMux,
	}
	os.Remove(addr)

	listener, err := net.Listen("unix", addr)
	if err != nil {
		panic(err)
	}
	server.Serve(listener)
}