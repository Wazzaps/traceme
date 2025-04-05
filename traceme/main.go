package main

import (
	"context"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

var traceDir = "/var/traceme"
var socketDir = "/var/run"
var serverCmd []string
var subServers = map[string]SubServer{}
var subServersMutex sync.Mutex
var serversWg sync.WaitGroup

const indexHTML = `
<!DOCTYPE html>
<html>
<head>
    <title>In-progress Traces</title>
</head>
<body>
    <h1>In-progress Traces</h1>
	<table>
		<tr>
			<th>Client IP</th>
			<th>Age</th>
			<th>Actions</th>
		</tr>
		{{range .Traces}}
			<tr>
				<td>{{.ClientIp}}</td>
				<td>{{.Age}}</td>
				<td>
					<form action="/" method="post">
						<input type="hidden" name="clientIp" value="{{.ClientIp}}">
						<button type="submit" name="action" value="end">End</button>
					</form>
				</td>
			</tr>
		{{end}}
	</table>
</body>
</html>
`

var tmpl = template.Must(template.New("index").Parse(indexHTML))

type SubServer struct {
	Id        string
	startedAt time.Time
	clientIp  string
	proxy     *httputil.ReverseProxy
	cmd       *exec.Cmd
}

func (s *SubServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.proxy.ServeHTTP(w, r)
}

func handler(w http.ResponseWriter, r *http.Request) {
	remoteIp, _ := rpartition(r.RemoteAddr, ":")
	subServersMutex.Lock()
	subServer, ok := subServers[remoteIp]
	if !ok {
		socketPath := fmt.Sprintf("%s/server-%s.sock", socketDir, remoteIp)
		os.Remove(socketPath)
		println("Starting server for", remoteIp)

		traceId := fmt.Sprintf("%s-%s", remoteIp, time.Now().Format("2006-01-02-15-04-05"))
		tracePathParent := filepath.Join(traceDir, traceId)
		if err := os.MkdirAll(tracePathParent, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "failed to create trace directory: %v\n", err)
			os.Exit(1)
		}
		tracePath := filepath.Join(tracePathParent, "trace")
		recordCmd := exec.Command("/opt/traceme/bin/rr", append([]string{
			"record",
			"-W",
			"--output-trace-dir",
			tracePath,
			"--",
		}, serverCmd...)...)
		recordCmd.Stdin = os.Stdin
		recordCmd.Stdout = os.Stdout
		recordCmd.Stderr = os.Stderr
		recordCmd.Env = append(os.Environ(), fmt.Sprintf("LISTEN_UNIX=%s", socketPath))
		if err := recordCmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start server: %v\n", err)
			os.Exit(1)
		}
		go func() {
			err := recordCmd.Wait()
			subServersMutex.Lock()
			delete(subServers, remoteIp)
			subServersMutex.Unlock()

			// Pack the trace
			packCmd := exec.Command("/opt/traceme/bin/rr", "pack", tracePath)
			if err := packCmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "failed to pack trace: %v\n", err)
			}

			// Compress the trace
			tarCmd := exec.Command("tar", "--zstd", "-cf", tracePath+".tar.zst", "-C", tracePath, ".")
			if err := tarCmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "failed to create tar archive: %v\n", err)
			}

			// Cleanup trace directory
			if err := os.RemoveAll(tracePath); err != nil {
				fmt.Fprintf(os.Stderr, "failed to cleanup trace directory: %v\n", err)
			}

			if err != nil {
				fmt.Fprintf(os.Stderr, "Server for %s exited with error: %v\n", remoteIp, err)
			} else {
				fmt.Fprintf(os.Stderr, "Server for %s exited\n", remoteIp)
			}
			serversWg.Done()
		}()
		subServer = SubServer{
			Id:        traceId,
			startedAt: time.Now(),
			clientIp:  remoteIp,
			proxy: &httputil.ReverseProxy{
				Director: func(req *http.Request) {
					req.URL.Scheme = "http"
					req.URL.Host = "localhost"
				},
				Transport: &http.Transport{
					DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
						for range 30 {
							if _, err := os.Stat(socketPath); err == nil {
								return net.Dial("unix", socketPath)
							}
							time.Sleep(100 * time.Millisecond)
						}
						return nil, fmt.Errorf("server didn't start within 3 seconds")
					},
				},
			},
			cmd: recordCmd,
		}
		serversWg.Add(1)
		subServers[remoteIp] = subServer
	}
	subServersMutex.Unlock()
	subServer.ServeHTTP(w, r)
}

type TraceItem struct {
	Id       string
	ClientIp string
	Age      time.Duration
}

func controlGetHandler(w http.ResponseWriter, r *http.Request) {
	subServersMutex.Lock()
	defer subServersMutex.Unlock()

	traces := []TraceItem{}
	for _, subServer := range subServers {
		traces = append(traces, TraceItem{
			Id:       subServer.Id,
			ClientIp: subServer.clientIp,
			Age:      time.Since(subServer.startedAt),
		})
	}
	if err := tmpl.Execute(w, map[string]interface{}{
		"Traces": traces,
	}); err != nil {
		http.Error(w, fmt.Sprintf("Failed to render template: %v", err), http.StatusInternalServerError)
		return
	}
}

func controlPostHandler(w http.ResponseWriter, r *http.Request) {
	subServersMutex.Lock()
	defer subServersMutex.Unlock()

	action := r.FormValue("action")
	if action == "end" {
		clientIp := r.FormValue("clientIp")
		println("Ending trace for", clientIp)
		subServers[clientIp].cmd.Process.Signal(syscall.SIGTERM)
	} else {
		http.Error(w, fmt.Sprintf("Invalid action: %s", action), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func Run() error {
	// Get trace directory
	if envDir := os.Getenv("TRACE_DIR"); envDir != "" {
		traceDir = envDir
	}

	// Get socket directory
	if envDir := os.Getenv("SOCKET_DIR"); envDir != "" {
		socketDir = envDir
		if err := os.MkdirAll(socketDir, 0755); err != nil {
			return fmt.Errorf("failed to create socket directory: %w", err)
		}
	}

	// Get command line args after the program name
	serverCmd = os.Args[1:]
	if len(serverCmd) == 0 {
		return fmt.Errorf("no command specified")
	}

	// Handle SIGINT and SIGTERM
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signalChan
		println("Received signal, exiting...")
		subServersMutex.Lock()
		for _, subServer := range subServers {
			subServer.cmd.Process.Signal(syscall.SIGINT)
		}
		subServersMutex.Unlock()
		serversWg.Wait()
		os.Exit(0)
	}()

	// Start control server
	controlServerMux := http.NewServeMux()
	controlServerMux.HandleFunc("GET /", controlGetHandler)
	controlServerMux.HandleFunc("POST /", controlPostHandler)
	controlServer := &http.Server{
		Addr:    ":8000",
		Handler: controlServerMux,
	}
	go controlServer.ListenAndServe()

	// Start proxy server
	fmt.Println("traceme: Listening on port 8080")
	if err := http.ListenAndServe(":8080", http.HandlerFunc(handler)); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

func rpartition(s, sep string) (before, after string) {
	index := strings.LastIndex(s, sep)
	if index == -1 {
		// Separator not found
		return s, ""
	}
	before = s[:index]
	after = s[index+len(sep):]
	return
}

func main() {
	if err := Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
