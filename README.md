# traceme

> [!WARNING]
> This is a proof-of-concept that contains hacky code and hardcoded constants.
>
> Do not expect things to work without tinkering.
>
> Do not run in production without ensuring it's secure.

## What

A small reverse proxy that spawns a separate instance of your (Go[^1]) web app per client (by source IP), and runs it under the [rr](https://rr-project.org/)[^2] time-travel debugger.

It also includes a "trace browser" that launches a preconfigured in-browser vscode that's ready to debug the selected trace.

[^1]: Technically any native language is supported, but non-static binaries are problematic with rr.soft

[^2]: We actually use a fork of rr called [rr.soft](https://github.com/sidkshatriya/rr.soft) that removes the hardware performance counter requirement, which allows it to run on commodity cloud instances.

## Why

Reproducing bugs is hard. Let your users do it, and when they reproduce it, you can debug it all you want.

## Usage

(Assuming `go` and `docker` are installed, and the user can run `docker` commands without sudo)

```shell
# rr requirement
sudo sysctl -w kernel.perf_event_paranoid=1

# Clone this repo and an example project
# The default setup assumes the repos are beside each other
git clone git@github.com:Wazzaps/traceme.git
git clone git@github.com:Wazzaps/traceme-example-webcounter.git

# Start the tracebrowser in the background
cd traceme
make docker-image/traceme
# You may want to set the PROJECT_NAME, PROJECT_PACKAGE, and APP_PATH variables if
# you're not using the example app. See the Makefile for more details.
make start-tracebrowser

# Start the example app in the background
cd ../traceme-example-webcounter
# If you specified a custom TRACE_DIR, specify it here as well
make start-webcounter

# Make some requests to the example app
curl -X POST http://127.0.0.1:8080/increment
curl -X POST http://127.0.0.1:8080/increment
curl -X POST http://127.0.0.1:8080/increment
curl http://127.0.0.1:8080/

# Stop the trace via the GUI
open http://127.0.0.1:8000/

# Debug the trace:
# - Select your trace
# - Install the go extension when prompted
# - Open `main.go`
# - Place a breakpoint on both `fmt.Fprintf` calls
# - Press F5 (The "Debug: Start Debugging" action in vscode)
# - Continue execution, see the "value" variable change
# - Press "Reverse", watch the value go back
# - Optionally assign a shortcut to `workbench.action.debug.reverseContinue`
# - You may debug the trace any number of times, until you find the bug
open http://127.0.0.1:8090/

# Clean up
make stop-webcounter
cd ../traceme
make stop-tracebrowser
```

## How

The `traceme` go module gets compiled into the `wazzaps/traceme` docker image (that can be used with `COPY --from=wazzaps/traceme:latest /opt/traceme /opt/traceme` and `CMD ["/opt/traceme/bin/traceme", "/your-app"]` in your app's `Dockerfile`).

The only dependency from your docker image is `glibc` and `tar` with `zstd` compression support.

It listens on port 8080 for requests, and whenever it gets a new client (by source IP), it spawns a new instance of your app under [rr](https://rr-project.org/). Eventually the trace is compressed and stored in `$TRACE_DIR`.

It also listens on port 8000, which displays a simple web UI to stop running traces.

The trace browser (which listens on port 8080, but is exposed on 8090 in the example) extracts the trace, clones a fresh copy of the app's code (to have a clean working area) and redirects to an instance of `code-server`.

It also overwrites the `.vscode/launch.json` file to load the trace correctly.

## Future work

- [ ] Clone the correct app version instead of the latest commit
- [ ] Fetch the correct golang stdlib version inside the trace browser
- [ ] Stop the trace automatically after a period of inactivity
- [ ] Send traces to S3 instead of storing them locally, allow stopping traces centrally (NATS?)
- [ ] Remove dependency on `glibc` and `tar` to work in Alpine images
- [ ] Integrate with Kubernetes to trace unmodified images
- [ ] Prove aarch64 support (on AWS Graviton and Apple Silicon)
- [ ] Allow multiple debugging sessions per trace
- [ ] Extract key points from the trace (e.g. network requests, database queries, etc.) for jumping closer to the action
- [ ] Data watchpoints (blocked on delve)
- [ ] Multiple AI agents debugging your code simultaneously
- [ ] ???
- [ ] Profit!
