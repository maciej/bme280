package bme280

//go:generate stringer -type Mode,Filter,StandByTime,Oversampling -output strings.go

import (
	"fmt"
	"time"
	"encoding/binary"
	"github.com/quhar/bme280"
	"math"
)

type bus interface {
	ReadReg(byte, []byte) error
	WriteReg(byte, []byte) error
}

type Mode byte
type Filter byte
type StandByTime byte
type Oversampling byte

//noinspection GoUnusedConst
const (
	ModeSleep  Mode = 0x00
	ModeForced Mode = 0x01
	ModeNormal Mode = 0x03
)

//noinspection GoUnusedConst,GoSnakeCaseUsage
const (
	I2CAddr = 0x77

	// Stand-by modes
	StandByTime1ms    StandByTime = 0x00
	StandByTime62_5ms StandByTime = 0x01
	StandByTime125ms  StandByTime = 0x02
	StandByTime250ms  StandByTime = 0x03
	StandByTime500ms  StandByTime = 0x04
	StandByTime1000ms StandByTime = 0x05
	StandByTime10ms   StandByTime = 0x06
	StandByTime20ms   StandByTime = 0x07

	// Filter coefficients
	FilterOff Filter = 0x00
	Filter2   Filter = 0x01
	Filter4   Filter = 0x02
	Filter8   Filter = 0x03
	Filter16  Filter = 0x04

	// Oversampling
	OversamplingOff Oversampling = 0x00
	Oversampling1x  Oversampling = 0x01
	Oversampling2x  Oversampling = 0x02
	Oversampling4x  Oversampling = 0x03
	Oversampling8x  Oversampling = 0x04
	Oversampling16x Oversampling = 0x05

	// Register addresses
	chipIdAddr             byte = 0xD0
	resetAddr              byte = 0xE0
	tempPressCalibDataAddr byte = 0x88
	humidityCalibDataAddr  byte = 0xE1
	pwrCtrlAddr            byte = 0xF4
	ctrlHumAddr            byte = 0xF2
	ctrlMeasAddr           byte = 0xF4
	configAddr             byte = 0xF5
	dataAddr               byte = 0xF7

	chipId byte = 0x60
)

var oversamplingCoefs []float32

func init() {
	oversamplingCoefs = make([]float32, 8)

	oversamplingCoefs[0] = 0.0
	oversamplingCoefs[1] = 1.0
	oversamplingCoefs[2] = 2.0
	oversamplingCoefs[3] = 4.0
	oversamplingCoefs[4] = 8.0
	oversamplingCoefs[5] = 16.0
	// As indicated in the datasheet 0x05 and higher values stand for x16 oversampling
	oversamplingCoefs[6] = 16.0
	oversamplingCoefs[7] = 16.0
}

type Driver struct {
	device      bus
	mode        Mode // Desired operation mode
	initialized bool
	calib struct {
		t1    uint16
		t2    int16
		t3    int16
		p1    uint16
		p2    int16
		p3    int16
		p4    int16
		p5    int16
		p6    int16
		p7    int16
		p8    int16
		p9    int16
		h1    uint8
		h2    int16
		h3    uint8
		h4    int16
		h5    int16
		h6    int8
		tFine int32
	}
}

type Settings struct {
	Filter                  Filter
	Standby                 StandByTime
	PressureOversampling    Oversampling
	TemperatureOversampling Oversampling
	HumidityOversampling    Oversampling
}

type Response struct {
	Temperature float64
	Pressure    float64
	Humidity    float64
}

type ucompData struct {
	temp  uint32
	press uint32
	hum   uint32
}

func New(device bus) *Driver {
	return &Driver{
		device: device,
	}
}

