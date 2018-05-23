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

func substr(str string, start, length int) string {
	rs := []rune(str)
	rl := len(rs)
	end := 0

	if start < 0 {
		start = rl - 1 + start
	}
	end = start + length

	if start > end {
		start, end = end, start
	}

	if start < 0 {
		start = 0
	}
	if start > rl {
		start = rl
	}
	if end < 0 {
		end = 0
	}
	if end > rl {
		end = rl
	}

	return string(rs[start:end])
}

func process() string {
	cmdStr := fmt.Sprintf("ps -A -o cmd")
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
		if len(i) < 2  {
			continue
		}

		if strings.HasPrefix(i, "[") {
			continue
		}
		if strings.HasPrefix(i, "-") {
			continue
		}
		i = strings.Replace(i, "\"", "'", -1)
		dl := strings.Split(i, " ")
		args := strings.TrimLeft(i, dl[0])
		args = strings.TrimLeft(args, " ")
		cmd := dl[0]
		cmd = strings.Replace(cmd, ":", "", 1)
		idx := strings.LastIndex(cmd, "/")
		if idx >= 0 {
			cmd = substr(cmd, idx + 1, 128)
		}
		if len(dl) == 1 {
			if _, ok := psMap[cmd]; ok {
				continue
			}
			psMap[cmd] = cmd
			ret = ret + fmt.Sprintf("alive{type=\"process\",pname=\"%s\",pargs=\"%s\"} 1\n",
				cmd, "null")
		} else {
			if strings.Contains(i, ": ") {
				if _, ok := psMap[cmd]; ok {
					continue
				}
				psMap[cmd] = i
				ret = ret + fmt.Sprintf("alive{type=\"process\",pname=\"%s\",pargs=\"%s\"} 1\n",
					cmd, "null")
			} else {
				if _, ok := psMap[i]; ok {
					continue
				}
				psMap[i] = i
				ret = ret + fmt.Sprintf("alive{type=\"process\",pname=\"%s\",pargs=\"%s\"} 1\n",
					cmd, args)
			}
		}
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
		if len(i) < 2 {
			continue
		}
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