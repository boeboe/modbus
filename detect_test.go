package modbus

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// readMBAPFrame reads one complete MBAP frame from conn. Returns the full frame
// (6-byte header + PDU) or an error.
func readMBAPFrame(conn net.Conn) ([]byte, error) {
	header := make([]byte, 6)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	pduLen := int(header[4])<<8 | int(header[5])
	if pduLen < 1 {
		return nil, io.ErrUnexpectedEOF
	}
	body := make([]byte, pduLen)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, err
	}
	return append(header, body...), nil
}

// writeMBAPException writes an MBAP exception frame for the given FC.
func writeMBAPException(conn net.Conn, txid []byte, unitId, fc, exCode byte) error {
	_, err := conn.Write([]byte{
		txid[0], txid[1], 0x00, 0x00, 0x00, 0x03,
		unitId, fc | 0x80, exCode,
	})
	return err
}

// writeMBAPNormal writes an MBAP normal-response frame.
func writeMBAPNormal(conn net.Conn, txid []byte, unitId, fc byte, payload []byte) error {
	length := uint16ToBytes(BigEndian, uint16(2+len(payload)))
	frame := append(append([]byte{txid[0], txid[1], 0x00, 0x00}, length...), unitId, fc)
	frame = append(frame, payload...)
	_, err := conn.Write(frame)
	return err
}

// wrongUnitID returns a unit ID that differs from the requested one AND is
// never 0xFF (the library accepts 0xFF as a gateway source on exceptions).
func wrongUnitID(requested uint8) uint8 {
	return uint8((int(requested) + 1) % 255)
}

// runDetectClient opens a client to addr, calls IsModbusDevice for the given
// unit ID using default detection mode (DetectAggressive).
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

// runDetectClientWithMode opens a client with a specific detection mode.
func runDetectClientWithMode(t *testing.T, addr string, timeout time.Duration, unitId uint8, mode DetectionMode) (bool, error) {
	t.Helper()
	client, err := NewClient(&ClientConfiguration{
		URL:           "tcp://" + addr,
		Timeout:       timeout,
		DetectionMode: mode,
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

// ---------------------------------------------------------------------------
// IsModbusDevice — core detection tests
// ---------------------------------------------------------------------------

// TestIsModbusDevice_ValidServer verifies detection against a mock that responds
// to every FC with an Illegal Function exception. FC08 (first probe) succeeds
// immediately because any valid exception is proof of Modbus.
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
		for {
			frame, err := readMBAPFrame(sock)
			if err != nil {
				return
			}
			txid := frame[0:2]
			unitId := frame[6]
			fc := frame[7]
			_ = writeMBAPException(sock, txid, unitId, fc, exIllegalFunction)
		}
	}()

	ok, err := runDetectClient(t, ln.Addr().String(), 2*time.Second, 1)
	if err != nil {
		t.Fatalf("IsModbusDevice: %v", err)
	}
	if !ok {
		t.Fatal("expected true (valid Modbus server)")
	}
}

// TestIsModbusDevice_FC43ValidResponse verifies detection when the server
// echoes FC08 (ambiguous, skipped) but returns a valid FC43 response.
func TestIsModbusDevice_FC43ValidResponse(t *testing.T) {
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
			frame, err := readMBAPFrame(sock)
			if err != nil {
				return
			}
			txid := frame[0:2]
			unitId := frame[6]
			fc := frame[7]

			switch fc {
			case fcDiagnostics:
				// Echo the full frame back (ambiguous — validator ignores normal FC08).
				_, _ = sock.Write(frame)
			case fcEncapsulatedInterface:
				payload := []byte{
					meiReadDeviceIdentification,
					ReadDeviceIdBasic,
					0x01, 0x00, 0x00,
					0x01,
					0x00, 0x03, 'A', 'B', 'C',
				}
				_ = writeMBAPNormal(sock, txid, unitId, fc, payload)
			default:
				_ = writeMBAPException(sock, txid, unitId, fc, exIllegalFunction)
			}
		}
	}()

	ok, err := runDetectClient(t, ln.Addr().String(), 2*time.Second, 1)
	if err != nil {
		t.Fatalf("IsModbusDevice: %v", err)
	}
	if !ok {
		t.Fatal("expected true (FC43 valid response)")
	}
}