func (d *Driver) Init() error {
	// This function follows the official driver bme280_init method algorithm
	buf := make([]byte, 1)
	retries := 5
	for {
		err := d.device.ReadReg(chipIdAddr, buf)
		if err != nil || buf[0] != chipId {
			if retries == 0 {
				if err == nil {
					return fmt.Errorf("chipId does not match expectd value, got %X", buf[0])
				}
				return err
			}
			retries--
			continue
		}
		break
	}

	err := d.softReset()
	if err != nil {
		return err
	}

	err = d.readCalibData()
	if err != nil {
		return err
	}

	time.Sleep(1 * time.Millisecond)
	d.initialized = true
	return nil
}

func (d *Driver) InitWith(mode Mode, c Settings) error {
	err := d.Init()
	if err != nil {
		return err
	}

	err = d.SetSettings(c)
	if err != nil {
		return err
	}

	return d.SetMode(mode)
}

func (d *Driver) SetSettings(s Settings) error {
	mode, err := d.GetMode()
	if err != nil {
		return err
	}

	if mode != ModeSleep {
		err = d.Sleep()
		if err != nil {
			return err
		}
	}

	return d.loadSettings(s)
}

func (d *Driver) SetMode(m Mode) error {
	lastMode, err := d.GetMode()
	if err != nil {
		return err
	}

	if lastMode != ModeSleep {
		d.Sleep()
	}

	buf := make([]byte, 1)
	err = d.device.ReadReg(pwrCtrlAddr, buf)
	if err != nil {
		return nil
	}

	buf[0] &^= 0x03
	buf[0] |= byte(m & 0x03)

	d.device.WriteReg(pwrCtrlAddr, buf)
	if err != nil {
		return nil
	}

	d.mode = m

	return nil
}

// Reads and outputs the current device settings
func (d *Driver) GetSettings() (Settings, error) {
	buf := make([]byte, 1)

	err := d.device.ReadReg(configAddr, buf)
	if err != nil {
		return Settings{}, err
	}

	filter := Filter(buf[0] & (1<<2 | 1<<3 | 1<<4) >> 2)
	standby := StandByTime(buf[0] & (1<<5 | 1<<6 | 1<<7) >> 5)

	err = d.device.ReadReg(ctrlMeasAddr, buf)
	if err != nil {
		return Settings{}, err
	}

	pressureOversampling := Oversampling(buf[0] & (1<<2 | 1<<3 | 1<<4) >> 2)
	tempOversampling := Oversampling(buf[0] & (1<<5 | 1<<6 | 1<<7) >> 5)

	err = d.device.ReadReg(ctrlHumAddr, buf)
	if err != nil {
		return Settings{}, err
	}
	humidityOversampling := Oversampling(buf[0] & (1 | 1<<1 | 1<<2))

	return Settings{
		filter,
		standby,
		pressureOversampling,
		tempOversampling,
		humidityOversampling,
	}, nil
}

func (d *Driver) GetMode() (Mode, error) {
	buf := make([]byte, 1)

	err := d.device.ReadReg(pwrCtrlAddr, buf)
	if err != nil {
		return 0xFF, err
	}

	return Mode(buf[0] & 0x03), nil
}

// Puts the device to sleep
func (d *Driver) Sleep() error {
	settings, err := d.GetSettings()
	if err != nil {
		return err
	}

	err = d.softReset()
	if err != nil {
		return err
	}

	return d.loadSettings(settings)
}

func (d *Driver) Read() (Response, error) {
	if d.mode == ModeForced {
		err := d.forceMeasurement()
		if err != nil {
			return Response{}, err
		}
	}

	buf := make([]byte, 8)
	err := d.device.ReadReg(dataAddr, buf)
	if err != nil {
		return Response{}, err
	}

	u := ucompData{
		uint32(buf[3])<<12 | uint32(buf[4])<<4 | uint32(buf[5])>>4,
		uint32(buf[0])<<12 | uint32(buf[1])<<4 | uint32(buf[2])>>4,
		uint32(buf[6])<<8 | uint32(buf[7]),
	}

	temp, tFine := d.compensateTemperature(u.temp)
	d.calib.tFine = tFine
	pressure := d.compensatePressure(u.press)
	humidity := d.compensateHumidity(u.hum)

	return Response{
		temp,
		pressure,
		humidity,
	}, nil
}

