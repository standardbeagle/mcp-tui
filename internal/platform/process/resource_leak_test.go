package process

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestZombieProcessAccumulation tests for zombie process accumulation over time
func TestZombieProcessAccumulation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Zombie process test is Unix-specific")
	}

	t.Run("Long_Running_Session_Zombie_Prevention", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		manager := NewUnixManager(ctx)
		defer manager.Close()

		// Track initial process count
		initialZombieCount := countZombieProcesses(t)

		// Simulate long-running session with many short-lived processes
		const numProcesses = 50
		const batchSize = 10
		
		for batch := 0; batch < numProcesses/batchSize; batch++ {
			var wg sync.WaitGroup
			
			// Start batch of processes
			for i := 0; i < batchSize; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					
					// Start short-lived process
					proc, err := manager.Start(ctx, "echo", []string{fmt.Sprintf("test-%d", id)})
					if err != nil {
						t.Logf("Failed to start process %d: %v", id, err)
						return
					}
					
					// Wait for process to finish
					err = proc.Wait()
					if err != nil {
						t.Logf("Process %d failed: %v", id, err)
					}
				}(batch*batchSize + i)
			}
			
			wg.Wait()
			
			// Allow cleanup between batches
			time.Sleep(200 * time.Millisecond)
			manager.Cleanup()
			
			// Check for zombie accumulation
			currentZombieCount := countZombieProcesses(t)
			zombieIncrease := currentZombieCount - initialZombieCount
			
			if zombieIncrease > 5 {
				t.Errorf("Zombie process accumulation detected: %d zombies after batch %d", 
					zombieIncrease, batch+1)
			}
		}

		// Final cleanup and verification
		manager.Cleanup()
		time.Sleep(500 * time.Millisecond)
		
		finalZombieCount := countZombieProcesses(t)
		finalIncrease := finalZombieCount - initialZombieCount
		
		assert.LessOrEqual(t, finalIncrease, 2, 
			"Should not accumulate more than 2 zombie processes during long session")
	})

	t.Run("Rapid_Process_Creation_And_Termination", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		manager := NewUnixManager(ctx)
		defer manager.Close()

		// Track resource usage
		var processesStarted int32
		var processesFinished int32
		var errors int32

		// Rapid process creation
		const numRoutines = 20
		const processesPerRoutine = 25
		
		var wg sync.WaitGroup
		
		for i := 0; i < numRoutines; i++ {
			wg.Add(1)
			go func(routineID int) {
				defer wg.Done()
				
				for j := 0; j < processesPerRoutine; j++ {
					proc, err := manager.Start(ctx, "sleep", []string{"0.1"})
					if err != nil {
						atomic.AddInt32(&errors, 1)
						continue
					}
					
					atomic.AddInt32(&processesStarted, 1)
					
					// Wait for completion
					go func() {
						proc.Wait()
						atomic.AddInt32(&processesFinished, 1)
					}()
				}
			}(i)
		}

		wg.Wait()
		
		// Wait for all processes to finish
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if atomic.LoadInt32(&processesFinished) >= atomic.LoadInt32(&processesStarted) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		started := atomic.LoadInt32(&processesStarted)
		finished := atomic.LoadInt32(&processesFinished)
		errorCount := atomic.LoadInt32(&errors)

		t.Logf("Rapid process test: %d started, %d finished, %d errors", 
			started, finished, errorCount)

		assert.Greater(t, started, int32(400), "Should start most processes")
		assert.LessOrEqual(t, errorCount, started/10, "Error rate should be low")
		
		// Give cleanup time to work
		time.Sleep(2 * time.Second)
		manager.Cleanup()
	})

	t.Run("Process_Manager_Memory_Leak", func(t *testing.T) {
		// Test for memory leaks in process manager itself
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Measure initial memory
		runtime.GC()
		var initialStats runtime.MemStats
		runtime.ReadMemStats(&initialStats)

		// Create and destroy many process managers
		for i := 0; i < 100; i++ {
			manager := NewUnixManager(ctx)
			
			// Start a few processes
			for j := 0; j < 3; j++ {
				proc, err := manager.Start(ctx, "echo", []string{"test"})
				if err == nil {
					proc.Wait()
				}
			}
			
			manager.Cleanup()
			manager.Close()
		}

		// Force garbage collection
		runtime.GC()
		runtime.GC()
		time.Sleep(100 * time.Millisecond)
		
		var finalStats runtime.MemStats
		runtime.ReadMemStats(&finalStats)

		memoryIncrease := finalStats.Alloc - initialStats.Alloc
		
		t.Logf("Memory usage: initial %d bytes, final %d bytes, increase %d bytes",
			initialStats.Alloc, finalStats.Alloc, memoryIncrease)

		// Allow for reasonable memory increase (100KB)
		assert.Less(t, memoryIncrease, uint64(100*1024), 
			"Process manager should not leak significant memory")
	})
}

