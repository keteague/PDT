package cloudsync

import (
	"context"
	"testing"
	"time"
)

func TestPartSizeFor_StaysAtPartSizeForOrdinaryFiles(t *testing.T) {
	if got := PartSizeFor(1 << 30); got != PartSize { // 1GiB
		t.Errorf("PartSizeFor(1GiB) = %d, want the default %d", got, PartSize)
	}
}

func TestPartSizeFor_GrowsPastMaxPartsForHugeFiles(t *testing.T) {
	// At the default 16MiB part size, maxParts (10,000) caps a multipart
	// upload at ~160GB - a file bigger than that must use a larger part
	// size instead, or PutObjectPart would be asked for a part number S3
	// itself rejects.
	huge := int64(maxParts)*PartSize + 1
	got := PartSizeFor(huge)
	if got <= PartSize {
		t.Fatalf("PartSizeFor(%d) = %d, want something larger than the default %d", huge, got, PartSize)
	}
	if parts := (huge + got - 1) / got; parts > maxParts {
		t.Errorf("PartSizeFor(%d) = %d still needs %d parts, want at most %d", huge, got, parts, maxParts)
	}
}

func TestExpectedPartSize_LastPartGetsTheRemainder(t *testing.T) {
	// A 25MiB file split into 16MiB parts: part 1 is a full 16MiB, part 2
	// is the 9MiB remainder - not another full 16MiB (which would run past
	// the actual file) and not zero (which would silently drop real
	// content).
	const size = 25 * 1024 * 1024
	const partSize = 16 * 1024 * 1024
	totalParts := int((size + partSize - 1) / partSize)

	if got := expectedPartSize(1, size, partSize, totalParts); got != partSize {
		t.Errorf("part 1 size = %d, want the full part size %d", got, partSize)
	}
	if got := expectedPartSize(2, size, partSize, totalParts); got != size-partSize {
		t.Errorf("part 2 (last) size = %d, want the %d-byte remainder", got, size-partSize)
	}
}

func TestExpectedPartSize_OutOfRangeIsNegative(t *testing.T) {
	if got := expectedPartSize(0, 100, 50, 2); got >= 0 {
		t.Errorf("expectedPartSize(0, ...) = %d, want negative (part numbers are 1-based)", got)
	}
	if got := expectedPartSize(3, 100, 50, 2); got >= 0 {
		t.Errorf("expectedPartSize(3, ...) = %d, want negative (only 2 parts exist)", got)
	}
}

// TestPauseGate_BlocksReadsUntilResumed is the direct regression test for
// Pause's own contract: a reader waiting on a paused gate must not proceed
// until SetPaused(false) is called, and must do so promptly once it is.
func TestPauseGate_BlocksReadsUntilResumed(t *testing.T) {
	var g PauseGate
	g.SetPaused(true)

	done := make(chan error, 1)
	go func() { done <- g.wait(context.Background()) }()

	select {
	case <-done:
		t.Fatal("wait() returned before SetPaused(false) was called")
	case <-time.After(100 * time.Millisecond):
	}

	g.SetPaused(false)
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("wait() after resume returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wait() did not return after SetPaused(false)")
	}
}

// TestPauseGate_CancelWinsOverPause guards the other half of Pause/Cancel's
// interaction: a Cancel while paused must not hang forever waiting for a
// Resume that will never come.
func TestPauseGate_CancelWinsOverPause(t *testing.T) {
	var g PauseGate
	g.SetPaused(true)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- g.wait(ctx) }()
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("wait() after cancel returned %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wait() did not return after ctx was canceled")
	}
}

func TestPausingReader_StopsOnAlreadyCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := &pausingReader{ctx: ctx, r: nil}
	if _, err := r.Read(make([]byte, 1)); err != context.Canceled {
		t.Errorf("Read() with an already-canceled context = %v, want context.Canceled", err)
	}
}
