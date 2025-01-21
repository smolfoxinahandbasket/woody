package main

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

var networkLock sync.Mutex
var defaultSlotForTargetMap = map[string]uint16{
	"pcsx2": 28011, // based on https://github.com/PCSX2/pcsx2/blob/4dafea65f256f2fa342f5bd33c624bbc14e6e0f0/pcsx2/PINE.h#L13
	"rpcs3": 28012, // based on https://github.com/RPCS3/rpcs3/blob/92d07072915b99917892dd7833c06eb44a09e234/rpcs3/Emu/IPC_config.h#L8
}

type PineConnection struct {
	network string
	address string
}

// setting slot to zero results in the default slot for the given target being used
// note that the connection isn't
func NewPineConnection(target string, slot uint16) (*PineConnection, error) {
	if target == "" {
		return nil, errors.New("empty string provided for target name when creating PINE connection")
	}
	if slot == 0 {
		var slotFound bool
		slot, slotFound = defaultSlotForTargetMap[target]
		if !slotFound {
			targetNames := maps.Keys(defaultSlotForTargetMap)
			errorMessage := fmt.Sprintf("unknown target \"%v\" when finding slot for target. Supported values are %v", target, targetNames)
			return nil, errors.New(errorMessage)
		}
	}

	switch runtime.GOOS {
	case "windows":
		address := fmt.Sprintf(":%v", slot)
		return &PineConnection{network: "tcp", address: address}, nil
	case "darwin", "linux":
		address := findSocketPath(target, slot)
		return &PineConnection{network: "unix", address: address}, nil
	default:
		return nil, errors.New("unknown operating system when creating PineConnection")
	}
}

// based on the standard at https://projects.govanify.com/govanify/pine/-/blob/3298a7dac42b2385a378720bf705fcd6a2eb553f/standard/draft.dtd
func findSocketPath(target string, slot uint16) string {
	var dir string
	if runtime.GOOS == "linux" && os.Getenv("XDG_RUNTIME_DIR") != "" {
		dir = os.Getenv("XDG_RUNTIME_DIR")
	} else if runtime.GOOS == "darwin" && os.Getenv("TMPDIR") != "" {
		dir = os.Getenv("TMPDIR")
	} else {
		dir = "/tmp"
	}

	// We always append the slot number here, despite the standard saying that it isn't used when the default slot is used.
	// Then, when making or testing the connection, we can check for the socket having the number or not.
	// This results in more flexibility in naming that we'll accept.
	// (keep in mind that the emulator may not be running at this point, so we can't just check which file exists and stick with it)
	file := target + ".sock." + fmt.Sprint(slot)

	return dir + "/" + file
}

func (connection PineConnection) TestConnection() error {
	conn, err := connection.connect()
	if err != nil {
		return err
	}
	return conn.Close()
}

func (connection PineConnection) connect() (net.Conn, error) {
	conn, err := net.Dial(connection.network, connection.address)
	if err == nil {
		return conn, nil
	}

	if connection.network == "unix" {
		// we need to check for a file with the slot number appended and one without (since we can't count on emulators always using one)
		addressWithoutSlot := connection.address[:strings.LastIndex(connection.address, ".")]
		conn, err := net.Dial(connection.network, addressWithoutSlot)
		if err == nil {
			return conn, nil
		}
	}

	return nil, errors.New(fmt.Sprintf("could not connect to PINE at \"%v\"", connection.address))
}

func (connection PineConnection) Send(bytes []byte) ([]byte, error) {
	// let's make sure that only one thing is sent at a time
	networkLock.Lock()
	defer networkLock.Unlock()

	logger.Info("bytes for the Request", "bytes", hex.Dump(bytes))

	conn, err := connection.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// 15 seconds seems like a long time but we need some safe timeout
	err = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if err != nil {
		return nil, err
	}

	logger.Info("writing the Request bytes")
	writer := bufio.NewWriter(conn)
	_, err = writer.Write(bytes)
	if err != nil {
		return nil, err
	}
	err = writer.Flush()
	if err != nil {
		return nil, err
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		conn.(*net.UnixConn).CloseWrite()
	} else {
		conn.(*net.TCPConn).CloseWrite()
	}
	logger.Info("Request bytes written")

	// read the entire response
	readBytes, err := io.ReadAll(conn)
	logger.Info("bytes for the Answer", "bytes", hex.Dump(readBytes))

	return readBytes, err
}

