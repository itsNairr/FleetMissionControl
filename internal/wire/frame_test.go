package wire

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sync"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	fw := NewFrameWriter(&buf)

	if err := fw.WriteFrame(MsgCommand, []byte("hello")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	msgType, payload, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if msgType != MsgCommand || string(payload) != "hello" {
		t.Fatalf("got type=%d payload=%q", msgType, payload)
	}
}

// Many goroutines writing at once must still produce whole, readable frames
func TestConcurrentWritesDoNotInterleave(t *testing.T) {
	var buf bytes.Buffer
	fw := NewFrameWriter(&buf)

	const writers = 50
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			fw.WriteFrame(MsgTelemetry, []byte(fmt.Sprintf("frame-%03d", i)))
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i := 0; i < writers; i++ {
		_, payload, err := ReadFrame(&buf)
		if err != nil {
			t.Fatalf("frame %d unreadable: %v", i, err)
		}
		seen[string(payload)] = true
	}
	if len(seen) != writers {
		t.Fatalf("expected %d distinct frames, got %d", writers, len(seen))
	}
}

func TestRejectsOversizedFrame(t *testing.T) {
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, MaxFrameSize+1)

	if _, _, err := ReadFrame(bytes.NewReader(header)); err == nil {
		t.Fatal("expected oversized frame to be rejected")
	}
}