// TestIsModbusDevice_ExceptionOnly verifies that an exception response (other
// than Illegal Function vs. the probe FC) is accepted as positive detection.
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
		for {
			frame, err := readMBAPFrame(sock)
			if err != nil {
				return
			}
			txid := frame[0:2]
			unitId := frame[6]
			fc := frame[7]
			// Illegal Data Address (0x02) — not Illegal Function, still valid Modbus.
			_ = writeMBAPException(sock, txid, unitId, fc, exIllegalDataAddress)
		}
	}()

	ok, err := runDetectClient(t, ln.Addr().String(), 2*time.Second, 1)
	if err != nil {
		t.Fatalf("IsModbusDevice: %v", err)
	}
	if !ok {
		t.Fatal("expected true (exception response is still valid Modbus)")
	}
}

// TestIsModbusDevice_TCPEcho_NotModbus verifies that a single-exchange TCP echo
// service is rejected: FC08 echo is ambiguous (skipped), then the connection
// dies before other probes can complete.
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

// TestIsModbusDevice_PersistentTCPEcho_NotModbus verifies that even a persistent
// TCP echo (stays open, echoes every frame) is rejected: FC08 echo is ambiguous,
// and other probes have structural payload mismatch.
func TestIsModbusDevice_PersistentTCPEcho_NotModbus(t *testing.T) {
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
			frame, err := readMBAPFrame(sock)
			if err != nil {
				return
			}
			_, _ = sock.Write(frame) // echo entire MBAP frame
		}
	}()

	ok, err := runDetectClient(t, ln.Addr().String(), 1*time.Second, 1)
	if err != nil {
		t.Fatalf("IsModbusDevice: %v", err)
	}
	if ok {
		t.Fatal("expected false (persistent echo is not valid Modbus)")
	}
}

// TestIsModbusDevice_RandomGarbage_NotModbus verifies that random binary data is
// rejected.
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
		_, _ = readMBAPFrame(sock)
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

// TestIsModbusDevice_UnitIDMismatch_NotSuccessForThatUnit verifies that a device
// responding with a different unit ID is rejected for the requested unit.
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
			frame, err := readMBAPFrame(sock)
			if err != nil {
				return
			}
			txid := frame[0:2]
			fc := frame[7]
			wrongUnitId := byte(0x99)
			_ = writeMBAPException(sock, txid, wrongUnitId, fc, exIllegalDataAddress)
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

// TestIsModbusDevice_Unit2Responds verifies detection for a specific unit ID.
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
		for {
			frame, err := readMBAPFrame(sock)
			if err != nil {
				return
			}
			txid := frame[0:2]
			unitId := frame[6]
			fc := frame[7]

			if unitId != 2 {
				// Respond with mismatched unit → ErrBadUnitId at transport level.
				_ = writeMBAPException(sock, txid, 0x99, fc, exIllegalDataAddress)
				continue
			}
			// Correct unit: exception proves Modbus.
			_ = writeMBAPException(sock, txid, unitId, fc, exIllegalFunction)
		}
	}()

	ok, err := runDetectClient(t, ln.Addr().String(), 2*time.Second, 2)
	if err != nil {
		t.Fatalf("IsModbusDevice: %v", err)
	}
	if !ok {
		t.Fatal("expected true when unit ID 2 responds")
	}
}

// TestIsModbusDevice_Unit1_Responds verifies detection for unit ID 1.
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
		for {
			frame, err := readMBAPFrame(sock)
			if err != nil {
				return
			}
			txid := frame[0:2]
			unitId := frame[6]
			fc := frame[7]

			if unitId != 1 {
				return // close if wrong unit
			}

			switch fc {
			case fcEncapsulatedInterface:
				payload := []byte{
					meiReadDeviceIdentification,
					ReadDeviceIdBasic,
					0x01, 0x00, 0x00,
					0x01,
					0x00, 0x03, 'D', 'E', 'F',
				}
				_ = writeMBAPNormal(sock, txid, unitId, fc, payload)
			default:
				_ = writeMBAPException(sock, txid, unitId, fc, exIllegalFunction)
			}
		}
	}()

	ok, err := runDetectClient(t, ln.Addr().String(), 2*time.Second, 1)
	if err != nil {
		t.Fatalf("IsModbusDevice: %v", err)
	}
	if !ok {
		t.Fatal("expected true for unit ID 1")
	}
}

// TestIsModbusDevice_ContextCanceled_ReturnsError verifies that a canceled context
// is propagated as an error.
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

// ---------------------------------------------------------------------------
// Detection modes
// ---------------------------------------------------------------------------