type PineRequest interface {
	toBytes() ([]byte, error)
}

type PineAnswer interface {
	fromBytes([]byte) error
}

// events are unimplemented in the standard right now
// and batch messages are unimplemented because we don't need them

type PineRead8Request struct {
	address uint32
}

func (request PineRead8Request) toBytes() ([]byte, error) {
	// 4 bytes for the length
	// 1 byte for the opcode
	// 4 bytes for the address
	bytes := make([]byte, 9)
	binary.LittleEndian.PutUint32(bytes[0:], uint32(len(bytes)))
	bytes[4] = 0
	binary.LittleEndian.PutUint32(bytes[5:], request.address)
	return bytes, nil
}

type PineRead8Answer struct {
	resultCode  uint8
	memoryValue uint8
}

func (answer *PineRead8Answer) fromBytes(bytes []byte) error {
	// 4 bytes for the length
	// 1 byte for the result code
	// 1 byte for the value
	length := binary.LittleEndian.Uint32(bytes[0:])
	if length != 5 && length != 6 {
		logger.Error("unexpected length (length != 5 && length != 6)", "length", length, "bytes", hex.Dump(bytes))
		return errors.New("length of bytes for PineStatusAnswer != 5 or 6")
	}
	logger.Debug("answer bytes", "bytes", hex.Dump(bytes))
	answer.resultCode = bytes[4]
	answer.memoryValue = bytes[5]
	return nil
}

type PineRead16Request struct {
	address uint32
}

func (request PineRead16Request) toBytes() ([]byte, error) {
	// 4 bytes for the length
	// 1 byte for the opcode
	// 4 bytes for the address
	bytes := make([]byte, 9)
	binary.LittleEndian.PutUint32(bytes[0:], uint32(len(bytes)))
	bytes[4] = 1
	binary.LittleEndian.PutUint32(bytes[5:], request.address)
	return bytes, nil
}

type PineRead16Answer struct {
	resultCode  uint8
	memoryValue uint16
}

func (answer *PineRead16Answer) fromBytes(bytes []byte) error {
	// 4 bytes for the length
	// 1 byte for the result code
	// 2 bytes for the value
	length := binary.LittleEndian.Uint32(bytes[0:])
	if length != 5 && length != 7 {
		logger.Error("unexpected length (length != 5 && length != 7)", "length", length, "bytes", hex.Dump(bytes))
		return errors.New("length of bytes for PineStatusAnswer != 5 or 7")
	}
	logger.Debug("answer bytes", "bytes", hex.Dump(bytes))
	answer.resultCode = bytes[4]
	answer.memoryValue = binary.LittleEndian.Uint16(bytes[5:])
	return nil
}

type PineRead32Request struct {
	address uint32
}

func (request PineRead32Request) toBytes() ([]byte, error) {
	// 4 bytes for the length
	// 1 byte for the opcode
	// 4 bytes for the address
	bytes := make([]byte, 9)
	binary.LittleEndian.PutUint32(bytes[0:], uint32(len(bytes)))
	bytes[4] = 2
	binary.LittleEndian.PutUint32(bytes[5:], request.address)
	return bytes, nil
}

type PineRead32Answer struct {
	resultCode  uint8
	memoryValue uint32
}