func (d *Driver) compensateTemperature(u uint32) (float64, int32) {
	var tmin int32 = -4000
	var tmax int32 = 8500

	v1 := int32(u)/8 - int32(d.calib.t1)*2
	v1 = v1 * int32(d.calib.t2) / 2048
	v2 := int32(u)/16 - int32(d.calib.t1)
	v2 = ((v2 * v2) / 4096) * int32(d.calib.t3) / 16384

	tFine := v1 + v2
	temp := (tFine*5 + 128) / 256

	if temp < tmin {
		temp = tmin
	} else if temp > tmax {
		temp = tmax
	}

	return float64(temp) / 100.0, tFine
}

func (d *Driver) compensatePressure(u uint32) float64 {
	var pmin uint32 = 3000000
	var pmax uint32 = 11000000

	v1 := int64(d.calib.tFine) - 128000
	v2 := v1 * v1 * int64(d.calib.p6)
	v2 = v2 + (v1 * int64(d.calib.p5) * 131072)
	v2 = v2 + (int64(d.calib.p4) * 34359738368)
	v1 = (v1 * v1 * int64(d.calib.p3) / 256) + (v1 * int64(d.calib.p2) * 4096)
	v3 := int64(140737488355328)
	v1 = (v3 + v1) * int64(d.calib.p1) / 8589934592

	if v1 == 0 {
		return float64(pmin) / 100.0
	}

	v4 := int64(1048576) - int64(u)
	v4 = ((v4*2147483648 - v2) * 3125) / v1
	v1 = int64(d.calib.p9) * (v4 / 8192) * (v4 / 8192) / 33554432
	v2 = int64(d.calib.p8) * v4 / 524288
	v4 = ((v4 + v1 + v2) / 256) + int64(d.calib.p7)*16

	pressure := uint32(v4/2) * 100 / 128

	if pressure < pmin {
		pressure = pmin
	} else if pressure > pmax {
		pressure = pmax
	}

	return float64(pressure) / 100.0 / 100.0
}

func (d *Driver) compensateHumidity(u uint32) float64 {
	var hmax uint32 = 100000

	v1 := d.calib.tFine - int32(76800)
	v2 := int32(u * 16384)
	v3 := int32(d.calib.h4) * 1048576
	v4 := int32(d.calib.h5) * v1

	v5 := (v2 - v3 - v4 + 16384) / 32768
	v2 = v1 * int32(d.calib.h6) / 1024
	v3 = v1 * int32(d.calib.h3) / 2048
	v4 = (v2 * (v3 + 32768) / 1024) + 2097152

	v2 = (v4*int32(d.calib.h2) + 8192) / 16384
	v3 = v5 * v2
	v4 = (v3 / 32768) * (v3 / 32768) / 128
	v5 = v3 - (v4 * int32(d.calib.h1) / 16)

	if v5 < 0 {
		v5 = 0
	} else if v5 > 419430400 {
		v5 = 419430400
	}

	humidity := uint32(v5 / 4096)

	if humidity > hmax {
		humidity = hmax
	}

	return float64(humidity) / 1000.0
}

func (d *Driver) softReset() error {
	var softResetCmd byte = 0xB6

	err := d.device.WriteReg(resetAddr, []byte{softResetCmd})
	if err != nil {
		return err
	}

	time.Sleep(2 * time.Millisecond) // As per specification, wait 2 milliseconds
	return nil
}

