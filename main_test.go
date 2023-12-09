package main

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
)

type StatusHandlerFunc func(ctx *fasthttp.RequestCtx)

// test for valid response
func testStatusHandlerResponseAndErrorHandling(t *testing.T, handler func(ctx *fasthttp.RequestCtx, appCtx context.Context)) {

	appCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctx := &fasthttp.RequestCtx{}
	handler(ctx, appCtx)

	var actualStatus ReturnStatus
	err := json.Unmarshal(ctx.Response.Body(), &actualStatus)
	assert.NoError(t, err)

	gracePeriod := int64(1)
	assert.LessOrEqual(t, actualStatus.LastChanged.Unix(), time.Now().Unix()+gracePeriod)
}

// Test for concurrent
func testStatusHandlerConcurrentExecution(t *testing.T, handler func(ctx *fasthttp.RequestCtx, appCtx context.Context)) {
	appCtx, cancel := context.WithCancel(context.Background())

	// Number of concurrent requests
	var numRequests int = 10
	var counterTotal int = numRequests * (numRequests + 1) / 2
	var counters []int64
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(numRequests)

	// Run the handler multiple times in parallel
	for i := 0; i < numRequests; i++ {
		go func() {
			defer wg.Done()

			// Create a request and response context
			ctx := &fasthttp.RequestCtx{}

			// Call the handler
			handler(ctx, appCtx)

			var actualStatus ReturnStatus
			err := json.Unmarshal(ctx.Response.Body(), &actualStatus)
			if err != nil {
				t.Errorf("Failed to unmarshal response: %v", err)
				return
			}
			mu.Lock()
			counters = append(counters, actualStatus.Counter)
			mu.Unlock()
		}()
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// The counter length should be equal to the number of requests
	assert.Equal(t, numRequests, len(counters))

	// The sum of all counters should be equal to the sum of all numbers from 1 to numRequests
	var sum int64
	for _, counter := range counters {
		sum += counter
	}
	assert.Equal(t, int64(counterTotal), sum)
	cancel()
}

// Benchmark for statusHandlerAtomic
func benchmarkStatusHandler(b *testing.B, handler func(ctx *fasthttp.RequestCtx, appCtx context.Context)) {
	appCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctx := &fasthttp.RequestCtx{}
	for i := 0; i < b.N; i++ {
		handler(ctx, appCtx)
	}
}

func benchmarkStatusHandlerParallel(b *testing.B, handler func(ctx *fasthttp.RequestCtx, appCtx context.Context)) {
	appCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b.RunParallel(func(pb *testing.PB) {

		ctx := &fasthttp.RequestCtx{}
		for pb.Next() {
			handler(ctx, appCtx)
		}
	})
}

// Test and benchmark atomic

func TestStatusHandlerAtomic_ConcurrentExecution(t *testing.T) {
	testStatusHandlerConcurrentExecution(t, statusHandlerAtomic)
}

func TestStatusHandlerAtomic_ResponseAndErrorHandling(t *testing.T) {
	testStatusHandlerResponseAndErrorHandling(t, statusHandlerAtomic)
}

func BenchmarkStatusHandlerAtomic(b *testing.B) {
	benchmarkStatusHandler(b, statusHandlerAtomic)
}

func BenchmarkStatusHandlerAtomicParallel(b *testing.B) {
	benchmarkStatusHandlerParallel(b, statusHandlerAtomic)
}

// Test and benchmark mutex

func TestStatusHandlerMutex_ConcurrentExecution(t *testing.T) {
	testStatusHandlerConcurrentExecution(t, statusHandlerMutex)
}

func TestStatusHandlerMutex_ResponseAndErrorHandling(t *testing.T) {
	testStatusHandlerResponseAndErrorHandling(t, statusHandlerMutex)
}

func BenchmarkStatusHandlerMutex(b *testing.B) {
	benchmarkStatusHandler(b, statusHandlerMutex)
}

func BenchmarkStatusHandlerMutexParallel(b *testing.B) {
	benchmarkStatusHandlerParallel(b, statusHandlerMutex)
}

// Test and benchmark channel

func TestStatusHandlerChannel_ConcurrentExecution(t *testing.T) {
	testStatusHandlerConcurrentExecution(t, statusHandlerGoSub)
}

func TestStatusHandlerChannel_ResponseAndErrorHandling(t *testing.T) {
	testStatusHandlerResponseAndErrorHandling(t, statusHandlerGoSub)
}

func BenchmarkStatusHandlerChannel(b *testing.B) {
	benchmarkStatusHandler(b, statusHandlerGoSub)
}

func BenchmarkStatusHandlerChannelParallel(b *testing.B) {
	benchmarkStatusHandlerParallel(b, statusHandlerGoSub)
}
