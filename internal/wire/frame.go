package wire

// Shared TCP framing used by both the gateway and the simulator.
//
// Frame layout: [4-byte Big-Endian length][1-byte message type][protobuf payload]
// The length counts the type byte + payload, so a reader always knows exactly how much to read.

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	"google.golang.org/protobuf/proto"
)

// MessageType tags each frame so one socket can carry several kinds of protobuf messages
type MessageType byte

const (
	MsgTelemetry       MessageType = 1 // Vehicle -> Cloud: pb.VehicleTelemetry
	MsgCommand         MessageType = 2 // Cloud -> Vehicle: pb.VehicleCommand
	MsgCommandResponse MessageType = 3 // Vehicle -> Cloud: pb.CommandResponse
)

// MaxFrameSize caps a single frame so a corrupt/malicious length header can't make us allocate gigabytes
const MaxFrameSize = 1 << 20 // 1 MiB

// ReadFrame blocks until one full frame has arrived and returns its type and protobuf payload
func ReadFrame(r io.Reader) (MessageType, []byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}

	frameLen := binary.BigEndian.Uint32(header)
	if frameLen < 1 || frameLen > MaxFrameSize {
		// We can't find the next frame boundary after a bad header, so the stream is unusable
		return 0, nil, fmt.Errorf("invalid frame length: %d bytes", frameLen)
	}

	body := make([]byte, frameLen)
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}
	return MessageType(body[0]), body[1:], nil
}

// FrameWriter serializes writes to a socket. Several goroutines can share one FrameWriter
// (e.g. many HTTP requests commanding the same car) without their bytes interleaving on the wire.
type FrameWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func NewFrameWriter(w io.Writer) *FrameWriter {
	return &FrameWriter{w: w}
}

// WriteFrame sends header + type + payload as ONE write while holding the lock
func (fw *FrameWriter) WriteFrame(msgType MessageType, payload []byte) error {
	buf := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(buf[:4], uint32(1+len(payload)))
	buf[4] = byte(msgType)
	copy(buf[5:], payload)

	fw.mu.Lock()
	defer fw.mu.Unlock()
	_, err := fw.w.Write(buf)
	return err
}

// WriteMessage marshals a protobuf message and sends it as a single frame
func (fw *FrameWriter) WriteMessage(msgType MessageType, msg proto.Message) error {
	payload, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal %v frame: %w", msgType, err)
	}
	return fw.WriteFrame(msgType, payload)
}