func (answer *PineRead32Answer) fromBytes(bytes []byte) error {
	// 4 bytes for the length
	// 1 byte for the result code
	// 4 bytes for the value
	length := binary.LittleEndian.Uint32(bytes[0:])
	if length != 5 && length != 9 {
		logger.Error("unexpected length (length != 5 && length != 9)", "length", length, "bytes", hex.Dump(bytes))
		return errors.New("length of bytes for PineStatusAnswer != 5 or 9")
	}
	logger.Debug("answer bytes", "bytes", hex.Dump(bytes))
	answer.resultCode = bytes[4]
	answer.memoryValue = binary.LittleEndian.Uint32(bytes[5:])
	return nil
}

type PineRead64Request struct {
	address uint32
}

func (request PineRead64Request) toBytes() ([]byte, error) {
	// 4 bytes for the length
	// 1 byte for the opcode
	// 4 bytes for the address
	bytes := make([]byte, 9)
	binary.LittleEndian.PutUint32(bytes[0:], uint32(len(bytes)))
	bytes[4] = 3
	binary.LittleEndian.PutUint32(bytes[5:], request.address)
	return bytes, nil
}

type PineRead64Answer struct {
	resultCode  uint8
	memoryValue uint64
}

func (answer *PineRead64Answer) fromBytes(bytes []byte) error {
	// 4 bytes for the length
	// 1 byte for the result code
	// 8 bytes for the value
	length := binary.LittleEndian.Uint32(bytes[0:])
	if length != 5 && length != 13 {
		logger.Error("unexpected length (length != 5 && length != 13)", "length", length, "bytes", hex.Dump(bytes))
		return errors.New("length of bytes for PineStatusAnswer != 5 or 13")
	}
	logger.Debug("answer bytes", "bytes", hex.Dump(bytes))
	answer.resultCode = bytes[4]
	answer.memoryValue = binary.LittleEndian.Uint64(bytes[5:])
	return nil
}

type PineWrite8Request struct {
	address uint32
	data    uint8
}

func (request PineWrite8Request) toBytes() ([]byte, error) {
	// 4 bytes for the length
	// 1 byte for the opcode
	// 4 bytes for the address
	// 1 byte for the data
	bytes := make([]byte, 10)
	binary.LittleEndian.PutUint32(bytes[0:], uint32(len(bytes)))
	bytes[4] = 4
	binary.LittleEndian.PutUint32(bytes[5:], request.address)
	bytes[9] = request.data
	return bytes, nil
}

type PineWrite8Answer struct {
	resultCode uint8
}

func (answer *PineWrite8Answer) fromBytes(bytes []byte) error {
	// 4 bytes for the length
	// 1 byte for the result code
	length := binary.LittleEndian.Uint32(bytes[0:])
	if length != 5 {
		logger.Error("unexpected length (length != 5)", "length", length, "bytes", hex.Dump(bytes))
		return errors.New("length of bytes for PineStatusAnswer != 5")
	}
	logger.Debug("answer bytes", "bytes", hex.Dump(bytes))
	answer.resultCode = bytes[4]
	return nil
}

type PineWrite16Request struct {
	address uint32
	data    uint16
}

func (request PineWrite16Request) toBytes() ([]byte, error) {
	// 4 bytes for the length
	// 1 byte for the opcode
	// 4 bytes for the address
	// 2 bytes for the data
	bytes := make([]byte, 11)
	binary.LittleEndian.PutUint32(bytes[0:], uint32(len(bytes)))
	bytes[4] = 5
	binary.LittleEndian.PutUint32(bytes[5:], request.address)
	binary.LittleEndian.PutUint16(bytes[9:], request.data)
	return bytes, nil
}

type PineWrite16Answer struct {
	resultCode uint8
}

func (answer *PineWrite16Answer) fromBytes(bytes []byte) error {
	// 4 bytes for the length
	// 1 byte for the result code
	length := binary.LittleEndian.Uint32(bytes[0:])
	if length != 5 {
		logger.Error("unexpected length (length != 5)", "length", length, "bytes", hex.Dump(bytes))
		return errors.New("length of bytes for PineStatusAnswer != 5")
	}
	logger.Debug("answer bytes", "bytes", hex.Dump(bytes))
	answer.resultCode = bytes[4]
	return nil
}

type PineWrite32Request struct {
	address uint32
	data    uint32
}

