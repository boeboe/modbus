# Release Notes

## v1.0.3 — 2026-03-01

### New function code support

- **FC08 Diagnostics** — New `Diagnostics(ctx, unitId, subFunction, data)` method. Supports all standard sub-functions (Return Query Data, Restart Communications, diagnostic counters, etc.). New exported types:
  - `DiagnosticSubFunction` — typed `uint16` with 15 named constants (`DiagReturnQueryData`, `DiagRestartCommunications`, …, `DiagClearOverrunCounterAndFlag`) and a `String()` method for logging.
  - `DiagnosticResponse` — holds the echoed `SubFunction` and sub-function-specific `Data`.
- **FC17 Report Server ID** — New `ReportServerId(ctx, unitId)` method. Returns `*ReportServerIdResponse` with `ByteCount` and device-specific `Data` (server ID, run indicator status, optional additional data).
- **RTU transport** — `expectedResponseLenth` updated for FC08 (variable-length, no byte-count field) and FC17 (byte-count-prefixed). New `readVariableLengthDiagnostics` reads FC08 responses by inter-frame silence (t3.5). Exception handling added for FC08|0x80 and FC17|0x80.

### Scanner-grade Modbus device detection

- **FC08 probe first** — `IsModbusDevice` now starts with FC08 Diagnostics (sub-function 0x0000 loopback) before FC43/FC03/FC04/FC01/FC02. FC08 is the safest probe: it does not touch device memory and even an "Illegal Function" exception is proof of Modbus. Normal FC08 echo responses are intentionally ignored (indistinguishable from TCP echo services at the PDU level).
- **Per-probe structural validation** — Each probe now carries its own validate function that checks function-specific payload structure (byte counts, payload lengths) in addition to FC matching. This rejects non-Modbus traffic such as TCP echo services, HTTP on port 502, and random binary protocols.
- **Exception-aware detection** — Any valid Modbus exception (codes 0x01–0x0B) is treated as strong positive detection. The new `isValidModbusException` helper enforces correct FC|0x80, single-byte payload, and valid exception code range.
- **Detection modes** — New `ClientConfiguration.DetectionMode` field with three modes:
  - `DetectAggressive` (default, zero value): FC08 → FC43 → FC03 → FC04 → FC01 → FC02.
  - `DetectStrict`: FC08 → FC43 → FC03. Good speed/coverage balance.
  - `DetectBasic`: FC03 only. Fastest single-probe check.
- **`DetectUnitID(ctx)`** — New method that scans the full unit-ID range (0–255) and returns a slice of **all** responding unit IDs. Scan order: 1, 255, 0, then 2–254. On context cancellation the partial list found so far is returned alongside the error.
- **`FingerprintDevice(ctx, unitId)`** — New method that runs all detection probes and records which FCs the device supports. Returns `*ModbusFingerprint` with per-FC boolean fields. A function is marked supported when the device responds normally or with a non-Illegal-Function exception.
- **`ModbusFingerprint` struct** — New exported type with fields `SupportsFC08`, `SupportsFC43`, `SupportsFC03`, `SupportsFC04`, `SupportsFC01`, `SupportsFC02`.

### Tests

- Rewritten detection tests with proper MBAP frame-level mock servers (`readMBAPFrame`/`writeMBAPException`/`writeMBAPNormal` helpers) that handle the new multi-probe sequence correctly.
- New tests: `TestIsModbusDevice_FC43ValidResponse`, `TestIsModbusDevice_ExceptionOnly`, `TestIsModbusDevice_PersistentTCPEcho_NotModbus`, `TestIsModbusDevice_DetectBasic_FC03Only`, `TestIsModbusDevice_DetectStrict`, `TestDetectUnitID_Found`, `TestDetectUnitID_NotFound`, `TestDetectUnitID_Unit1First`, `TestDetectUnitID_MultipleUnits`, `TestDetectUnitID_HighUnitID`, `TestFingerprintDevice`, `TestFingerprintDevice_ContextCanceled`, `TestIsValidModbusException`.

### Documentation

