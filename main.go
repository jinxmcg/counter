package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/goccy/go-json" // drop in replacement for encoding/json
	"github.com/valyala/fasthttp"
)

var (
	GlobalCtx = context.Background()
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
	LastChanged int64 // Now as int64 - Unix time so we can use atomic
}

// for status atomic is the most performant
var status statusAtomic

var statusV2 ReturnStatus // mutex allows to use the full struct
var statusV2Mutex sync.Mutex

var RedisClient *redis.Client
var LuaScriptSHA1 string

func init() {
	log.Println("Inited")

	// Initialize Redis client
	RedisClient = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379", // or your Redis server address
		Password: "",               // no password set
		DB:       0,                // use default DB
	})

	// Optional: Check the connection
	_, err := RedisClient.Ping(GlobalCtx).Result()
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %s", err)
	}

	// Lua script
	luaScript := `
	-- Lua script to increment the counter, toggle the status, and set the last changed time
	local counter = redis.call('INCR', 'counter')
	local currentStatus = redis.call('GET', 'status')
	if currentStatus == false then
		currentStatus = 0
	else
		currentStatus = tonumber(currentStatus)
	end
	local newStatus = 1 - currentStatus
	redis.call('SET', 'status', newStatus)
	local lastChanged = ARGV[1]
	redis.call('SET', 'lastChanged', lastChanged)
	return {counter, newStatus, lastChanged}`

	// Load Lua script into Redis
	sha1, err := RedisClient.ScriptLoad(GlobalCtx, luaScript).Result()
	if err != nil {
		log.Fatalf("Failed to load Lua script: %s", err)
	}

	LuaScriptSHA1 = sha1
	log.Printf("Redis sha %s", LuaScriptSHA1)

}

func statusHandlerAtomic(ctx *fasthttp.RequestCtx) {

	tmpStatus := atomic.LoadInt64(&status.Status)
	atomic.StoreInt64(&status.Status, 1-tmpStatus)
	atomic.StoreInt64(&status.LastChanged, time.Now().Unix())

	responseStatus := ReturnStatus{
		Counter:     atomic.AddInt64(&status.Counter, 1),
		Status:      (1 - tmpStatus) != 0,
		LastChanged: time.Unix(atomic.LoadInt64(&status.LastChanged), 0),
	}

	ctx.Response.Header.Set("Content-Type", "application/json")
	if err := json.NewEncoder(ctx).Encode(responseStatus); err != nil {
		ctx.Error(err.Error(), fasthttp.StatusInternalServerError)
	}

}

func statusHandlerMutex(ctx *fasthttp.RequestCtx) {
	statusV2Mutex.Lock()
	statusV2.Counter++
	statusV2.Status = !statusV2.Status
	statusV2.LastChanged = time.Now()

	// Convert the status to JSON before unlocking
	response, err := json.Marshal(statusV2)
	statusV2Mutex.Unlock()

	if err != nil {
		// If there is an error in marshaling, handle it here
		ctx.Error(err.Error(), fasthttp.StatusInternalServerError)
		return
	}

	ctx.Response.Header.Set("Content-Type", "application/json")
	// Write the JSON response
	ctx.Response.SetBody(response)
}

func statusHandlerRedis(ctx *fasthttp.RequestCtx) {
	lastChanged := time.Now().Unix()
	result, err := RedisClient.EvalSha(GlobalCtx, LuaScriptSHA1, []string{}, lastChanged).Result()
	if err != nil {
		ctx.Error("Failed to execute Lua script", fasthttp.StatusInternalServerError)
		return
	}

	resultSlice, ok := result.([]interface{})
	if !ok || len(resultSlice) != 3 {
		ctx.Error("Invalid script result", fasthttp.StatusInternalServerError)
		return
	}

	// Parse results
	counter, _ := resultSlice[0].(int64)
	newStatus, _ := resultSlice[1].(int64)
	// Last changed is already known

	// Create response
	responseStatus := ReturnStatus{
		Counter:     counter,
		Status:      newStatus != 0,
		LastChanged: time.Unix(lastChanged, 0),
	}

	ctx.Response.Header.Set("Content-Type", "application/json")
	if err := json.NewEncoder(ctx).Encode(responseStatus); err != nil {
		ctx.Error(err.Error(), fasthttp.StatusInternalServerError)
	}
}

// Liveness probe handler
func livenessHandler(ctx *fasthttp.RequestCtx) {
	ctx.SetStatusCode(fasthttp.StatusOK)
}

// Readiness probe handler
func readinessHandler(ctx *fasthttp.RequestCtx) {
	// Here you can add logic to check if the server is ready to accept traffic
	// For example, check if a database connection is established
	// For simplicity, this example always returns HTTP 200
	ctx.SetStatusCode(fasthttp.StatusOK)
}

func main() {

	// Create a channel to listen for termination signals
	sigChan := make(chan os.Signal, 1)
	// Catch SIGINT (Ctrl+C) and SIGTERM signals
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	h := func(ctx *fasthttp.RequestCtx) {
		switch string(ctx.Path()) {
		case "/status":
			statusHandlerAtomic(ctx)
		case "/statusV2":
			statusHandlerMutex(ctx)
		case "/statusV3":
			statusHandlerRedis(ctx)
		case "/health": // Liveness probe endpoint
			livenessHandler(ctx)
		case "/ready": // Readiness probe endpoint
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