func (request PineWrite32Request) toBytes() ([]byte, error) {
	// 4 bytes for the length
	// 1 byte for the opcode
	// 4 bytes for the address
	// 4 bytes for the data
	bytes := make([]byte, 13)
	binary.LittleEndian.PutUint32(bytes[0:], uint32(len(bytes)))
	bytes[4] = 6
	binary.LittleEndian.PutUint32(bytes[5:], request.address)
	binary.LittleEndian.PutUint32(bytes[9:], request.data)
	return bytes, nil
}

type PineWrite32Answer struct {
	resultCode uint8
}

func (answer *PineWrite32Answer) fromBytes(bytes []byte) error {
	// 4 bytes for the length
	// 1 byte for the result code
	length := binary.LittleEndian.Uint32(bytes[0:])
	if length != 5 {
		logger.Error("unexpected length (length != 5)", "length", length, "bytes", hex.Dump(bytes))
		return errors.New("length of bytes for PineStatusAnswer != 5")
	}
	logger.Debug("answer bytes", "bytes", hex.Dump(bytes))
	answer.resultCode = bytes[4]
	return nil
}

type PineWrite64Request struct {
	address uint32
	data    uint64
}

func (request PineWrite64Request) toBytes() ([]byte, error) {
	// 4 bytes for the length
	// 1 byte for the opcode
	// 4 bytes for the address
	// 8 bytes for the data
	bytes := make([]byte, 17)
	binary.LittleEndian.PutUint32(bytes[0:], uint32(len(bytes)))
	bytes[4] = 7
	binary.LittleEndian.PutUint32(bytes[5:], request.address)
	binary.LittleEndian.PutUint64(bytes[9:], request.data)
	return bytes, nil
}

type PineWrite64Answer struct {
	resultCode uint8
}

func (answer *PineWrite64Answer) fromBytes(bytes []byte) error {
	// 4 bytes for the length
	// 1 byte for the result code
	length := binary.LittleEndian.Uint32(bytes[0:])
	if length != 5 {
		logger.Error("unexpected length (length != 5)", "length", length, "bytes", hex.Dump(bytes))
		return errors.New("length of bytes for PineStatusAnswer != 5")
	}
	logger.Debug("answer bytes", "bytes", hex.Dump(bytes))
	answer.resultCode = bytes[4]
	return nil
}

type PineVersionRequest struct{}

func (request PineVersionRequest) toBytes() ([]byte, error) {
	// 4 bytes for the length
	// 1 byte for the opcode
	bytes := make([]byte, 5)
	binary.LittleEndian.PutUint32(bytes[0:], uint32(len(bytes)))
	bytes[4] = 8
	return bytes, nil
}

type PineVersionAnswer struct {
	resultCode uint8
	version    string
}

func (answer *PineVersionAnswer) fromBytes(bytes []byte) error {
	// 4 bytes for the length
	// 1 byte for the result code
	// 4 bytes for the length of the version string
	// remaining bytes for the version string
	length := binary.LittleEndian.Uint32(bytes[0:])
	if length < 10 {
		return errors.New("bytes for PineVersionAnswer < 10")
	}
	logger.Debug("version string", "bytes", hex.Dump(bytes))
	answer.resultCode = bytes[4]
	versionStringLength := binary.LittleEndian.Uint32(bytes[5:])
	answer.version = ""
	if versionStringLength > 0 {
		answer.version = string(bytes[9 : 9+versionStringLength])
		answer.version, _ = strings.CutSuffix(answer.version, "\u0000")
	}
	return nil
}

type PineSaveStateRequest struct {
	slot uint8
}

func (request PineSaveStateRequest) toBytes() ([]byte, error) {
	// 4 bytes for the length
	// 1 byte for the opcode
	// 1 byte for the slot
	bytes := make([]byte, 6)
	binary.LittleEndian.PutUint32(bytes[0:], uint32(len(bytes)))
	bytes[4] = 9
	bytes[5] = request.slot
	return bytes, nil
}

