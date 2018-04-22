package main

import "bytes"
import "crc"
import "crypto/md5"
import "encoding/binary"
import influxdb "github.com/influxdata/influxdb/client/v2"
import "github.com/spf13/viper"
import "io"
import "log"
import "math"
import "net"
import "os"
import "os/signal"
import "syscall"
import "time"

type SplinkHeader struct {
	Op uint8
	Len uint8
	Addr uint32
}
const SplinkHeaderLen = 8

type SplinkOperation byte
const (
	SplinkReadOp SplinkOperation = 'Q'
	SplinkWriteOp SplinkOperation = 'W'
)

const (
	SplinkAddrComport          = 0x0000A000
	SplinkAddrDisconnect       = 0x0000A00D
	SplinkAddrChallenge        = 0x001F0000
	SplinkAddrChallengeSuccess = 0x001F0010
)

func main() {
	// read configuration data
	viper.SetDefault("port", "3000")
	viper.SetDefault("influx_host", "localhost")
	viper.SetDefault("influx_port", "8089") // default port for influx udp protocol
	viper.SetDefault("password", "Selectronic SP PRO")
	
	viper.SetConfigName("config")
	viper.AddConfigPath("/etc/splink")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		log.Fatal("Error reading config: ", err)
	}

	// connect to serial host
	serialHost := viper.GetString("host")
	serialPort := viper.GetString("port")
	conn, err := net.Dial("tcp", serialHost + ":" + serialPort)
	if err != nil {
		log.Fatal("Error connecting to serial host: ", err)
	}
	defer conn.Close()

	// authenticate to inverter
	comPort := splinkAuthenticate(conn, viper.GetString("password"))
	log.Println("Using comport ", comPort)
	defer splinkDisconnect(conn, comPort)

	// deauthenticate on sigint and any other exit
	sigChan := make(chan os.Signal, 2)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		splinkDisconnect(conn, comPort)
		os.Exit(1)
	}()

	// make influxdb client
	influxHost := viper.GetString("influx_host")
	influxPort := viper.GetString("influx_port")
	influxConf := influxdb.UDPConfig{Addr: influxHost + ":" + influxPort}
	influxConn, err := influxdb.NewUDPClient(influxConf)
	if err != nil {
		log.Println("Influx error: ", err)
		return
	}
	defer influxConn.Close()

	influxBatch, _ := influxdb.NewBatchPoints(influxdb.BatchPointsConfig{
		Precision: "s",
	})

	// ticker to request new data every 15 seconds
	ticker := time.NewTicker(15 * time.Second)
	go func() {
		for _ = range ticker.C {
			splinkRequestData(conn, influxBatch)
			influxConn.Write(influxBatch)
		}
	}()

	select {}
}

func splinkRequestData(conn net.Conn, bp influxdb.BatchPoints) {
	val := splinkRead(conn, 0x0000A058, 2)
	var signedPower int32
	binary.Read(bytes.NewReader(val), binary.LittleEndian, &signedPower)
	
	tags := map[string]string {
		"type": "power",
	}
	fields := map[string]interface{}{
		"signed_power": float64(signedPower) * (4798.0 * (2000.0 / 26214400.0 / 1000.0)),
	}
	pt, err := influxdb.NewPoint("power", tags, fields, time.Now())
	if err != nil {
		log.Println("Error: ", err)
	}
	bp.AddPoint(pt)
}

func splinkAuthenticate(conn net.Conn, password string) uint16 {
	comPort := binary.LittleEndian.Uint16(splinkRead(conn, SplinkAddrComport, 1))

	if comPort == math.MaxUint16 {
		log.Println("unauthenticated")
		challenge := splinkRead(conn, SplinkAddrChallenge, 8)

		authResponse := append(challenge, []byte(padRight(password, 32, " "))...)
		authHash := md5.Sum(authResponse)
		splinkWrite(conn, SplinkAddrChallenge, makeUint16Slice(authHash[:]))

		challengeSuccess := binary.LittleEndian.Uint16(splinkRead(conn, SplinkAddrChallengeSuccess, 1))

		if challengeSuccess != 1 {
			log.Fatal("Error authenticating to SP PRO")
		}

		comPort = binary.LittleEndian.Uint16(splinkRead(conn, SplinkAddrComport, 1))
	}

	return comPort
}

func splinkDisconnect(conn net.Conn, comPort uint16) {
	if comPort == 1 || comPort == 2 {
		log.Println("Disconnecting...")
		splinkWrite(conn, SplinkAddrDisconnect + uint32(comPort) - 1, []uint16{1})
	}
}

func splinkMakeHeader(op SplinkOperation, address uint32, dataLen uint8) []byte {
	packetBuf := new(bytes.Buffer)

	header := SplinkHeader{
		Op: byte(op),
		Len: dataLen - 1,
		Addr: address,
	}
	binary.Write(packetBuf, binary.LittleEndian, header)

	headerCrc := crc.Crc16(packetBuf.Bytes())
	binary.Write(packetBuf, binary.LittleEndian, headerCrc)

	return packetBuf.Bytes()
}

func splinkRead(conn net.Conn, address uint32, respLen uint8) []byte {
	packet := splinkMakeHeader(SplinkReadOp, address, respLen)
	_, err := conn.Write(packet)
	if err != nil {
		log.Fatal("Error writing serial read: ", err)
	}

	// header, response and data crc
	resp := make([]byte, SplinkHeaderLen+2*respLen+2)
	_, err = io.ReadFull(conn, resp)
	if err != nil {
		log.Fatal("Error reading serial read: ", err)
	}

	// check header and data crc
	if crc.Crc16(resp[:SplinkHeaderLen]) != 0 || crc.Crc16(resp[SplinkHeaderLen:]) != 0 {
		log.Printf("Received packet CRC mismatch: % x\n", resp)
	}

	// return only data (discard header and data crc)
	return resp[SplinkHeaderLen : len(resp) - 2]
}

func splinkWrite(conn net.Conn, address uint32, data []uint16) {
	// convert data to bytes and calculate crc
	dataBuf := new(bytes.Buffer)
	binary.Write(dataBuf, binary.LittleEndian, data)
	binary.Write(dataBuf, binary.LittleEndian, crc.Crc16(dataBuf.Bytes()))

	// combine header with data
	packet := splinkMakeHeader(SplinkWriteOp, address, uint8(len(data)))
	packet = append(packet, dataBuf.Bytes()...)

	_, err := conn.Write(packet)
	if err != nil {
		log.Fatal("Error writing serial write: ", err)
	}

	resp := make([]byte, len(packet))
	_, err = io.ReadFull(conn, resp)
	if err != nil {
		log.Fatal("Error reading serial write: ", err)
	} else if !bytes.Equal(packet, resp) {
		log.Fatalf("Error receiving write response, got %v, expected %v\n", resp, packet)
	}
}
