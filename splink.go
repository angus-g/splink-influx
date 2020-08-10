package main

import "bytes"
import "crypto/md5"
import "encoding/binary"
import "github.com/angus-g/splink-influx/crc"
import "github.com/influxdata/influxdb-client-go"
import "github.com/influxdata/influxdb-client-go/api"
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
	Op   uint8
	Len  uint8
	Addr uint32
}

const SplinkHeaderLen = 8

type SplinkOperation byte

const (
	SplinkReadOp  SplinkOperation = 'Q'
	SplinkWriteOp SplinkOperation = 'W'
)

const (
	SplinkAddrComport          = 0x0000A000
	SplinkAddrDisconnect       = 0x0000A00D
	SplinkAddrChallenge        = 0x001F0000
	SplinkAddrChallengeSuccess = 0x001F0010
)

const (
	scaleVdc     = 958.0 / 327680.0
	scaleIdc     = 10738.0
	scaleVac     = 4798.0
	scaleIac     = 2000.0
	scalePower   = scaleVac * (scaleIac / 3276800.0)
	scalePower32 = scalePower / 8.0
	scaleEnergy  = 24.0 * scalePower
	scaleTemp    = 480.0
)

type SavedState struct {
	GeneratorStartReason int
	GeneratorRunReason   int
}

func main() {
	// read configuration data
	viper.SetDefault("port", "3000")
	viper.SetDefault("influx_host", "localhost")
	viper.SetDefault("influx_port", "8086")
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
	conn, err := net.Dial("tcp", serialHost+":"+serialPort)
	if err != nil {
		log.Fatal("Error connecting to serial host: ", err)
	}

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
	influxConn := influxdb2.NewClient("http://"+influxHost+":"+influxPort, "")
	defer influxConn.Close()

	// for InfluxDB 1.8: no org, bucket is db name
	influxWrite := influxConn.WriteAPI("", "inverter")

	// ticker to request new data every 15 seconds
	ticker := time.NewTicker(15 * time.Second)
	go func() {
		ss := SavedState{-1, -1}
		for _ = range ticker.C {
			splinkRequestData(conn, influxWrite, &ss)
		}
	}()

	select {}
}

// write a float64 value into the "splink_values" measurement
func writeFloat(wa api.WriteAPI, value_type string, value float64, t time.Time) {
	pt := influxdb2.NewPointWithMeasurement("splink_values").
		AddTag("type", value_type).
		AddField("value", value).
		SetTime(t)
	wa.WritePoint(pt)
}

func splinkRequestData(conn net.Conn, wa api.WriteAPI, ss *SavedState) {
	// multiple reads for different values

	// battery voltage (16-bit)
	val := splinkRead(conn, 0x0000A05C, 1)
	var batteryVoltage int16
	binary.Read(bytes.NewBuffer(val), binary.LittleEndian, &batteryVoltage)

	// source power (16-bit)
	val = splinkRead(conn, 0x0000A08A, 1)
	var sourcePower int16
	binary.Read(bytes.NewBuffer(val), binary.LittleEndian, &sourcePower)

	// load power (32-bit)
	val = splinkRead(conn, 0x0000A093, 2)
	loadPower := binary.LittleEndian.Uint32(val)

	// load and input energy (accumulated) (16-bit)
	// input hours (16-bit)
	val = splinkRead(conn, 0x0000A0BE, 3)
	loadEnergy := binary.LittleEndian.Uint16(val[:2])
	inputEnergy := binary.LittleEndian.Uint16(val[2:4])
	inputHours := binary.LittleEndian.Uint16(val[4:6])

	// generator start/run reason
	val = splinkRead(conn, 0x0000A07E, 2)
	genStartReason := int(val[0])
	genRunReason := int(val[2])

	// initial populate
	if ss.GeneratorStartReason == -1 {
		ss.GeneratorStartReason = genStartReason
	}
	if ss.GeneratorRunReason == -1 {
		ss.GeneratorRunReason = genRunReason
	}

	t := time.Now()
	writeFloat(wa, "battery_voltage", float64(batteryVoltage)*scaleVdc, t)
	writeFloat(wa, "source_power", float64(sourcePower)*scalePower, t)
	writeFloat(wa, "load_power", float64(loadPower)*scalePower32, t)
	writeFloat(wa, "ac_load_energy", float64(loadEnergy)*scaleEnergy, t)
	writeFloat(wa, "ac_input_energy", float64(inputEnergy)*scaleEnergy, t)
	writeFloat(wa, "ac_input_hours", float64(inputHours)/60.0, t)

	// update stateful statuses only on state change
	if ss.GeneratorStartReason != genStartReason {
		fromState := splinkGeneratorReason(ss.GeneratorStartReason)
		toState := splinkGeneratorReason(genStartReason)

		pt := influxdb2.NewPointWithMeasurement("splink_states").
			AddTag("type", "generator_start_reason").
			AddField("from_state", fromState).
			AddField("to_state", toState).
			SetTime(t)
		wa.WritePoint(pt)

		ss.GeneratorStartReason = genStartReason
	}

	if ss.GeneratorRunReason != genRunReason {
		fromState := splinkGeneratorReason(ss.GeneratorRunReason)
		toState := splinkGeneratorReason(genRunReason)

		pt := influxdb2.NewPointWithMeasurement("splink_states").
			AddTag("type", "generator_run_reason").
			AddField("from_state", fromState).
			AddField("to_state", toState).
			SetTime(t)
		wa.WritePoint(pt)

		ss.GeneratorRunReason = genRunReason
	}
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
		splinkWrite(conn, SplinkAddrDisconnect+uint32(comPort)-1, []uint16{1})
	}
	conn.Close()
}

func splinkMakeHeader(op SplinkOperation, address uint32, dataLen uint8) []byte {
	packetBuf := new(bytes.Buffer)

	header := SplinkHeader{
		Op:   byte(op),
		Len:  dataLen - 1,
		Addr: address,
	}
	binary.Write(packetBuf, binary.LittleEndian, header)

	headerCrc := crc.Crc16(packetBuf.Bytes())
	binary.Write(packetBuf, binary.LittleEndian, headerCrc)

	return packetBuf.Bytes()
}

func splinkRead(conn net.Conn, address uint32, respLen uint8) []byte {
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	defer conn.SetDeadline(time.Time{})

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
	return resp[SplinkHeaderLen : len(resp)-2]
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
