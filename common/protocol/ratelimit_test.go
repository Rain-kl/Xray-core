package protocol

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestRateLimiterWaitLargerThanBurst(t *testing.T) {
	limiter := NewRateLimiter(1 << 20)
	if limiter == nil {
		t.Fatal("expected limiter")
	}

	start := time.Now()
	if err := limiter.Wait(context.Background(), 128*1024); err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Wait took %s for 128 KiB at 1 MiB/s", elapsed)
	}
}

func TestRateLimiterWaitCancellation(t *testing.T) {
	limiter := NewRateLimiter(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Drain the initial burst so the next wait has to block.
	if err := limiter.Wait(context.Background(), 64*1024); err != nil {
		t.Fatalf("draining initial burst: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- limiter.Wait(ctx, 1) }()

	select {
	case err := <-done:
		t.Fatalf("Wait returned before cancellation: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("Wait error = %v, want context.Canceled", err)
		}
		if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
			t.Fatalf("Wait took %s to observe cancellation", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("Wait did not stop after cancellation")
	}
}

func TestLimiterRegistryAcquireSharesAndReleasesReferences(t *testing.T) {
	registry := newLimiterRegistry()
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel1()
	defer cancel2()

	const email = "shared-limiter@example.com"
	var first, second *RateLimiter
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		first = registry.Acquire(ctx1, email, "uplink", 4096)
	}()
	go func() {
		defer wg.Done()
		second = registry.Acquire(ctx2, email, "uplink", 4096)
	}()
	wg.Wait()
	if first == nil || second == nil || first != second {
		t.Fatal("concurrent acquisitions did not return the shared limiter")
	}
	if got := uint64(first.limiter.Limit()); got != 4096 {
		t.Fatalf("shared limiter rate = %d, want configured rate 4096", got)
	}
	registry.Update(email, "uplink", 8192)
	if got := uint64(first.limiter.Limit()); got != 8192 {
		t.Fatalf("updated shared limiter rate = %d, want 8192", got)
	}

	cancel1()
	waitForRegistryEntries(t, registry, 1)
	if got := len(registry.byUser); got != 1 {
		t.Fatalf("registry entries after one release = %d, want 1", got)
	}
	cancel2()
	waitForRegistryEntries(t, registry, 0)
	if got := len(registry.byUser); got != 0 {
		t.Fatalf("registry entries after all releases = %d, want 0", got)
	}
}

func TestLimiterRegistryKeepsEmailCaseDistinct(t *testing.T) {
	registry := newLimiterRegistry()
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel1()
	defer cancel2()

	first := registry.Acquire(ctx1, "Case@example.com", "uplink", 1024)
	second := registry.Acquire(ctx2, "case@example.com", "uplink", 1024)
	if first == nil || second == nil || first == second {
		t.Fatal("case-distinct emails must have distinct limiters")
	}
}

func TestLimiterRegistryUnlimitedDoesNotAllocate(t *testing.T) {
	registry := newLimiterRegistry()
	limiter := registry.Acquire(context.Background(), "unlimited@example.com", "uplink", 0)
	if limiter != nil {
		t.Fatal("zero rate should not allocate a limiter")
	}
	if got := len(registry.byUser); got != 0 {
		t.Fatalf("registry entries = %d, want 0", got)
	}
	if NewRateLimiter(0) != nil {
		t.Fatal("NewRateLimiter(0) should return nil")
	}
}

func waitForRegistryEntries(t *testing.T, registry *limiterRegistry, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		registry.mu.Lock()
		got := len(registry.byUser)
		registry.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	registry.mu.Lock()
	got := len(registry.byUser)
	registry.mu.Unlock()
	t.Fatalf("registry entries = %d, want %d", got, want)
}

func TestMemoryUserGetLimiterMethodsUseContextAndGroups(t *testing.T) {
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel1()
	defer cancel2()

	first := &MemoryUser{
		Email:                   "group-a@example.com",
		UplinkSpeedLimit:        1024,
		DownlinkSpeedLimit:      2048,
		RateLimitGroup:          "shared",
		DownlinkGroupSpeedLimit: 4096,
	}
	second := &MemoryUser{
		Email:                   "group-b@example.com",
		UplinkSpeedLimit:        1024,
		DownlinkSpeedLimit:      2048,
		RateLimitGroup:          "shared",
		DownlinkGroupSpeedLimit: 4096,
	}

	if first.GetUplinkLimiter(ctx1) == nil || first.GetDownlinkLimiter(ctx1) == nil {
		t.Fatal("expected per-user uplink and downlink limiters")
	}
	group1 := first.GetDownlinkGroupLimiter(ctx1)
	group2 := second.GetDownlinkGroupLimiter(ctx2)
	if group1 == nil || group1 != group2 {
		t.Fatal("users in the same rate-limit group should share a group limiter")
	}

	cancel1()
	cancel2()
}
