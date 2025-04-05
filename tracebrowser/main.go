package main

import (
	"fmt"
	"html/template"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const indexHTML = `
<!DOCTYPE html>
<html>
<head>
    <title>Trace Browser</title>
</head>
<body>
    <h1>Available Traces</h1>
    <ul>
    {{range .Dirs}}
        <li><a href="http://{{$.Hostname}}/?folder={{$.StateDir}}/{{.}}/code/{{$.ProjectName}}">{{.}}</a></li>
    {{end}}
    </ul>
</body>
</html>
`

var tmpl = template.Must(template.New("index").Parse(indexHTML))

var projectName = ""
var projectPackage = ""
var traceDir = ""
var codeSrcDir = ""
var stateDir = ""
var readyServers = map[string]bool{}
var proxy *httputil.ReverseProxy

func handleIndex(w http.ResponseWriter, r *http.Request) {
	// Read directories from trace dir
	entries, err := os.ReadDir(traceDir)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read trace directory: %v", err), http.StatusInternalServerError)
		return
	}

	// Filter directories
	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			dirs = append(dirs, entry.Name())
		}
	}

	// Render template
	if err := tmpl.Execute(w, map[string]any{
		"Dirs":           dirs,
		"Hostname":       r.Host,
		"ProjectName":    projectName,
		"ProjectPackage": projectPackage,
		"StateDir":       template.URL(stateDir),
	}); err != nil {
		http.Error(w, fmt.Sprintf("Failed to render template: %v", err), http.StatusInternalServerError)
		return
	}
}

func handleTrace(w http.ResponseWriter, r *http.Request) {
	parsedQuery := r.URL.Query()
	relativeDir, found := strings.CutPrefix(parsedQuery.Get("folder"), stateDir)
	if !found {
		proxy.ServeHTTP(w, r)
		return
	}
	traceName := strings.Split(relativeDir, "/")[1]
	traceArchivePath := filepath.Join(traceDir, traceName, "trace.tar.zst")
	tracePath := filepath.Join(stateDir, traceName, "trace")
	codePath := filepath.Join(stateDir, traceName, "code", projectName)

	if _, ok := readyServers[traceName]; !ok {
		// Check if trace archive exists
		if _, err := os.Stat(traceArchivePath); os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}

		// Extract it if needed
		if _, err := os.Stat(tracePath); os.IsNotExist(err) {
			err = os.MkdirAll(tracePath, 0755)
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to create trace directory: %v", err), http.StatusInternalServerError)
				return
			}
			cmd := exec.Command("tar", "--zstd", "-xf", traceArchivePath, "-C", tracePath)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				http.Error(w, fmt.Sprintf("Failed to extract trace archive: %v", err), http.StatusInternalServerError)
				return
			}
		}

		// Create code dir
		if _, err := os.Stat(codePath); os.IsNotExist(err) {
			err = os.MkdirAll(codePath, 0755)
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to create code directory: %v", err), http.StatusInternalServerError)
				return
			}
			cmd := exec.Command("git", "clone", codeSrcDir, codePath)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				http.Error(w, fmt.Sprintf("Failed to clone code: %v", err), http.StatusInternalServerError)
				return
			}
		}

		// Create launch config
		launchConfig := filepath.Join(codePath, ".vscode", "launch.json")
		err := os.WriteFile(launchConfig, fmt.Appendf(nil, `{
			"version": "0.2.0",
			"configurations": [
				{
					"name": "Replay trace",
					"type": "go",
					"request": "launch",
					"mode": "replay",
					"program": "${workspaceFolder}/main.go",
					"traceDirPath": "${workspaceFolder}/../../trace",
					"env": {
						"DELVE_RR_REPLAY_FLAGS": "-W",
						"PATH": "/opt/traceme/bin:/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
					},
					"substitutePath": [
						{
							"from": "${workspaceFolder}",
							"to": "%s"
						},
						{
							"from": "/usr/local/go/src",
							"to": ""
						}
					]
				}
			]
		}`, projectPackage), 0644)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create launch config: %v", err), http.StatusInternalServerError)
			return
		}

		readyServers[traceName] = true
	}

	proxy.ServeHTTP(w, r)
}

func handler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" && r.URL.RawQuery == "" {
		handleIndex(w, r)
	} else {
		handleTrace(w, r)
	}
}

func main() {
	if envTraceDir := os.Getenv("TRACE_DIR"); envTraceDir != "" {
		traceDir = envTraceDir
	} else {
		fmt.Println("please set TRACE_DIR")
		os.Exit(1)
	}

	if envStateDir := os.Getenv("STATE_DIR"); envStateDir != "" {
		stateDir = envStateDir
	} else {
		fmt.Println("please set STATE_DIR")
		os.Exit(1)
	}

	if envCodeSrcDir := os.Getenv("CODE_SRC_DIR"); envCodeSrcDir != "" {
		codeSrcDir = envCodeSrcDir
	} else {
		fmt.Println("please set CODE_SRC_DIR")
		os.Exit(1)
	}

	if envProjectName := os.Getenv("PROJECT_NAME"); envProjectName != "" {
		projectName = envProjectName
	} else {
		fmt.Println("please set PROJECT_NAME")
		os.Exit(1)
	}

	if envProjectPackage := os.Getenv("PROJECT_PACKAGE"); envProjectPackage != "" {
		projectPackage = envProjectPackage
	} else {
		fmt.Println("please set PROJECT_PACKAGE")
		os.Exit(1)
	}

	// Start code-server
	codeServer := exec.Command(
		"/app/code-server/bin/code-server",
		"--bind-addr", "0.0.0.0:8081",
		"--user-data-dir", filepath.Join(stateDir, "data"),
		"--extensions-dir", filepath.Join(stateDir, "extensions"),
		"--disable-telemetry",
		"--auth", "none",
		"--disable-workspace-trust",
	)
	codeServer.Stdout = os.Stdout
	codeServer.Stderr = os.Stderr
	err := codeServer.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start code-server: %v\n", err)
		os.Exit(1)
	}
	proxy = httputil.NewSingleHostReverseProxy(&url.URL{
		Scheme: "http",
		Host:   "localhost:8081",
	})

	// Start proxy
	fmt.Println("Starting server on :8080")
	if err := http.ListenAndServe(":8080", http.HandlerFunc(handler)); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}