// TestIsModbusDevice_DetectBasic_FC03Only verifies that DetectBasic only sends
// FC03. A mock that responds to FC03 normally should be detected.
func TestIsModbusDevice_DetectBasic_FC03Only(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	receivedFCs := make(chan uint8, 10)
	go func() {
		sock, _ := ln.Accept()
		if sock == nil {
			return
		}
		defer func() { _ = sock.Close() }()
		for {
			frame, err := readMBAPFrame(sock)
			if err != nil {
				return
			}
			txid := frame[0:2]
			unitId := frame[6]
			fc := frame[7]
			receivedFCs <- fc

			if fc == fcReadHoldingRegisters {
				// Normal FC03 response: byte-count=2, data=(0x00, 0x00)
				_ = writeMBAPNormal(sock, txid, unitId, fc, []byte{0x02, 0x00, 0x00})
			} else {
				_ = writeMBAPException(sock, txid, unitId, fc, exIllegalFunction)
			}
		}
	}()

	ok, err := runDetectClientWithMode(t, ln.Addr().String(), 2*time.Second, 1, DetectBasic)
	if err != nil {
		t.Fatalf("IsModbusDevice: %v", err)
	}
	if !ok {
		t.Fatal("expected true (FC03 responded)")
	}

	// Verify only FC03 was sent (no FC08, FC43, etc.)
	close(receivedFCs)
	var fcs []uint8
	for fc := range receivedFCs {
		fcs = append(fcs, fc)
	}
	if len(fcs) != 1 || fcs[0] != fcReadHoldingRegisters {
		t.Fatalf("expected only FC03, got %v", fcs)
	}
}

// TestIsModbusDevice_DetectStrict verifies that DetectStrict sends FC08, FC43,
// FC03 (and nothing more).
func TestIsModbusDevice_DetectStrict(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	receivedFCs := make(chan uint8, 10)
	go func() {
		sock, _ := ln.Accept()
		if sock == nil {
			return
		}
		defer func() { _ = sock.Close() }()
		for {
			frame, err := readMBAPFrame(sock)
			if err != nil {
				return
			}
			txid := frame[0:2]
			unitId := frame[6]
			fc := frame[7]
			receivedFCs <- fc

			switch fc {
			case fcDiagnostics:
				// Echo (ambiguous) — validator skips.
				_, _ = sock.Write(frame)
			case fcEncapsulatedInterface:
				// Exception (Illegal Function) — but still valid Modbus.
				_ = writeMBAPException(sock, txid, unitId, fc, exIllegalFunction)
			default:
				_ = writeMBAPException(sock, txid, unitId, fc, exIllegalFunction)
			}
		}
	}()

	ok, err := runDetectClientWithMode(t, ln.Addr().String(), 2*time.Second, 1, DetectStrict)
	if err != nil {
		t.Fatalf("IsModbusDevice: %v", err)
	}
	if !ok {
		t.Fatal("expected true (FC43 exception)")
	}

	// Verify: FC08 (echo, skipped) → FC43 (exception, detected) → stop.
	// Only FC08 and FC43 should have been sent.
	close(receivedFCs)
	var fcs []uint8
	for fc := range receivedFCs {
		fcs = append(fcs, fc)
	}
	if len(fcs) != 2 || fcs[0] != fcDiagnostics || fcs[1] != fcEncapsulatedInterface {
		t.Fatalf("expected [FC08, FC43], got %v", fcs)
	}
}

// ---------------------------------------------------------------------------
// DetectUnitID
// ---------------------------------------------------------------------------