- **[API.md](API.md)** — New §2.9 Diagnostics and Report Server ID: full signatures, `DiagnosticSubFunction` constants, `DiagnosticResponse`, `ReportServerIdResponse`, examples. §2.8 rewritten: detection modes, probe table with per-FC validation details, `DetectUnitID`, `FingerprintDevice` with `ModbusFingerprint`. `ClientConfiguration` updated with `DetectionMode` field.
- **[README.md](README.md)** — FC table updated with FC08 (`Diagnostics`) and FC17 (`ReportServerId`). Detection paragraph updated for new probe order, modes, `DetectUnitID`, and `FingerprintDevice`.

---

## v1.0.2 — 2026-03-01

### Modbus device detection

- **`IsModbusDevice(ctx, unitId)`** — New method to probe a target and determine whether the given unit ID responds with Modbus-compliant structure (valid MBAP where applicable, normal or exception response). Use after `Open()`; read-only and does not mutate server state.
- **Probe order** — FC43 (Read Device Identification, Basic) → FC03 (Read Holding Registers, addr 0, qty 1) → FC04 (Read Input Registers) → FC01 (Read Coils) → FC02 (Read Discrete Inputs). Returns `true` on first valid response, `false` only after all probes are tried.
- **API consistency** — Takes `unitId uint8` like other client methods; which unit IDs to try (e.g. sweep 1..247) is left to the caller.
- **Tests** — Coverage for valid server, exception-only response, TCP echo (rejected), random garbage (rejected), unit ID mismatch, and context cancellation.

### Documentation

- **[API.md](API.md)** — New §2.8 Modbus device detection: signature, return values, probe order, and example with caller-driven unit ID sweep.
- **[README.md](README.md)** — Device detection line updated for `IsModbusDevice(ctx, unitId)`.

---

## v1.0.1 — 2026-03-01

### Device identification (FC43) improvements

- **`ReadAllDeviceIdentification(ctx, unitId)`** — New convenience method that requests the Extended category (basic + regular + extended) in one call. The device responds with all identification objects it supports; no need to call `ReadDeviceIdentification` multiple times or choose a category.
- **Read device ID constants** — Exported constants for category and access type: `ReadDeviceIdBasic` (0x01), `ReadDeviceIdRegular` (0x02), `ReadDeviceIdExtended` (0x03), `ReadDeviceIdIndividual` (0x04). Use these with `ReadDeviceIdentification` for clearer, self-documenting code.
- **Documentation** — API.md section 2.7 (Device identification) rewritten: describes Basic / Regular / Extended categories, documents `ReadAllDeviceIdentification` and the constants, and adds examples for “read all”, category-only, and individual object access.
- **Tests** — `TestReadAllDeviceIdentification` added; existing FC43 tests updated to use `ReadDeviceIdBasic`.

### Documentation

- **[API.md](API.md)** — FC43 section updated with full device identification API and examples.
- **[README.md](README.md)** — Client function table now lists `ReadAllDeviceIdentification` for FC43/14.

---

## v1.0.0 — 2026-02-27

Initial release.

### Features

#### Context propagation and per-request unit ID
Every client method accepts a `context.Context` as its first argument and a
`unitId uint8` as its second. Cancellation and deadlines are honoured at the
transport level, and the unit / slave ID can be changed on a per-call basis without
mutating shared client state.

#### Function code support

| FC | Name |
|---|---|
| 01 (0x01) | Read Coils — `ReadCoils` |
| 02 (0x02) | Read Discrete Inputs — `ReadDiscreteInputs` |
| 03 (0x03) | Read Holding Registers — `ReadRegister(s)`, typed reads |
| 04 (0x04) | Read Input Registers — `ReadRegister(s)`, typed reads |
| 05 (0x05) | Write Single Coil — `WriteCoil` |
| 06 (0x06) | Write Single Register — `WriteRegister` |
| 15 (0x0F) | Write Multiple Coils — `WriteCoils` |
| 16 (0x10) | Write Multiple Registers — `WriteRegisters` |
| 20 (0x14) | Read File Record — `ReadFileRecords` |
| 21 (0x15) | Write File Record — `WriteFileRecords` |
| 23 (0x17) | Read/Write Multiple Registers — `ReadWriteMultipleRegisters` |
| 24 (0x18) | Read FIFO Queue — `ReadFIFOQueue` |
| 43 (0x2B) | Read Device Identification — `ReadDeviceIdentification` |

