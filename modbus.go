package modbus

import (
	"errors"
	"fmt"
)

type pdu struct {
	unitId       uint8
	functionCode uint8
	payload      []byte
}

// ExceptionError is returned by client methods when the remote device responds
// with a Modbus exception. It gives callers structured access to the raw function
// and exception codes while remaining compatible with errors.Is / errors.As:
//
//	var excErr *modbus.ExceptionError
//	if errors.As(err, &excErr) {
//	    fmt.Printf("fc=0x%02x exception=0x%02x\n", excErr.FunctionCode, excErr.ExceptionCode)
//	}
//	if errors.Is(err, modbus.ErrIllegalDataAddress) { ... }
type ExceptionError struct {
	FunctionCode  byte  // originating Modbus function code (high bit clear)
	ExceptionCode byte  // raw Modbus exception code (0x01–0x0b)
	Sentinel      error // one of the Err* sentinel variables below
}

func (e *ExceptionError) Error() string        { return e.Sentinel.Error() }
func (e *ExceptionError) Unwrap() error        { return e.Sentinel }
func (e *ExceptionError) Is(target error) bool { return target == e.Sentinel }

const (
	// coils.
	fcReadCoils          uint8 = 0x01
	fcWriteSingleCoil    uint8 = 0x05
	fcWriteMultipleCoils uint8 = 0x0f

	// discrete inputs.
	fcReadDiscreteInputs uint8 = 0x02

	// 16-bit input/holding registers.
	fcReadHoldingRegisters       uint8 = 0x03
	fcReadInputRegisters         uint8 = 0x04
	fcWriteSingleRegister        uint8 = 0x06
	fcWriteMultipleRegisters     uint8 = 0x10
	fcMaskWriteRegister          uint8 = 0x16
	fcReadWriteMultipleRegisters uint8 = 0x17
	fcReadFifoQueue              uint8 = 0x18

	// diagnostics and server ID (serial line / common).
	fcDiagnostics    uint8 = 0x08
	fcReportServerId uint8 = 0x11

	// file access.
	fcReadFileRecord        uint8 = 0x14
	fcWriteFileRecord       uint8 = 0x15
	fcEncapsulatedInterface uint8 = 0x2b

	// encapsulated interface (FC43) MEI types.
	meiReadDeviceIdentification uint8 = 0x0e
)

// Read Device ID codes for FC43 (Read Device Identification).
// Use with ReadDeviceIdentification; ReadAllDeviceIdentification uses Extended internally.
const (
	ReadDeviceIdBasic      = 0x01 // Basic: VendorName, ProductCode, MajorMinorRevision (mandatory)
	ReadDeviceIdRegular    = 0x02 // Regular: Basic + VendorUrl, ProductName, ModelName, UserApplicationName
	ReadDeviceIdExtended   = 0x03 // Extended: Regular + private/vendor objects (0x80–0xFF)
	ReadDeviceIdIndividual = 0x04 // Individual: request a single object by objectId
)

const (
	// exception codes.
	exIllegalFunction         uint8 = 0x01
	exIllegalDataAddress      uint8 = 0x02
	exIllegalDataValue        uint8 = 0x03
	exServerDeviceFailure     uint8 = 0x04
	exAcknowledge             uint8 = 0x05
	exServerDeviceBusy        uint8 = 0x06
	exMemoryParityError       uint8 = 0x08
	exGWPathUnavailable       uint8 = 0x0a
	exGWTargetFailedToRespond uint8 = 0x0b

	// Modbus protocol limits used for coil/register access validation.
	maxReadCoils      = 2000 // FC01/FC02: max coils per read request
	maxWriteCoils     = 1968 // FC15:      max coils per write request (0x7b0)
	maxReadRegisters  = 125  // FC03/FC04: max registers per read request
	maxWriteRegisters = 123  // FC16:      max registers per write request
	maxRWReadRegs     = 125  // FC23:      max registers to read  (0x7D)
	maxRWWriteRegs    = 121  // FC23:      max registers to write (0x79)
	maxFIFOCount      = 31   // FC24:      max register count returned in FIFO queue
	maxFileByteCount  = 0xF5 // FC20:      max byte count in read-file-record request
	maxFileReqDataLen = 0xFB // FC21:      max request data length in write-file-record
)