// TestDetectUnitID_Found verifies that DetectUnitID returns all responding
// unit IDs. Here only unit 3 answers with a valid Modbus exception.
func TestDetectUnitID_Found(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	// Mock: only responds to unit 3. All others get wrong-unit-ID response.
	go func() {
		sock, _ := ln.Accept()
		if sock == nil {
			return
		}
		defer func() { _ = sock.Close() }()
		for {
			frame, err := readMBAPFrame(sock)
			if err != nil {
				return
			}
			txid := frame[0:2]
			unitId := frame[6]
			fc := frame[7]

			if unitId != 3 {
				_ = writeMBAPException(sock, txid, wrongUnitID(unitId), fc, exIllegalDataAddress)
				continue
			}
			_ = writeMBAPException(sock, txid, unitId, fc, exIllegalFunction)
		}
	}()

	client, err := NewClient(&ClientConfiguration{
		URL:     "tcp://" + ln.Addr().String(),
		Timeout: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = client.Close() }()

	ids, err := client.DetectUnitID(context.Background())
	if err != nil {
		t.Fatalf("DetectUnitID: %v", err)
	}
	if len(ids) != 1 || ids[0] != 3 {
		t.Fatalf("expected [3], got %v", ids)
	}
}

// TestDetectUnitID_NotFound verifies that DetectUnitID returns an empty slice
// when no unit responds.
func TestDetectUnitID_NotFound(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	// Mock: always responds with wrong unit ID.
	go func() {
		sock, _ := ln.Accept()
		if sock == nil {
			return
		}
		defer func() { _ = sock.Close() }()
		for {
			frame, err := readMBAPFrame(sock)
			if err != nil {
				return
			}
			txid := frame[0:2]
			unitId := frame[6]
			fc := frame[7]
			_ = writeMBAPException(sock, txid, wrongUnitID(unitId), fc, exIllegalDataAddress)
		}
	}()

	client, err := NewClient(&ClientConfiguration{
		URL:     "tcp://" + ln.Addr().String(),
		Timeout: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = client.Close() }()

	ids, err := client.DetectUnitID(context.Background())
	if err != nil {
		t.Fatalf("DetectUnitID: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected empty slice, got %v", ids)
	}
}

// TestDetectUnitID_Unit1First verifies that unit 1 is the first entry in the
// returned list when it responds.
func TestDetectUnitID_Unit1First(t *testing.T) {
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
			frame, err := readMBAPFrame(sock)
			if err != nil {
				return
			}
			txid := frame[0:2]
			unitId := frame[6]
			fc := frame[7]

			if unitId == 1 {
				_ = writeMBAPException(sock, txid, unitId, fc, exIllegalFunction)
			} else {
				_ = writeMBAPException(sock, txid, wrongUnitID(unitId), fc, exIllegalDataAddress)
			}
		}
	}()

	client, err := NewClient(&ClientConfiguration{
		URL:     "tcp://" + ln.Addr().String(),
		Timeout: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = client.Close() }()

	ids, err := client.DetectUnitID(context.Background())
	if err != nil {
		t.Fatalf("DetectUnitID: %v", err)
	}
	if len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("expected [1], got %v", ids)
	}
}

// TestDetectUnitID_MultipleUnits verifies that DetectUnitID returns ALL
// responding unit IDs, including those outside the old 0–10 range.
func TestDetectUnitID_MultipleUnits(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	// Units 1, 20 and 100 respond; all others get wrong-unit-ID.
	respondTo := map[uint8]bool{1: true, 20: true, 100: true}
	go func() {
		sock, _ := ln.Accept()
		if sock == nil {
			return
		}
		defer func() { _ = sock.Close() }()
		for {
			frame, err := readMBAPFrame(sock)
			if err != nil {
				return
			}
			txid := frame[0:2]
			unitId := frame[6]
			fc := frame[7]

			if respondTo[unitId] {
				_ = writeMBAPException(sock, txid, unitId, fc, exIllegalFunction)
			} else {
				_ = writeMBAPException(sock, txid, wrongUnitID(unitId), fc, exIllegalDataAddress)
			}
		}
	}()

	client, err := NewClient(&ClientConfiguration{
		URL:     "tcp://" + ln.Addr().String(),
		Timeout: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = client.Close() }()

	ids, err := client.DetectUnitID(context.Background())
	if err != nil {
		t.Fatalf("DetectUnitID: %v", err)
	}

	// Scan order is 1, 255, 0, 2..254, so result should be [1, 20, 100].
	expected := []uint8{1, 20, 100}
	if len(ids) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, ids)
	}
	for i, id := range expected {
		if ids[i] != id {
			t.Fatalf("expected %v, got %v", expected, ids)
		}
	}
}

// TestDetectUnitID_HighUnitID verifies that unit IDs above 10 (e.g. 21) are
// discovered, covering the full 0–255 range.
func TestDetectUnitID_HighUnitID(t *testing.T) {
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
			frame, err := readMBAPFrame(sock)
			if err != nil {
				return
			}
			txid := frame[0:2]
			unitId := frame[6]
			fc := frame[7]

			if unitId == 21 {
				_ = writeMBAPException(sock, txid, unitId, fc, exIllegalFunction)
			} else {
				_ = writeMBAPException(sock, txid, wrongUnitID(unitId), fc, exIllegalDataAddress)
			}
		}
	}()

	client, err := NewClient(&ClientConfiguration{
		URL:     "tcp://" + ln.Addr().String(),
		Timeout: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = client.Close() }()

	ids, err := client.DetectUnitID(context.Background())
	if err != nil {
		t.Fatalf("DetectUnitID: %v", err)
	}
	if len(ids) != 1 || ids[0] != 21 {
		t.Fatalf("expected [21], got %v", ids)
	}
}

// ---------------------------------------------------------------------------
// FingerprintDevice
// ---------------------------------------------------------------------------

