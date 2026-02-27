package modbus

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestReadDeviceIdentification(t *testing.T) {
	var err error
	var ln net.Listener
	var client *ModbusClient
	var di *DeviceIdentification

	ln, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test listener: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		var err error
		var sock net.Conn
		var req []byte
		var payload []byte
		var txid []byte
		var unitId byte

		sock, err = ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = sock.Close() }()

		req = make([]byte, 11)
		_, err = io.ReadFull(sock, req)
		if err != nil {
			return
		}

		if req[2] != 0x00 || req[3] != 0x00 ||
			req[4] != 0x00 || req[5] != 0x05 ||
			req[7] != fcEncapsulatedInterface ||
			req[8] != meiReadDeviceIdentification ||
			req[9] != 0x01 || req[10] != 0x00 {
			return
		}

		txid = req[0:2]
		unitId = req[6]

		payload = []byte{
			meiReadDeviceIdentification,
			0x01,
			0x01,
			0x00,
			0x00,
			0x02,
			0x00, 0x03, 'A', 'C', 'M',
			0x01, 0x05, 'P', '1', '2', '3', '4',
		}

		_, _ = sock.Write(append([]byte{
			txid[0], txid[1],
			0x00, 0x00,
			0x00, byte(2 + len(payload)),
			unitId,
			fcEncapsulatedInterface,
		}, payload...))
	}()

	client, err = NewClient(&ClientConfiguration{
		URL:     "tcp://" + ln.Addr().String(),
		Timeout: 1 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	err = client.Open()
	if err != nil {
		t.Fatalf("failed to open client: %v", err)
	}
	defer func() { _ = client.Close() }()

	di, err = client.ReadDeviceIdentification(context.Background(), 1, 0x01, 0x00)
	if err != nil {
		t.Fatalf("ReadDeviceIdentification() should have succeeded, got: %v", err)
	}

	if di.ReadDeviceIdCode != 0x01 || di.ConformityLevel != 0x01 ||
		di.MoreFollows != 0x00 || di.NextObjectId != 0x00 {
		t.Fatalf("unexpected FC43 header fields: %#v", di)
	}

	if len(di.Objects) != 2 {
		t.Fatalf("expected 2 objects, got: %v", len(di.Objects))
	}

	if di.Objects[0].Id != 0x00 || di.Objects[0].Name != "VendorName" || di.Objects[0].Value != "ACM" {
		t.Fatalf("unexpected first object: %#v", di.Objects[0])
	}

	if di.Objects[1].Id != 0x01 || di.Objects[1].Name != "ProductCode" || di.Objects[1].Value != "P1234" {
		t.Fatalf("unexpected second object: %#v", di.Objects[1])
	}
}

func TestReadDeviceIdentificationException(t *testing.T) {
	var err error
	var ln net.Listener
	var client *ModbusClient

	ln, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test listener: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		var err error
		var sock net.Conn
		var req []byte
		var txid []byte
		var unitId byte

		sock, err = ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = sock.Close() }()

		req = make([]byte, 11)
		_, err = io.ReadFull(sock, req)
		if err != nil {
			return
		}

		txid = req[0:2]
		unitId = req[6]

		_, _ = sock.Write([]byte{
			txid[0], txid[1],
			0x00, 0x00,
			0x00, 0x03,
			unitId,
			(fcEncapsulatedInterface | 0x80),
			exIllegalFunction,
		})
	}()

	client, err = NewClient(&ClientConfiguration{
		URL:     "tcp://" + ln.Addr().String(),
		Timeout: 1 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	err = client.Open()
	if err != nil {
		t.Fatalf("failed to open client: %v", err)
	}
	defer func() { _ = client.Close() }()

	_, err = client.ReadDeviceIdentification(context.Background(), 1, 0x01, 0x00)
	if !errors.Is(err, ErrIllegalFunction) {
		t.Fatalf("expected %v, got: %v", ErrIllegalFunction, err)
	}
}

func TestReadDeviceIdentificationRejectsUnexpectedCode(t *testing.T) {
	var err error
	var client *ModbusClient

	client, err = NewClient(&ClientConfiguration{URL: "tcp://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	_, err = client.ReadDeviceIdentification(context.Background(), 1, 0x00, 0x00)
	if err != ErrUnexpectedParameters {
		t.Fatalf("expected %v, got: %v", ErrUnexpectedParameters, err)
	}
}