type PineSaveStateAnswer struct {
	resultCode uint8
}

func (answer *PineSaveStateAnswer) fromBytes(bytes []byte) error {
	// 4 bytes for the length
	// 1 byte for the result code
	length := binary.LittleEndian.Uint32(bytes[0:])
	if length != 5 {
		logger.Error("unexpected length (length != 5)", "length", length, "bytes", hex.Dump(bytes))
		return errors.New("length of bytes for PineSaveStateAnswer != 5")
	}
	logger.Debug("answer bytes", "bytes", hex.Dump(bytes))
	answer.resultCode = bytes[4]
	return nil
}

type PineLoadStateRequest struct {
	slot uint8
}

func (request PineLoadStateRequest) toBytes() ([]byte, error) {
	// 4 bytes for the length
	// 1 byte for the opcode
	// 1 byte for the slot
	bytes := make([]byte, 6)
	binary.LittleEndian.PutUint32(bytes[0:], uint32(len(bytes)))
	bytes[4] = 10
	bytes[5] = request.slot
	return bytes, nil
}

type PineLoadStateAnswer struct {
	resultCode uint8
}

func (answer *PineLoadStateAnswer) fromBytes(bytes []byte) error {
	// 4 bytes for the length
	// 1 byte for the result code
	length := binary.LittleEndian.Uint32(bytes[0:])
	if length != 5 {
		logger.Error("unexpected length (length != 5)", "length", length, "bytes", hex.Dump(bytes))
		return errors.New("length of bytes for PineLoadStateAnswer != 5")
	}
	logger.Debug("answer bytes", "bytes", hex.Dump(bytes))
	answer.resultCode = bytes[4]
	return nil
}

type PineTitleRequest struct{}

func (request PineTitleRequest) toBytes() ([]byte, error) {
	// 4 bytes for the length
	// 1 byte for the opcode
	bytes := make([]byte, 5)
	binary.LittleEndian.PutUint32(bytes[0:], uint32(len(bytes)))
	bytes[4] = 11
	return bytes, nil
}

type PineTitleAnswer struct {
	resultCode uint8
	title      string
}

func (answer *PineTitleAnswer) fromBytes(bytes []byte) error {
	// 4 bytes for the length
	// 1 byte for the result code
	// 4 bytes for the length of the version string
	// remaining bytes for the version string
	length := binary.LittleEndian.Uint32(bytes[0:])
	if length < 10 {
		return errors.New("bytes for PineVersionAnswer < 10")
	}
	logger.Debug("version string", "bytes", hex.Dump(bytes))
	answer.resultCode = bytes[4]
	versionStringLength := binary.LittleEndian.Uint32(bytes[5:])
	answer.title = ""
	if versionStringLength > 0 {
		answer.title = string(bytes[9 : 9+versionStringLength])
		answer.title, _ = strings.CutSuffix(answer.title, "\u0000")
	}
	return nil
}

type PineIDRequest struct{}

func (request PineIDRequest) toBytes() ([]byte, error) {
	// 4 bytes for the length
	// 1 byte for the opcode
	bytes := make([]byte, 5)
	binary.LittleEndian.PutUint32(bytes[0:], uint32(len(bytes)))
	bytes[4] = 12
	return bytes, nil
}

type PineIDAnswer struct {
	resultCode uint8
	id         string
}

func (answer *PineIDAnswer) fromBytes(bytes []byte) error {
	// 4 bytes for the length
	// 1 byte for the result code
	// 4 bytes for the length of the version string
	// remaining bytes for the version string
	length := binary.LittleEndian.Uint32(bytes[0:])
	if length < 10 {
		return errors.New("bytes for PineVersionAnswer < 10")
	}
	logger.Debug("version string", "bytes", hex.Dump(bytes))
	answer.resultCode = bytes[4]
	versionStringLength := binary.LittleEndian.Uint32(bytes[5:])
	answer.id = ""
	if versionStringLength > 0 {
		answer.id = string(bytes[9 : 9+versionStringLength])
		answer.id, _ = strings.CutSuffix(answer.id, "\u0000")
	}
	return nil
}

