# Golang Counter Application

This is a simple counter application written in Go. It exposes an HTTP API that allows you to increment a counter and get its current value. The application is optimized for performance and can be deployed in a Kubernetes environment.

### Table of Contents
- [API Endpoints](#api-endpoints)
- [Running the Application Locally and developing](#running-the-application-locally-and-developing)
- [Running tests and benchmarks](#running-tests-and-benchmarks)
- [Deployment](#deployment)
- [Performance Optimizations](#performance-optimizations)
- [TODO](#todo)

## API Endpoints

- `/status`: Increments the counter and returns its current value. The counter is stored in memory and will be reset if the application restarts. It uses atomic operations for incrementing the counter
```
curl http://127.0.0.1/status

{"counter":305689,"status":false,"lastChanged":"2023-12-11T04:45:08Z"}
```
- `/statusV2`: Similar to `/status`, but uses a mutex for synchronization instead of atomic operations.
```
curl http://127.0.0.1/statusV2

{"counter":305689,"status":false,"lastChanged":"2023-12-11T04:45:08Z"}
```

- `/health`: Returns a 200 OK response. Used for liveness probes in a Kubernetes environment.

- `/ready`: Returns a 200 OK response. Used for readiness probes in a Kubernetes environment.


In case of errors the application returns a 500 Internal Server Error response or 404 Not Found response depending on the case.

## Running the Application Locally and developing

1. Install Go: Follow the instructions at https://golang.org/doc/install to download and install Go.

2. Clone the repository: `git clone https://github.com/jinxmcg/counter.git`

3. Navigate to the project directory: `cd counter`

4. Run the application: `go run main.go`

The application will start and listen on port 8080.

## Running tests and benchmarks

1. Navigate to the project directory: `cd counter`

2. Run the tests: `go test -v`

```
➜  counter git:(main) ✗ go test -v
2023/12/10 15:06:11 Inited counter app
2023/12/10 15:06:11 Number of CPU cores available on node: 20
2023/12/10 15:06:11 CPU Limit imposed by k8s (cgroup): 20.00 cores
=== RUN   TestStatusHandlerAtomic_ConcurrentExecution
--- PASS: TestStatusHandlerAtomic_ConcurrentExecution (0.00s)
=== RUN   TestStatusHandlerAtomic_ResponseAndErrorHandling
--- PASS: TestStatusHandlerAtomic_ResponseAndErrorHandling (0.00s)
=== RUN   TestStatusHandlerMutex_ConcurrentExecution
--- PASS: TestStatusHandlerMutex_ConcurrentExecution (0.00s)
=== RUN   TestStatusHandlerMutex_ResponseAndErrorHandling
--- PASS: TestStatusHandlerMutex_ResponseAndErrorHandling (0.00s)
PASS
ok      counter 0.012s
```

3. Run the test and benchmarks: `go test -bench=. -benchmem`

```
➜  counter git:(main) ✗ go test -bench=. -benchmem

2023/12/10 15:08:02 Inited counter app
2023/12/10 15:08:02 Number of CPU cores available on node: 20
2023/12/10 15:08:02 CPU Limit imposed by k8s (cgroup): 20.00 cores
goos: linux
goarch: amd64
pkg: counter
cpu: Intel(R) Core(TM) i9-7900X CPU @ 3.30GHz
BenchmarkStatusHandlerAtomic-20                  1085088              1162 ns/op             549 B/op          2 allocs/op
BenchmarkStatusHandlerAtomicParallel-20          7686867               236.1 ns/op           519 B/op          2 allocs/op
BenchmarkStatusHandlerMutex-20                   1000000              1670 ns/op             192 B/op          3 allocs/op
BenchmarkStatusHandlerMutexParallel-20           1217329              1071 ns/op             192 B/op          3 allocs/op
PASS
ok      counter 8.702s
```


## Deployment

**Prerequisites:**
- Docker
- Kubernetes cluster (e.g. k3s or cloud environment)

The application can be deployed in a Kubernetes environment. A `Dockerfile` is provided for building a Docker image of the application, and a `kubernetes.yaml` file is provided for creating a Kubernetes Deployment, Service and Ingress.

To build the Docker image, run: 

`DOCKER_BUILDKIT=1 docker build . --tag counter:v0.3` v0.3 is the tag used in kubernetes.yaml

**Tests are also run during the build process in the Dockerfile**

To deploy the application to Kubernetes, run: 

`kubectl apply -f kubernetes.yaml`

The Kubernetes Deployment includes liveness and readiness probes that hit the `/health` and `/ready` endpoints, respectively. The Deployment also includes resource requests and limits to ensure the application has enough CPU and memory resources.

## Performance Optimizations

The application uses atomic operations for incrementing the counter, which is faster and more efficient than using a mutex. However, a version of the counter that uses a mutex (`/statusV2`) is also provided for comparison.

In Go benchmarks, the atomic operations (/status endpoint) outperform mutex-based operations (/statusV2 endpoint) due to their lower overhead. 

However, in a real-world K8s environment, the situation changes. High concurrency and frequent context switching can make atomic operations less efficient due to cache coherency issues. Mutexes, despite their higher overhead, can sometimes be more efficient in such scenarios because they reduce the frequency of cache invalidation across cores.

Because the counter is stored in memory, it will be reset if the application restarts and also if the application is scaled to multiple pods in a Kubernetes environment will not be synchronized. 

Scalability can be achieved not by scaling number of replicas but by scaling the CPU cores of the deployment. If we want to increase number of replicas we need to use a synchronizing mechanism by using a database such as Redis with connection pooling and scripts inside Redis server to reduce number of roundtrips by a factor of at least 3.


**Application deployed to k3s benchmark**

Performed on Intel(R) Core(TM) i9-7900X CPU @ 3.30GHz. https://github.com/tsliwowicz/go-wrk was used to benchmark the application.
```
➜  counter git:(main) go-wrk -c 80 -d 5 http://127.0.0.1/status

Running 5s test @ http://127.0.0.1/status
  80 goroutine(s) running concurrently
119826 requests in 4.896115935s, 20.51MB read
Requests/sec:           24473.69
Transfer/sec:           4.19MB
Avg Req Time:           3.268817ms
Fastest Request:        178.982µs
Slowest Request:        89.773461ms
Number of Errors:       0
```

```
➜  counter git:(main) go-wrk -c 80 -d 5 http://127.0.0.1/statusV2

Running 5s test @ http://127.0.0.1/statusV2
  80 goroutine(s) running concurrently
128802 requests in 4.872268606s, 23.14MB read
Requests/sec:           26435.73
Transfer/sec:           4.75MB
Avg Req Time:           3.026206ms
Fastest Request:        196.253µs
Slowest Request:        107.60546ms
Number of Errors:       0
```

The application uses the `fasthttp` package instead of the standard `net/http` package for handling HTTP requests. `fasthttp` is optimized for high performance and low memory usage.

*Dockerfile* uses a multi-stage build to reduce the size of the Docker image. The final image is only 12.9MB. It also caches the packages to speed up the build process. The packages are downloaded only if the go.mod or go.sum files change.

## TODO
- [x] create a Redis branch and include demo Redis code
- [ ] make a demo with synchronization for many pods using db from the Redis branch
- [ ] run Redis benchmarks and tests too
- [ ] create a docker compose file for running the application and db locally
- [ ] update kubernetes.yaml to deploy Redis too
- [ ] add a Makefile for building and running the application
- [ ] migrate helper CPU limit read functions to a separate package and evaluate if they are still needed
