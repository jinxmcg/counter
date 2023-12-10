package main

import (
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
)

type StatusHandlerFunc func(ctx *fasthttp.RequestCtx)

// test for valid response
func testStatusHandlerResponseAndErrorHandling(t *testing.T, handler func(ctx *fasthttp.RequestCtx)) {

	ctx := &fasthttp.RequestCtx{}
	handler(ctx)

	var actualStatus ReturnStatus
	err := json.Unmarshal(ctx.Response.Body(), &actualStatus)
	assert.NoError(t, err)

	gracePeriod := int64(1)
	assert.LessOrEqual(t, actualStatus.LastChanged.Unix(), time.Now().Unix()+gracePeriod)
}

// Test for concurrent
func testStatusHandlerConcurrentExecution(t *testing.T, handler func(ctx *fasthttp.RequestCtx)) {

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
			handler(ctx)

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

}

// Benchmark for statusHandlerAtomic
func benchmarkStatusHandler(b *testing.B, handler func(ctx *fasthttp.RequestCtx)) {

	ctx := &fasthttp.RequestCtx{}
	for i := 0; i < b.N; i++ {
		handler(ctx)
	}
}

func benchmarkStatusHandlerParallel(b *testing.B, handler func(ctx *fasthttp.RequestCtx)) {

	b.RunParallel(func(pb *testing.PB) {

		ctx := &fasthttp.RequestCtx{}
		for pb.Next() {
			handler(ctx)
		}
	})
}

// Test and benchmark atomic
// TestMain can be used for setup and teardown
func TestMain(m *testing.M) {
	code := m.Run() // Run the tests
	// Teardown
	RedisClient.Close()
	os.Exit(code)
}

/*
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
*/
// Test and benchmark redis

func TestStatusHandlerRedis_ResponseAndErrorHandling(t *testing.T) {
	// reset redis counters
	RedisClient.Del(GlobalCtx, "counter", "status", "lastChanged")

	testStatusHandlerResponseAndErrorHandling(t, statusHandlerRedis)
}

func TestStatusHandlerRedis_ConcurrentExecution(t *testing.T) {
	// reset redis counters
	RedisClient.Del(GlobalCtx, "counter", "status", "lastChanged")

	testStatusHandlerConcurrentExecution(t, statusHandlerRedis)
}

func BenchmarkStatusHandlerRedis(b *testing.B) {
	// reset redis counters
	RedisClient.Del(GlobalCtx, "counter", "status", "lastChanged")

	benchmarkStatusHandler(b, statusHandlerRedis)
}

func BenchmarkStatusHandlerRedisParallel(b *testing.B) {
	// reset redis counters
	RedisClient.Del(GlobalCtx, "counter", "status", "lastChanged")

	benchmarkStatusHandlerParallel(b, statusHandlerRedis)
}
