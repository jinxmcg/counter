package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	// "encoding/json"
	"github.com/goccy/go-json"    // faster drop in replacement for encoding/json
	"github.com/valyala/fasthttp" // fasthttp is a drop in replacement for net/http
)

// json returns a JSON response with the given status code and JSON object.
type ReturnStatus struct {
	Counter     int64     `json:"counter"`
	Status      bool      `json:"status"`
	LastChanged time.Time `json:"lastChanged"` // Now as Unix time
}

// internal for statusHandlerAtomic
type statusAtomic struct {
	Counter     int64
	Status      int64 // 0 = false, 1 = true - so we can use atomic
	LastChanged int64 // Now as int64 - Unix time so we can use atomic operations
}

// for status atomic is the most performant
var status statusAtomic

// statusV2 with a mutex version
var statusV2 ReturnStatus // mutex allows to use the full struct
var statusV2Mutex sync.Mutex

// init is called before main
func init() {

	log.Println("Inited counter app")

	log.Printf("Number of CPU cores available on node: %d\n", runtime.NumCPU())

	limit, err := getCpuLimit()

	if err != nil {
		log.Printf("Error getting CPU limit: %s\n", err)
	} else {
		log.Printf("CPU Limit imposed by k8s (cgroup): %.2f cores\n", limit)
	}

}

// statusHandlerAtomic is an example of using atomic to update the status
func statusHandlerAtomic(ctx *fasthttp.RequestCtx) {

	// atomically update the status
	tmpStatus := atomic.LoadInt64(&status.Status)
	atomic.StoreInt64(&status.Status, 1-tmpStatus)
	atomic.StoreInt64(&status.LastChanged, time.Now().Unix())

	// Create a JSON response
	responseStatus := ReturnStatus{
		Counter:     atomic.AddInt64(&status.Counter, 1),
		Status:      (1 - tmpStatus) != 0,
		LastChanged: time.Unix(atomic.LoadInt64(&status.LastChanged), 0),
	}

	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.Response.Header.Set("Content-Type", "application/json")

	// Write the JSON response to the body
	if err := json.NewEncoder(ctx).Encode(responseStatus); err != nil {
		ctx.Error(err.Error(), fasthttp.StatusInternalServerError)
	}
}

// statusHandlerMutex is an example of using a mutex to update the status
func statusHandlerMutex(ctx *fasthttp.RequestCtx) {
	// Lock the mutex to ensure no other goroutine is updating the status
	statusV2Mutex.Lock()

	statusV2.Counter++
	statusV2.Status = !statusV2.Status
	statusV2.LastChanged = time.Now()

	// Convert the status to JSON before unlocking
	response, err := json.Marshal(statusV2)

	// Unlock the mutex
	statusV2Mutex.Unlock()

	if err != nil {
		ctx.Error(err.Error(), fasthttp.StatusInternalServerError)
		return
	}

	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.Response.Header.Set("Content-Type", "application/json")
	// Write the JSON response
	ctx.Response.SetBody(response)
}

// Liveness probe handler
func livenessHandler(ctx *fasthttp.RequestCtx) {
	ctx.SetStatusCode(fasthttp.StatusOK)
}

// Readiness probe handler
func readinessHandler(ctx *fasthttp.RequestCtx) {
	ctx.SetStatusCode(fasthttp.StatusOK)
}

// kind and k3d do not support cgroup v2 yet
// helper function to log the cpu limit imposed by k8s
func readCgroupFile(path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if scanner.Scan() {
		return strconv.ParseInt(scanner.Text(), 10, 64)
	}

	if err := scanner.Err(); err != nil {
		return 0, err
	}

	return 0, fmt.Errorf("cgroup file is empty! no cpu limits")
}

// readCgroupValue reads a value from a cgroup file.
func readCgroupValue(filePath string) (int64, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, err
	}

	// Convert the cgroup file value to an integer.
	value, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, err
	}

	return value, nil
}

// getCpuLimit calculates the CPU limit as set by k8s cgroups.
func getCpuLimit() (float64, error) {
	quota, err := readCgroupValue("/sys/fs/cgroup/cpu/cpu.cfs_quota_us")
	if err != nil {
		return 0, err
	}

	// If quota is -1, then the cgroup does not limit the CPU usage.
	if quota == -1 {
		return float64(runtime.NumCPU()), nil
	}

	period, err := readCgroupValue("/sys/fs/cgroup/cpu/cpu.cfs_period_us")
	if err != nil {
		return 0, err
	}

	// The CPU limit is calculated as the quota divided by the period.
	return float64(quota) / float64(period), nil
}

func main() {

	// Create a channel to listen for termination signals
	sigChan := make(chan os.Signal, 1)
	// Catch SIGINT (Ctrl+C) and SIGTERM signals
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	h := func(ctx *fasthttp.RequestCtx) {
		switch string(ctx.Path()) {
		case "/status": // atomic example
			statusHandlerAtomic(ctx)
		case "/statusV2": // mutex example
			statusHandlerMutex(ctx)
		case "/health": // K8s Liveness probe endpoint
			livenessHandler(ctx)
		case "/ready": // K8s Readiness probe endpoint
			readinessHandler(ctx)
		default:
			ctx.Error("Unsupported path", fasthttp.StatusNotFound)
		}
	}
	// Start your server in a goroutine
	server := &fasthttp.Server{Handler: h}
	go func() {
		if err := server.ListenAndServe(":8080"); err != nil {
			log.Fatalf("Error in ListenAndServe: %s", err)
		}
	}()

	// Block until a signal is received
	<-sigChan
	log.Println("Shutting down server...")

	shutdownErr := server.Shutdown()
	if shutdownErr != nil {
		log.Fatalf("Error during shutdown: %s", shutdownErr)
	}
	log.Println("Server gracefully stopped")
}
