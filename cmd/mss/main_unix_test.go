//go:build !windows

package main

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// TestNoRefreshDoesNotWaitOnTheBuildLock is the point of the flag: while a
// builder holds the write lock, a --no-refresh command answers right away and
// a refreshing one queues behind it — which is the 39 waits a measured
// parallel /mss run spent sitting in.
func TestNoRefreshDoesNotWaitOnTheBuildLock(t *testing.T) {
	root, dir := cmdEnv(t)
	claudeSession(t, root, "s1-aaaa", claudeUser("s1-aaaa", "2026-01-02T03:04:05Z", "lockneedle alpha"))
	mustEnsure(t, dir)

	// Hold the lock the way a running mss index does.
	lock, err := os.OpenFile(dir+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	done := make(chan error, 1)
	go func() {
		done <- searchWithOptions(dir, []string{"--sessions", "--no-refresh", "lockneedle"}, "", true)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("no-refresh search while the lock is held: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("--no-refresh waited on the write lock")
	}

	// The same query without the flag queues, which is the difference the
	// flag exists for — if this half stops blocking, the first half proves
	// nothing.
	queued := make(chan error, 1)
	go func() {
		queued <- searchWithOptions(dir, []string{"--sessions", "lockneedle"}, "", true)
	}()
	select {
	case err := <-queued:
		t.Fatalf("a refreshing search finished while the build lock was held (%v)", err)
	case <-time.After(500 * time.Millisecond):
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-queued:
		if err != nil {
			t.Fatalf("the queued refresh after unlock: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the queued refresh never finished after the lock was released")
	}
}
