package modbus

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// runDetectClient opens a client to addr, calls IsModbusDevice for the given unit ID, returns result and error.
func runDetectClient(t *testing.T, addr string, timeout time.Duration, unitId uint8) (bool, error) {
	t.Helper()
	client, err := NewClient(&ClientConfiguration{
		URL:     "tcp://" + addr,
		Timeout: timeout,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = client.Close() }()
	return client.IsModbusDevice(context.Background(), unitId)
}

func TestIsModbusDevice_ValidServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		sock, _ := ln.Accept()
		if sock == nil {
			return
		}
		defer func() { _ = sock.Close() }()
		req := make([]byte, 256)
		n, _ := io.ReadAtLeast(sock, req, 11)
		if n < 11 {
			return
		}
		txid := req[0:2]
		unitId := req[6]
		payload := []byte{
			meiReadDeviceIdentification,
			ReadDeviceIdBasic,
			0x01, 0x00, 0x00,
			0x01,
			0x00, 0x03, 'A', 'B', 'C',
		}
		length := uint16ToBytes(BigEndian, uint16(2+len(payload)))
		frame := append(append(append([]byte{}, txid[0], txid[1], 0x00, 0x00), length...), unitId, fcEncapsulatedInterface)
		_, _ = sock.Write(append(frame, payload...))
	}()

	ok, err := runDetectClient(t, ln.Addr().String(), 2*time.Second, 1)
	if err != nil {
		t.Fatalf("IsModbusDevice: %v", err)
	}
	if !ok {
		t.Fatal("expected true (valid Modbus server)")
	}
}

func TestIsModbusDevice_ExceptionOnly(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		sock, _ := ln.Accept()
		if sock == nil {
			return
		}
		defer func() { _ = sock.Close() }()
		req := make([]byte, 256)
		n, _ := io.ReadAtLeast(sock, req, 11)
		if n < 11 {
			return
		}
		txid := req[0:2]
		unitId := req[6]
		// Exception response: FC | 0x80, exception code 0x02 (Illegal Data Address)
		_, _ = sock.Write([]byte{
			txid[0], txid[1], 0x00, 0x00, 0x00, 0x03,
			unitId,
			fcEncapsulatedInterface | 0x80,
			exIllegalDataAddress,
		})
	}()

	ok, err := runDetectClient(t, ln.Addr().String(), 2*time.Second, 1)
	if err != nil {
		t.Fatalf("IsModbusDevice: %v", err)
	}
	if !ok {
		t.Fatal("expected true (exception response is still valid Modbus)")
	}
}

func TestIsModbusDevice_TCPEcho_NotModbus(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		sock, _ := ln.Accept()
		if sock == nil {
			return
		}
		defer func() { _ = sock.Close() }()
		buf := make([]byte, 256)
		n, _ := sock.Read(buf)
		_, _ = sock.Write(buf[:n])
	}()

	ok, err := runDetectClient(t, ln.Addr().String(), 1*time.Second, 1)
	if err != nil {
		t.Fatalf("IsModbusDevice: %v", err)
	}
	if ok {
		t.Fatal("expected false (echo is not valid Modbus)")
	}
}

func TestIsModbusDevice_RandomGarbage_NotModbus(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		sock, _ := ln.Accept()
		if sock == nil {
			return
		}
		defer func() { _ = sock.Close() }()
		_, _ = io.ReadFull(sock, make([]byte, 11))
		_, _ = sock.Write([]byte{0x12, 0x34, 0x00, 0x00, 0x00, 0x02, 0x01, 0xAB, 0x99})
	}()

	ok, err := runDetectClient(t, ln.Addr().String(), 1*time.Second, 1)
	if err != nil {
		t.Fatalf("IsModbusDevice: %v", err)
	}
	if ok {
		t.Fatal("expected false (garbage response is not valid Modbus)")
	}
}