type PineUUIDRequest struct{}

func (request PineUUIDRequest) toBytes() ([]byte, error) {
	// 4 bytes for the length
	// 1 byte for the opcode
	bytes := make([]byte, 5)
	binary.LittleEndian.PutUint32(bytes[0:], uint32(len(bytes)))
	bytes[4] = 13
	return bytes, nil
}

type PineUUIDAnswer struct {
	resultCode uint8
	uuid       string
}

func (answer *PineUUIDAnswer) fromBytes(bytes []byte) error {
	// 4 bytes for the length
	// 1 byte for the result code
	// 4 bytes for the length of the version string
	// remaining bytes for the version string
	length := binary.LittleEndian.Uint32(bytes[0:])
	if length < 10 {
		return errors.New("bytes for PineVersionAnswer < 10")
	}
	logger.Debug("version string", "bytes", hex.Dump(bytes))
	answer.resultCode = bytes[4]
	versionStringLength := binary.LittleEndian.Uint32(bytes[5:])
	answer.uuid = ""
	if versionStringLength > 0 {
		answer.uuid = string(bytes[9 : 9+versionStringLength])
		answer.uuid, _ = strings.CutSuffix(answer.uuid, "\u0000")
	}
	return nil
}

type PineGameVersionRequest struct{}

func (request PineGameVersionRequest) toBytes() ([]byte, error) {
	// 4 bytes for the length
	// 1 byte for the opcode
	bytes := make([]byte, 5)
	binary.LittleEndian.PutUint32(bytes[0:], uint32(len(bytes)))
	bytes[4] = 14
	return bytes, nil
}

type PineGameVersionAnswer struct {
	resultCode  uint8
	gameVersion string
}

func (answer *PineGameVersionAnswer) fromBytes(bytes []byte) error {
	// 4 bytes for the length
	// 1 byte for the result code
	// 4 bytes for the length of the version string
	// remaining bytes for the version string
	length := binary.LittleEndian.Uint32(bytes[0:])
	if length < 10 {
		return errors.New("bytes for PineVersionAnswer < 10")
	}
	logger.Debug("version string", "bytes", hex.Dump(bytes))
	answer.resultCode = bytes[4]
	versionStringLength := binary.LittleEndian.Uint32(bytes[5:])
	answer.gameVersion = ""
	if versionStringLength > 0 {
		answer.gameVersion = string(bytes[9 : 9+versionStringLength])
		answer.gameVersion, _ = strings.CutSuffix(answer.gameVersion, "\u0000")
	}
	return nil
}

type PineStatusRequest struct{}

func (request PineStatusRequest) toBytes() ([]byte, error) {
	// 4 bytes for the length
	// 1 byte for the opcode
	bytes := make([]byte, 5)
	binary.LittleEndian.PutUint32(bytes[0:], uint32(len(bytes)))
	bytes[4] = 15
	return bytes, nil
}

type PineStatusAnswer struct {
	resultCode uint8
	status     uint32
}

func (answer *PineStatusAnswer) fromBytes(bytes []byte) error {
	// 4 bytes for the length
	// 1 byte for the result code
	// 4 bytes for the status
	// remaining bytes for the version string
	length := binary.LittleEndian.Uint32(bytes[0:])
	if length != 5 && length != 9 {
		logger.Error("unexpected length (length != 5 && length != 9)", "length", length, "bytes", hex.Dump(bytes))
		return errors.New("length of bytes for PineStatusAnswer != 5 or 9")
	}
	logger.Debug("status answer bytes", "length", length, "bytes", hex.Dump(bytes))
	// var answer *PineStatusAnswer = &PineStatusAnswer{}
	answer.resultCode = bytes[4]
	if length == 9 {
		answer.status = binary.LittleEndian.Uint32(bytes[5:])
	}
	logger.Debug("status answer", "answer.status", answer.status)
	return nil
}