#### Typed register reads (FC03/FC04)

All FC03/FC04 reads support configurable byte and word order via `SetEncoding`.

| Method(s) | Return type | Registers per value |
|---|---|---|
| `ReadRegister` / `ReadRegisters` | `uint16` / `[]uint16` | 1 |
| `ReadUint16` / `ReadUint16s` | `uint16` / `[]uint16` | 1 |
| `ReadInt16` / `ReadInt16s` | `int16` / `[]int16` | 1 |
| `ReadUint32` / `ReadUint32s` | `uint32` / `[]uint32` | 2 |
| `ReadInt32` / `ReadInt32s` | `int32` / `[]int32` | 2 |
| `ReadFloat32` / `ReadFloat32s` | `float32` / `[]float32` | 2 |
| `ReadUint48` / `ReadUint48s` | `uint64` / `[]uint64` | 3 |
| `ReadInt48` / `ReadInt48s` | `int64` / `[]int64` | 3 |
| `ReadUint64` / `ReadUint64s` | `uint64` / `[]uint64` | 4 |
| `ReadInt64` / `ReadInt64s` | `int64` / `[]int64` | 4 |
| `ReadFloat64` / `ReadFloat64s` | `float64` / `[]float64` | 4 |
| `ReadAscii` | `string` | `quantity` |
| `ReadAsciiReverse` | `string` | `quantity` |
| `ReadBCD` | `string` | `quantity` |
| `ReadPackedBCD` | `string` | `quantity` |
| `ReadBytes` | `[]byte` | `quantity` |
| `ReadRawBytes` | `[]byte` | `quantity` |

**Signed integers** reinterpret raw two's-complement register data as the
corresponding signed Go type.

**48-bit integers** are returned as `uint64` / `int64` — Go has no native 48-bit
type. Signed values are sign-extended from bit 47 to fill the 64-bit result.

**ASCII** methods read `quantity` registers (2 bytes each) as a character string.
`ReadAscii` places the high byte first; `ReadAsciiReverse` places the low byte first
(byte-swapped per register). Trailing space characters (`0x20`) are stripped.

**BCD** reads each byte as a single decimal digit (0–9).
**Packed BCD** reads each nibble as a single decimal digit; the high nibble is the
more-significant digit within a byte.

#### Connection pool
Set `ClientConfiguration.MaxConns > 1` to enable a bounded connection pool. Multiple
goroutines sharing a single `*ModbusClient` can execute requests concurrently, each on
its own TCP connection. `MinConns` controls the number of connections pre-warmed during
`Open()`. Applies to all TCP-based transports.

#### Retry policy
`ClientConfiguration.RetryPolicy` accepts a `RetryPolicy` implementation. Built-in
options: `NoRetry()` (default) and `ExponentialBackoff(base, maxDelay, maxAttempts)` /
`NewExponentialBackoff(ExponentialBackoffConfig)`. Failed connections are re-dialed
automatically between attempts.

#### Metrics hooks
Implement `ClientMetrics` or `ServerMetrics` and assign to the `Metrics` field of the
respective configuration struct. Callbacks fire on every request outcome: `OnRequest`,
`OnResponse`, `OnError`, and (client only) `OnTimeout`.

#### Structured logging
When `Logger` is `nil`, the library writes through `slog.Default()`. Structured
logging via `NewSlogLogger(slog.Handler)` is first-class. `NewStdLogger` and
`NopLogger` are also available.

### Bug fixes

- Server correctly returns exception code 0x03 (`Illegal Data Value`) for
  out-of-range coil quantities and write values.

### Documentation

- **[API.md](API.md)** — comprehensive public API reference covering all types,
  method signatures, configuration options, and annotated examples.
- **[README.md](README.md)** — transport and function code tables, logging and error
  handling sections, and links to API.md for detailed usage.