// TestGoroutineLeaks tests for goroutine leaks in process management
func TestGoroutineLeaks(t *testing.T) {
	t.Run("Process_Manager_Goroutine_Cleanup", func(t *testing.T) {
		initialGoroutines := runtime.NumGoroutine()

		// Create and destroy multiple process managers
		const numManagers = 10
		
		for i := 0; i < numManagers; i++ {
			ctx, cancel := context.WithCancel(context.Background())
			
			manager := NewUnixManager(ctx)
			
			// Start some processes
			for j := 0; j < 3; j++ {
				proc, err := manager.Start(ctx, "echo", []string{fmt.Sprintf("test-%d-%d", i, j)})
				if err == nil {
					go func() {
						proc.Wait()
					}()
				}
			}
			
			// Cleanup
			cancel()
			manager.Close()
			
			// Allow goroutines to finish
			time.Sleep(50 * time.Millisecond)
		}

		// Give time for cleanup
		time.Sleep(500 * time.Millisecond)
		runtime.GC()

		finalGoroutines := runtime.NumGoroutine()
		goroutineIncrease := finalGoroutines - initialGoroutines

		t.Logf("Goroutines: initial %d, final %d, increase %d",
			initialGoroutines, finalGoroutines, goroutineIncrease)

		// Allow for some reasonable increase (reaper goroutines, etc.)
		assert.LessOrEqual(t, goroutineIncrease, 5, 
			"Should not leak significant number of goroutines")
	})

	t.Run("Concurrent_Process_Operations_Goroutine_Safety", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		manager := NewUnixManager(ctx)
		defer manager.Close()

		initialGoroutines := runtime.NumGoroutine()

		// Concurrent operations
		var wg sync.WaitGroup
		const numOperations = 100

		// Start processes concurrently
		for i := 0; i < numOperations; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				
				proc, err := manager.Start(ctx, "echo", []string{fmt.Sprintf("concurrent-%d", id)})
				if err != nil {
					return
				}
				
				// Randomly kill or wait for some processes
				if id%3 == 0 {
					proc.Kill()
				} else {
					proc.Wait()
				}
			}(i)
		}

		// List processes concurrently
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				manager.List()
			}()
		}

		// Cleanup concurrently
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				manager.Cleanup()
			}()
		}

		wg.Wait()
		
		// Final cleanup
		time.Sleep(500 * time.Millisecond)
		runtime.GC()

		finalGoroutines := runtime.NumGoroutine()
		goroutineIncrease := finalGoroutines - initialGoroutines

		t.Logf("Concurrent operations goroutines: initial %d, final %d, increase %d",
			initialGoroutines, finalGoroutines, goroutineIncrease)

		assert.LessOrEqual(t, goroutineIncrease, 10, 
			"Concurrent operations should not leak many goroutines")
	})
}

