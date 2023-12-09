package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/goccy/go-json" // drop in replacement for encoding/json
	"github.com/valyala/fasthttp"
)

var (
	ctx = context.Background()
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

type statusV3Message struct{}

var statusV3UpdateChannel = make(chan statusV3Message) // once channel for all requests
var statusV3 ReturnStatus

func init() {
	fmt.Println("Inited")
}

func statusHandlerAtomic(ctx *fasthttp.RequestCtx, appCtx context.Context) {

	select {
	case <-appCtx.Done():
		// The context has been cancelled, stop processing
		return
	default:
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

}

func statusHandlerMutex(ctx *fasthttp.RequestCtx, appCtx context.Context) {
	select {
	case <-appCtx.Done():
		// The context has been cancelled, stop processing
		return
	default:
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

}

func statusHandlerGoSub(ctx *fasthttp.RequestCtx, appCtx context.Context) {

	select {
	case <-appCtx.Done():
		// The context has been cancelled, stop processing
		return
	default:
		// The context has not been cancelled, continue processing
		statusV3UpdateChannel <- statusV3Message{}
		ctx.Response.Header.Set("Content-Type", "application/json")
		if err := json.NewEncoder(ctx).Encode(statusV3); err != nil {
			ctx.Error(err.Error(), fasthttp.StatusInternalServerError)
		}
	}
}

func statusHandlerRedis(ctx *fasthttp.RequestCtx, appCtx context.Context) {
	select {
	case <-appCtx.Done():
		// The context has been cancelled, stop processing
		return
	default:
		// The context has not been cancelled, continue processing
	}
}

func statusV3Update(ctx context.Context) {
	for {
		select {
		case <-statusV3UpdateChannel:
			statusV3.Counter++
			statusV3.Status = !statusV3.Status
			statusV3.LastChanged = time.Now()
		case <-ctx.Done():
			return // Exit the goroutine when the context is cancelled
		}
	}
}

func main() {

	// Create a channel to listen for termination signals
	sigChan := make(chan os.Signal, 1)
	// Catch SIGINT (Ctrl+C) and SIGTERM signals
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	appCtx, cancelCtx := context.WithCancel(context.Background())
	go statusV3Update(appCtx)

	h := func(ctx *fasthttp.RequestCtx) {
		switch string(ctx.Path()) {
		case "/status":
			statusHandlerAtomic(ctx, appCtx)
		case "/statusV2":
			statusHandlerMutex(ctx, appCtx)
		case "/statusV3":
			statusHandlerGoSub(ctx, appCtx)
		case "/statusV4":
			statusHandlerRedis(ctx, appCtx)
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
	cancelCtx()
	log.Println("Server gracefully stopped")

}