// TestFingerprintDevice verifies that FingerprintDevice correctly records which
// FCs the device supports (non-exception normal response) vs. rejects
// (Illegal Function exception) vs. doesn't respond to (timeout/error).
func TestFingerprintDevice(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	// Mock: supports FC03 and FC01 (normal response), returns Illegal Data
	// Address for FC43, Illegal Function for FC08/FC04/FC02.
	go func() {
		sock, _ := ln.Accept()
		if sock == nil {
			return
		}
		defer func() { _ = sock.Close() }()
		for {
			frame, err := readMBAPFrame(sock)
			if err != nil {
				return
			}
			txid := frame[0:2]
			unitId := frame[6]
			fc := frame[7]

			switch fc {
			case fcReadHoldingRegisters:
				// Normal FC03 response: byte-count=2, data=0x0042
				_ = writeMBAPNormal(sock, txid, unitId, fc, []byte{0x02, 0x00, 0x42})
			case fcReadCoils:
				// Normal FC01 response: byte-count=1, data=0x01
				_ = writeMBAPNormal(sock, txid, unitId, fc, []byte{0x01, 0x01})
			case fcEncapsulatedInterface:
				// Illegal Data Address — device recognises FC43 but rejected the request.
				_ = writeMBAPException(sock, txid, unitId, fc, exIllegalDataAddress)
			default:
				// Illegal Function — FC not supported.
				_ = writeMBAPException(sock, txid, unitId, fc, exIllegalFunction)
			}
		}
	}()

	client, err := NewClient(&ClientConfiguration{
		URL:     "tcp://" + ln.Addr().String(),
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = client.Close() }()

	fp, err := client.FingerprintDevice(context.Background(), 1)
	if err != nil {
		t.Fatalf("FingerprintDevice: %v", err)
	}

	if fp.UnitId != 1 {
		t.Errorf("UnitId: expected 1, got %d", fp.UnitId)
	}
	if fp.SupportsFC08 {
		t.Error("SupportsFC08: expected false (Illegal Function)")
	}
	if !fp.SupportsFC43 {
		t.Error("SupportsFC43: expected true (Illegal Data Address = recognised)")
	}
	if !fp.SupportsFC03 {
		t.Error("SupportsFC03: expected true (normal response)")
	}
	if fp.SupportsFC04 {
		t.Error("SupportsFC04: expected false (Illegal Function)")
	}
	if !fp.SupportsFC01 {
		t.Error("SupportsFC01: expected true (normal response)")
	}
	if fp.SupportsFC02 {
		t.Error("SupportsFC02: expected false (Illegal Function)")
	}
	if fp.SupportsFC11 {
		t.Error("SupportsFC11: expected false (Illegal Function)")
	}
	if fp.SupportsFC18 {
		t.Error("SupportsFC18: expected false (Illegal Function)")
	}
	if fp.SupportsFC20 {
		t.Error("SupportsFC20: expected false (Illegal Function)")
	}
}

// TestFingerprintDevice_ContextCanceled verifies error propagation.
func TestFingerprintDevice_ContextCanceled(t *testing.T) {
	client, err := NewClient(&ClientConfiguration{URL: "tcp://127.0.0.1:1", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_ = client.Open()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.FingerprintDevice(ctx, 1)
	if err == nil {
		t.Fatal("expected error when context is canceled")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// isValidModbusException unit tests
// ---------------------------------------------------------------------------

func TestIsValidModbusException(t *testing.T) {
	tests := []struct {
		name    string
		reqFC   uint8
		resFC   uint8
		payload []byte
		want    bool
	}{
		{"valid exception 0x01", 0x03, 0x83, []byte{0x01}, true},
		{"valid exception 0x02", 0x03, 0x83, []byte{0x02}, true},
		{"valid exception 0x0B", 0x2B, 0xAB, []byte{0x0B}, true},
		{"normal response", 0x03, 0x03, []byte{0x02, 0x00, 0x00}, false},
		{"wrong FC", 0x03, 0x84, []byte{0x01}, false},
		{"empty payload", 0x03, 0x83, []byte{}, false},
		{"extra payload", 0x03, 0x83, []byte{0x01, 0x02}, false},
		{"out of range 0x00", 0x03, 0x83, []byte{0x00}, false},
		{"out of range 0x0C", 0x03, 0x83, []byte{0x0C}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &pdu{functionCode: tt.reqFC}
			res := &pdu{functionCode: tt.resFC, payload: tt.payload}
			if got := isValidModbusException(req, res); got != tt.want {
				t.Errorf("isValidModbusException() = %v, want %v", got, tt.want)
			}
		})
	}
}