// TestFileDescriptorLeaks tests for file descriptor leaks
func TestFileDescriptorLeaks(t *testing.T) {
	t.Run("Process_Creation_FD_Cleanup", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("File descriptor test is Unix-specific")
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		manager := NewUnixManager(ctx)
		defer manager.Close()

		// Track file descriptor usage
		initialFDs := countOpenFileDescriptors(t)

		// Create many processes that open files
		const numProcesses = 50
		var wg sync.WaitGroup

		for i := 0; i < numProcesses; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				// Process that reads from a file (uses FDs)
				proc, err := manager.Start(ctx, "cat", []string{"/proc/self/status"})
				if err != nil {
					t.Logf("Failed to start cat process %d: %v", id, err)
					return
				}

				err = proc.Wait()
				if err != nil {
					t.Logf("Cat process %d failed: %v", id, err)
				}
			}(i)
		}

		wg.Wait()
		manager.Cleanup()

		// Allow system to clean up
		time.Sleep(1 * time.Second)

		finalFDs := countOpenFileDescriptors(t)
		fdIncrease := finalFDs - initialFDs

		t.Logf("File descriptors: initial %d, final %d, increase %d",
			initialFDs, finalFDs, fdIncrease)

		// Allow for some increase but not too much
		assert.LessOrEqual(t, fdIncrease, 10, 
			"Should not leak significant file descriptors")
	})

	t.Run("Process_Pipe_FD_Cleanup", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Pipe FD test is Unix-specific")
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		manager := NewUnixManager(ctx)
		defer manager.Close()

		initialFDs := countOpenFileDescriptors(t)

		// Create processes with pipes
		for i := 0; i < 20; i++ {
			proc, err := manager.Start(ctx, "echo", []string{"test"})
			if err != nil {
				continue
			}

			// Wait and cleanup
			proc.Wait()
		}

		manager.Cleanup()
		time.Sleep(500 * time.Millisecond)

		finalFDs := countOpenFileDescriptors(t)
		fdIncrease := finalFDs - initialFDs

		t.Logf("Pipe FD test: initial %d, final %d, increase %d",
			initialFDs, finalFDs, fdIncrease)

		assert.LessOrEqual(t, fdIncrease, 5, 
			"Should not leak pipe file descriptors")
	})
}

// TestLongRunningProcesses tests resource management for long-running processes
func TestLongRunningProcesses(t *testing.T) {
	t.Run("Multiple_Long_Running_Processes", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		manager := NewUnixManager(ctx)
		defer manager.Close()

		// Start several long-running processes
		const numProcesses = 10
		processes := make([]Process, 0, numProcesses)

		for i := 0; i < numProcesses; i++ {
			proc, err := manager.Start(ctx, "sleep", []string{"30"}) // 30 second sleep
			if err != nil {
				t.Logf("Failed to start long-running process %d: %v", i, err)
				continue
			}
			processes = append(processes, proc)
		}

		t.Logf("Started %d long-running processes", len(processes))

		// Verify they're all running
		runningCount := 0
		for _, proc := range processes {
			if proc.IsRunning() {
				runningCount++
			}
		}

		assert.Equal(t, len(processes), runningCount, 
			"All started processes should be running")

		// Check process list
		managedProcesses := manager.List()
		assert.GreaterOrEqual(t, len(managedProcesses), len(processes),
			"Manager should track all processes")

		// Cleanup all processes
		for _, proc := range processes {
			proc.Kill()
		}

		// Wait for cleanup
		time.Sleep(2 * time.Second)
		manager.Cleanup()

		// Verify cleanup
		finalProcesses := manager.List()
		runningCount = 0
		for _, proc := range finalProcesses {
			if proc.IsRunning() {
				runningCount++
			}
		}

		assert.Equal(t, 0, runningCount, 
			"No processes should be running after cleanup")
	})

	t.Run("Process_Memory_Usage_Monitoring", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		manager := NewUnixManager(ctx)
		defer manager.Close()

		// Start memory-intensive process (if available)
		proc, err := manager.Start(ctx, "yes", []string{}) // Generates continuous output
		if err != nil {
			t.Skipf("Cannot start memory test process: %v", err)
		}

		// Let it run briefly
		time.Sleep(1 * time.Second)

		// Check it's still manageable
		assert.True(t, proc.IsRunning(), "Process should be running")

		// Kill it
		err = proc.Kill()
		assert.NoError(t, err, "Should be able to kill process")

		// Wait for cleanup
		time.Sleep(500 * time.Millisecond)
		assert.False(t, proc.IsRunning(), "Process should be stopped")
	})
}