func TestIsModbusDevice_UnitIDMismatch_NotSuccessForThatUnit(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		sock, _ := ln.Accept()
		if sock == nil {
			return
		}
		defer func() { _ = sock.Close() }()
		for {
			req := make([]byte, 256)
			n, err := io.ReadAtLeast(sock, req, 11)
			if err != nil || n < 11 {
				return
			}
			txid := req[0:2]
			wrongUnitId := byte(0xFF) // respond with different unit ID
			payload := []byte{
				meiReadDeviceIdentification,
				ReadDeviceIdBasic,
				0x01, 0x00, 0x00,
				0x01,
				0x00, 0x03, 'X', 'Y', 'Z',
			}
			length := uint16ToBytes(BigEndian, uint16(2+len(payload)))
			frame := append(append(append([]byte{}, txid[0], txid[1], 0x00, 0x00), length...), wrongUnitId, fcEncapsulatedInterface)
			if _, err := sock.Write(append(frame, payload...)); err != nil {
				return
			}
		}
	}()

	ok, err := runDetectClient(t, ln.Addr().String(), 2*time.Second, 1)
	if err != nil {
		t.Fatalf("IsModbusDevice: %v", err)
	}
	if ok {
		t.Fatal("expected false when response unit ID does not match request")
	}
}

// TestIsModbusDevice_Unit2Responds verifies detection for a specific unit ID (e.g. 2).
// Caller is responsible for sweeping unit IDs; this test just checks unit 2.
func TestIsModbusDevice_Unit2Responds(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		sock, _ := ln.Accept()
		if sock == nil {
			return
		}
		defer func() { _ = sock.Close() }()
		req := make([]byte, 256)
		n, err := io.ReadAtLeast(sock, req, 11)
		if err != nil || n < 11 {
			return
		}
		unitId := req[6]
		if unitId != 2 {
			_, _ = sock.Write([]byte{req[0], req[1], 0x00, 0x00, 0x00, 0x03, 0x99, fcEncapsulatedInterface | 0x80, exIllegalDataAddress})
			return
		}
		txid := req[0:2]
		payload := []byte{
			meiReadDeviceIdentification,
			ReadDeviceIdBasic,
			0x01, 0x00, 0x00,
			0x01,
			0x00, 0x03, 'O', 'K', '2',
		}
		length := uint16ToBytes(BigEndian, uint16(2+len(payload)))
		frame := append(append(append([]byte{}, txid[0], txid[1], 0x00, 0x00), length...), unitId, fcEncapsulatedInterface)
		_, _ = sock.Write(append(frame, payload...))
	}()

	ok, err := runDetectClient(t, ln.Addr().String(), 2*time.Second, 2)
	if err != nil {
		t.Fatalf("IsModbusDevice: %v", err)
	}
	if !ok {
		t.Fatal("expected true when unit ID 2 responds")
	}
}

func TestIsModbusDevice_Unit1_Responds(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		sock, _ := ln.Accept()
		if sock == nil {
			return
		}
		defer func() { _ = sock.Close() }()
		req := make([]byte, 256)
		n, _ := io.ReadAtLeast(sock, req, 11)
		if n < 11 {
			return
		}
		unitId := req[6]
		if unitId != 1 {
			return
		}
		txid := req[0:2]
		payload := []byte{
			meiReadDeviceIdentification,
			ReadDeviceIdBasic,
			0x01, 0x00, 0x00,
			0x01,
			0x00, 0x03, 'D', 'E', 'F',
		}
		length := uint16ToBytes(BigEndian, uint16(2+len(payload)))
		frame := append(append(append([]byte{}, txid[0], txid[1], 0x00, 0x00), length...), unitId, fcEncapsulatedInterface)
		_, _ = sock.Write(append(frame, payload...))
	}()

	ok, err := runDetectClient(t, ln.Addr().String(), 2*time.Second, 1)
	if err != nil {
		t.Fatalf("IsModbusDevice: %v", err)
	}
	if !ok {
		t.Fatal("expected true for unit ID 1")
	}
}

func TestIsModbusDevice_ContextCanceled_ReturnsError(t *testing.T) {
	client, err := NewClient(&ClientConfiguration{URL: "tcp://127.0.0.1:1", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_ = client.Open()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.IsModbusDevice(ctx, 1)
	if err == nil {
		t.Fatal("expected error when context is canceled")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
