package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"go.bug.st/serial"
)

const (
	KISS_FLAG     = 0xC0
	KISS_CMD_DATA = 0x00
)

type KISSConnection interface {
	Write([]byte) (int, error)
	Close() error
}

type TCPKISSConnection struct {
	conn net.Conn
}

func NewTCPKISSConnection(host string, port int) (*TCPKISSConnection, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	log.Printf("Connected to %s via TCP", addr)
	return &TCPKISSConnection{conn: conn}, nil
}

func (t *TCPKISSConnection) Write(b []byte) (int, error) {
	return t.conn.Write(b)
}

func (t *TCPKISSConnection) Close() error {
	return t.conn.Close()
}

type SerialKISSConnection struct {
	port serial.Port
}

func NewSerialKISSConnection(portName string, baud int) (*SerialKISSConnection, error) {
	mode := &serial.Mode{
		BaudRate: baud,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}
	ser, err := serial.Open(portName, mode)
	if err != nil {
		return nil, err
	}
	log.Printf("Opened serial port %s at %d baud", portName, baud)
	return &SerialKISSConnection{port: ser}, nil
}

func (s *SerialKISSConnection) Write(b []byte) (int, error) {
	return s.port.Write(b)
}

func (s *SerialKISSConnection) Close() error {
	return s.port.Close()
}

func escapeData(data []byte) []byte {
	var buf bytes.Buffer
	for _, b := range data {
		if b == KISS_FLAG {
			buf.WriteByte(0xDB)
			buf.WriteByte(0xDC)
		} else if b == 0xDB {
			buf.WriteByte(0xDB)
			buf.WriteByte(0xDD)
		} else {
			buf.WriteByte(b)
		}
	}
	return buf.Bytes()
}

func buildKISSFrameCmd(cmd byte, payload []byte) []byte {
	escaped := escapeData(payload)
	frame := []byte{KISS_FLAG, cmd}
	frame = append(frame, escaped...)
	frame = append(frame, KISS_FLAG)
	return frame
}

func main() {
	// Custom usage function with detailed help message.
	flag.Usage = func() {
		usageText := `Usage of setmode:
  -connection string
        Connection type: tcp or serial (default "serial")
  -host string
        TCP host (if connection is tcp) (default "127.0.0.1")
  -mode int
        Mode value to set (required)
  -port int
        TCP port (if connection is tcp) (default 5001)
  -serial-port string
        Serial port (if connection is serial) (default "/dev/ttyACM0")
  -write
        If set, writes the mode to memory

Modern Modes:
  Mode    DIP    Baud   bps   Mod    Proto    Usage     BW
  1       0001   19200  19200 4FSK   IL2Pc    FM        25k
  3       0011   9600   9600  4FSK   IL2Pc    FM        12.5k
  2       0010   9600   9600  GFSK   IL2Pc    FM        25k
  5       0101   3600   3600  QPSK   IL2Pc    FM        12.5k
  11      1011   1200   2400  QPSK   IL2Pc    SSB/FM    2.4kHz
  10      1010   1200   1200  BPSK   IL2Pc    SSB/FM    2.4kHz
  9       1001   300    600   QPSK   IL2Pc    SSB       500Hz
  8       1000   300    300   BPSK   IL2Pc    SSB       500Hz
  14      1110   300    300   AFSK   IL2Pc    SSB       500Hz

Legacy Modes:
  Mode    DIP    Baud   bps   Mod    Proto    Superseded by        Usage  BW
  0       0000   9600   9600  GFSK   AX.25    9600 GFSK IL2P       FM     25k
  4       0100   4800   4800  GFSK   IL2Pc    9600 4FSK IL2Pc      FM     12.5k
  7       0111   1200   1200  AFSK   IL2P     4800 GFSK IL2Pc      FM     12.5k
  6       0110   1200   1200  AFSK   AX.25    1200 AFSK IL2P       FM     12.5k
  12      1100   300    300   AFSK   AX.25    300 AFSK IL2P        SSB    500Hz
  13      1101   300    300   AFSK   IL2P     300 AFSK IL2Pc       SSB    500Hz

Before running this utility ensure the mode DIP switches are all set to ON (1111) and the firmware is at least v41.

Example, set mode to 3 without permanently storing to memory:

./setmode -mode 3

More info at https://wiki.oarc.uk/packet:ninotnc

`
		fmt.Fprint(os.Stderr, usageText)
	}

	if len(os.Args) == 1 {
		flag.Usage()
		os.Exit(0)
	}

	connectionType := flag.String("connection", "serial", "Connection type: tcp or serial")
	host := flag.String("host", "127.0.0.1", "TCP host (if connection is tcp)")
	port := flag.Int("port", 5001, "TCP port (if connection is tcp)")
	serialPort := flag.String("serial-port", "/dev/ttyACM0", "Serial port (if connection is serial)")
	modeArg := flag.Int("mode", 0, "Mode value to set (required)")
	write := flag.Bool("write", false, "If set, permanently store the mode (does not add 16 to the provided mode)")
	flag.Parse()

	if *modeArg == 0 {
		log.Fatal("The -mode flag is required and must be non-zero.")
	}

	var modeValue byte
	if *write {
		modeValue = byte(*modeArg)
	} else {
		modeValue = byte(*modeArg + 16)
	}

	packet := buildKISSFrameCmd(0x06, []byte{modeValue})

	var conn KISSConnection
	var err error
	ct := strings.ToLower(*connectionType)
	if ct == "tcp" {
		conn, err = NewTCPKISSConnection(*host, *port)
	} else if ct == "serial" {
		if *serialPort == "" {
			log.Fatal("The -serial-port flag is required for serial connection.")
		}
		conn, err = NewSerialKISSConnection(*serialPort, 57600)
	} else {
		log.Fatalf("Unknown connection type: %s", *connectionType)
	}
	if err != nil {
		log.Fatalf("Error establishing connection: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write(packet)
	if err != nil {
		log.Fatalf("Error sending mode command: %v", err)
	}

	if *write {
		log.Printf("Sent KISS packet to set mode to %d (%d)", modeValue, *modeArg)
	} else {
		log.Printf("Sent KISS packet to set mode to %d (%d + 16)", modeValue, *modeArg)
	}

	time.Sleep(500 * time.Millisecond)
}