// Sentinel error variables. Use errors.Is to test for a specific condition;
// use errors.As with *ExceptionError to inspect Modbus exception details.
var (
	ErrConfigurationError      = errors.New("modbus: configuration error")
	ErrRequestTimedOut         = errors.New("modbus: request timed out")
	ErrIllegalFunction         = errors.New("modbus: illegal function")
	ErrIllegalDataAddress      = errors.New("modbus: illegal data address")
	ErrIllegalDataValue        = errors.New("modbus: illegal data value")
	ErrServerDeviceFailure     = errors.New("modbus: server device failure")
	ErrAcknowledge             = errors.New("modbus: acknowledge")
	ErrServerDeviceBusy        = errors.New("modbus: server device busy")
	ErrMemoryParityError       = errors.New("modbus: memory parity error")
	ErrGWPathUnavailable       = errors.New("modbus: gateway path unavailable")
	ErrGWTargetFailedToRespond = errors.New("modbus: gateway target failed to respond")
	ErrBadCRC                  = errors.New("modbus: bad crc")
	ErrShortFrame              = errors.New("modbus: short frame")
	ErrProtocolError           = errors.New("modbus: protocol error")
	ErrBadUnitId               = errors.New("modbus: bad unit id")
	ErrBadTransactionId        = errors.New("modbus: bad transaction id")
	ErrUnknownProtocolId       = errors.New("modbus: unknown protocol identifier")
	ErrUnexpectedParameters    = errors.New("modbus: unexpected parameters")
)

// mapExceptionCodeToError converts a Modbus exception code into an *ExceptionError
// that wraps the appropriate sentinel so both errors.Is and errors.As work for
// callers.
func mapExceptionCodeToError(functionCode uint8, exceptionCode uint8) (err error) {
	var sentinel error

	switch exceptionCode {
	case exIllegalFunction:
		sentinel = ErrIllegalFunction
	case exIllegalDataAddress:
		sentinel = ErrIllegalDataAddress
	case exIllegalDataValue:
		sentinel = ErrIllegalDataValue
	case exServerDeviceFailure:
		sentinel = ErrServerDeviceFailure
	case exAcknowledge:
		sentinel = ErrAcknowledge
	case exMemoryParityError:
		sentinel = ErrMemoryParityError
	case exServerDeviceBusy:
		sentinel = ErrServerDeviceBusy
	case exGWPathUnavailable:
		sentinel = ErrGWPathUnavailable
	case exGWTargetFailedToRespond:
		sentinel = ErrGWTargetFailedToRespond
	default:
		err = fmt.Errorf("modbus: unknown exception code (0x%02x)", exceptionCode)
		return
	}

	err = &ExceptionError{
		FunctionCode:  functionCode,
		ExceptionCode: exceptionCode,
		Sentinel:      sentinel,
	}

	return
}

// mapErrorToExceptionCode converts an error into a Modbus exception code for use
// in server responses. It uses errors.Is so wrapped errors are handled correctly.
func mapErrorToExceptionCode(err error) (exceptionCode uint8) {
	switch {
	case errors.Is(err, ErrIllegalFunction):
		exceptionCode = exIllegalFunction
	case errors.Is(err, ErrIllegalDataAddress):
		exceptionCode = exIllegalDataAddress
	case errors.Is(err, ErrIllegalDataValue):
		exceptionCode = exIllegalDataValue
	case errors.Is(err, ErrServerDeviceFailure):
		exceptionCode = exServerDeviceFailure
	case errors.Is(err, ErrAcknowledge):
		exceptionCode = exAcknowledge
	case errors.Is(err, ErrMemoryParityError):
		exceptionCode = exMemoryParityError
	case errors.Is(err, ErrServerDeviceBusy):
		exceptionCode = exServerDeviceBusy
	case errors.Is(err, ErrGWPathUnavailable):
		exceptionCode = exGWPathUnavailable
	case errors.Is(err, ErrGWTargetFailedToRespond):
		exceptionCode = exGWTargetFailedToRespond
	default:
		exceptionCode = exServerDeviceFailure
	}

	return
}