func (d *Driver) readCalibData() error {
	buf := make([]byte, 26)

	err := d.device.ReadReg(tempPressCalibDataAddr, buf)
	if err != nil {
		return err
	}

	d.calib.t1 = binary.LittleEndian.Uint16(buf)
	d.calib.t2 = int16(binary.LittleEndian.Uint16(buf[2:]))
	d.calib.t3 = int16(binary.LittleEndian.Uint16(buf[4:]))
	d.calib.p1 = binary.LittleEndian.Uint16(buf[6:])
	d.calib.p2 = int16(binary.LittleEndian.Uint16(buf[8:]))
	d.calib.p3 = int16(binary.LittleEndian.Uint16(buf[10:]))
	d.calib.p4 = int16(binary.LittleEndian.Uint16(buf[12:]))
	d.calib.p5 = int16(binary.LittleEndian.Uint16(buf[14:]))
	d.calib.p6 = int16(binary.LittleEndian.Uint16(buf[16:]))
	d.calib.p7 = int16(binary.LittleEndian.Uint16(buf[18:]))
	d.calib.p8 = int16(binary.LittleEndian.Uint16(buf[20:]))
	d.calib.p9 = int16(binary.LittleEndian.Uint16(buf[22:]))
	d.calib.h1 = buf[25]

	buf = buf[:7]
	err = d.device.ReadReg(humidityCalibDataAddr, buf)
	if err != nil {
		return err
	}

	d.calib.h2 = int16(binary.LittleEndian.Uint16(buf))
	d.calib.h3 = buf[2]
	d.calib.h4 = int16(buf[3])*16 | int16(buf[4]&0x0F)
	d.calib.h5 = int16(int8(buf[5])*16) | int16(buf[4]>>4)
	d.calib.h6 = int8(buf[6])

	return nil
}

func (d *Driver) forceMeasurement() error {
	lastMode, err := d.GetMode()
	if err != nil {
		return err
	}
	if lastMode == ModeNormal {
		return fmt.Errorf("sensor in normal mode, cannot force measurement")
	}

	s, err := d.GetSettings()
	if err != nil {
		return err
	}

	buf := make([]byte, 1)

	buf[0] = byte(s.HumidityOversampling & 0x07)
	err = d.device.WriteReg(ctrlHumAddr, buf)
	if err != nil {
		return err
	}

	buf[0] = 0
	buf[0] = byte(bme280.ForcedMode) | byte(s.PressureOversampling&0x07)<<2 | byte(s.TemperatureOversampling&0x07)<<5
	err = d.device.WriteReg(ctrlMeasAddr, buf)
	if err != nil {
		return err
	}

	// Using the max measurement time formula
	tempMeasTime := 2.3 * oversamplingCoefs[int(s.TemperatureOversampling)]
	pressureMeasTime := 2.3*oversamplingCoefs[int(s.PressureOversampling)] + 0.575
	humidityMeasTime := 2.3*oversamplingCoefs[int(s.HumidityOversampling)] + 0.575
	measTime := 1.25 + tempMeasTime + pressureMeasTime + humidityMeasTime
	measTimeMicros := time.Duration(math.Ceil(float64(measTime * 1000)))

	time.Sleep(measTimeMicros * time.Microsecond)
	return nil
}

func (d *Driver) loadSettings(s Settings) error {
	buf := make([]byte, 1)

	buf[0] = byte(s.HumidityOversampling & 0x07)
	err := d.device.WriteReg(ctrlHumAddr, buf)
	if err != nil {
		return err
	}

	// Reference set_osr_humidity_settings writes to ctrlMeas register here
	// However since we guarantee (communication errors aside) that the function
	// will later on write to that register - we can skip this step here

	err = d.device.ReadReg(ctrlMeasAddr, buf)
	if err != nil {
		return err
	}

	buf[0] &^= (0x07 << 2) | (0x07 << 5) // clear temperature and pressue oversampling settings
	buf[0] |= byte(s.PressureOversampling&0x07)<<2 | byte(s.TemperatureOversampling&0x07)<<5

	err = d.device.WriteReg(ctrlMeasAddr, buf)
	if err != nil {
		return err
	}

	err = d.device.ReadReg(configAddr, buf)
	if err != nil {
		return err
	}

	buf[0] &^= (0x07 << 2) | (0x07 << 5)
	buf[0] |= byte(s.Filter&0x07)<<2 | byte(s.Standby&0x07)<<5

	err = d.device.WriteReg(configAddr, buf)
	if err != nil {
		return err
	}

	return nil
}
