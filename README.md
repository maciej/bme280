# BME280 driver

[![GoDoc](https://godoc.org/github.com/maciej/bme280?status.svg)](https://godoc.org/github.com/maciej/bme280)
[![Build Status](https://travis-ci.org/maciej/bme280.svg?branch=master)](https://travis-ci.org/maciej/bme280)

Go driver for the Bosch BME280 sensor.

## Example
```go
// Error handling is omitted
device, err := i2c.Open(&i2c.Devfs{Dev: "/dev/i2c-1"}, bme280.I2CAddr)
driver := bme280.New(device)
err = driver.InitWith(bme280.ModeForced, bme280.Settings{
		Filter:                  bme280.FilterOff,
		Standby:                 bme280.StandByTime1000ms,
		PressureOversampling:    bme280.Oversampling16x,
		TemperatureOversampling: bme280.Oversampling16x,
		HumidityOversampling:    bme280.Oversampling16x,
	})

response, err := driver.Read()
```

## References
* [Datasheet](http://datasheet.octopart.com/BME280-Bosch-Tools-datasheet-101965457.pdf)
* [Reference driver](https://github.com/BoschSensortec/BME280_driver)
* [quhar/bme280](https://github.com/quhar/bme280) - another BME280 driver written in Go worth considering
* [periph.io](https://periph.io) - if you're considering to use a whole low-level peripherals library in Go 
                                   (it has BME280 support)
