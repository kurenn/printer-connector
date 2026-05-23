package util

import (
	"context"
	"testing"
	"time"
)

const jitterMax = 1.25 // Next() multiplies by at most 0.75 + 0.5

func TestBackoff_StaysWithinMax(t *testing.T) {
	max := 100 * time.Millisecond
	bo := NewBackoff(10*time.Millisecond, max)
	ceiling := time.Duration(float64(max) * jitterMax)
	for i := 0; i < 20; i++ {
		d := bo.Next()
		if d <= 0 || d > ceiling {
			t.Fatalf("iteration %d: backoff %v outside (0, %v]", i, d, ceiling)
		}
	}
}

func TestBackoff_Reset(t *testing.T) {
	min := 10 * time.Millisecond
	bo := NewBackoff(min, time.Second)
	for i := 0; i < 5; i++ {
		bo.Next()
	}
	bo.Reset()
	d := bo.Next()
	ceiling := time.Duration(float64(min) * jitterMax)
	if d > ceiling {
		t.Fatalf("after Reset, first backoff %v exceeds %v", d, ceiling)
	}
}

func TestWait_Elapses(t *testing.T) {
	start := time.Now()
	if err := Wait(context.Background(), 20*time.Millisecond); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
		t.Fatalf("returned after %v, expected to wait ~20ms", elapsed)
	}
}

func TestWait_ReturnsErrWhenAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Wait(ctx, time.Hour); err == nil {
		t.Fatal("expected context error, got nil")
	}
}

func TestWait_ReturnsPromptlyOnCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := Wait(ctx, time.Hour); err == nil {
		t.Fatal("expected context error, got nil")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("did not return promptly on cancel: waited %v", elapsed)
	}
}