// Helper functions

func countZombieProcesses(t *testing.T) int {
	if runtime.GOOS == "windows" {
		return 0 // No zombie processes on Windows
	}

	// Count zombie processes by checking /proc
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Logf("Cannot read /proc: %v", err)
		return 0
	}

	zombieCount := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check if it's a PID directory
		name := entry.Name()
		if len(name) == 0 || (name[0] < '0' || name[0] > '9') {
			continue
		}

		// Read status file
		statusPath := fmt.Sprintf("/proc/%s/status", name)
		statusData, err := os.ReadFile(statusPath)
		if err != nil {
			continue
		}

		// Check for zombie state
		if bytes.Contains(statusData, []byte("State:\tZ (zombie)")) {
			zombieCount++
		}
	}

	return zombieCount
}

func countOpenFileDescriptors(t *testing.T) int {
	if runtime.GOOS == "windows" {
		return 0 // Different mechanism on Windows
	}

	// Count open FDs by checking /proc/self/fd
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Logf("Cannot read /proc/self/fd: %v", err)
		return 0
	}

	return len(entries)
}

// TestProcessManagerEdgeCases tests edge cases in process management
func TestProcessManagerEdgeCases(t *testing.T) {
	t.Run("Kill_Already_Dead_Process", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		manager := NewUnixManager(ctx)
		defer manager.Close()

		// Start and wait for process to finish
		proc, err := manager.Start(ctx, "echo", []string{"test"})
		require.NoError(t, err)

		err = proc.Wait()
		assert.NoError(t, err)

		// Try to kill already dead process
		err = proc.Kill()
		assert.NoError(t, err, "Killing dead process should not error")

		assert.False(t, proc.IsRunning(), "Dead process should not be running")
	})

	t.Run("Wait_On_Killed_Process", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		manager := NewUnixManager(ctx)
		defer manager.Close()

		// Start long-running process
		proc, err := manager.Start(ctx, "sleep", []string{"10"})
		require.NoError(t, err)

		// Kill it
		err = proc.Kill()
		assert.NoError(t, err)

		// Wait should complete
		err = proc.Wait()
		// May return error due to signal termination, which is expected
		t.Logf("Wait on killed process returned: %v", err)

		assert.False(t, proc.IsRunning(), "Killed process should not be running")
	})

	t.Run("Manager_Cleanup_With_Mixed_Process_States", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		manager := NewUnixManager(ctx)
		defer manager.Close()

		// Start mix of processes
		var processes []Process

		// Quick finishing process
		proc1, err := manager.Start(ctx, "echo", []string{"quick"})
		if err == nil {
			processes = append(processes, proc1)
			proc1.Wait()
		}

		// Long running process
		proc2, err := manager.Start(ctx, "sleep", []string{"5"})
		if err == nil {
			processes = append(processes, proc2)
		}

		// Process to be killed
		proc3, err := manager.Start(ctx, "sleep", []string{"10"})
		if err == nil {
			processes = append(processes, proc3)
			proc3.Kill()
		}

		// Allow some time for states to settle
		time.Sleep(100 * time.Millisecond)

		// Cleanup should handle all states
		manager.Cleanup()

		// Check final state
		remaining := manager.List()
		runningCount := 0
		for _, proc := range remaining {
			if proc.IsRunning() {
				runningCount++
			}
		}

		t.Logf("After cleanup: %d total processes, %d running", len(remaining), runningCount)

		// Kill any remaining processes
		for _, proc := range remaining {
			if proc.IsRunning() {
				proc.Kill()
			}
		}
	})
}