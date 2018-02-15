package bme280

import (
	"strings"
	"testing"
)

type nullBus struct{}

func (*nullBus) ReadReg(byte, []byte) error {
	return nil
}

func (*nullBus) WriteReg(byte, []byte) error {
	return nil
}

func TestRead(t *testing.T) {
	t.Run("fails if uninitialized", func(t *testing.T) {
		driver := New(&nullBus{})

		_, err := driver.Read()
		if err == nil || !strings.HasPrefix(err.Error(), "driver uninitialized") {
			t.Errorf("no or unexpected error: %s", err.Error())
		}
	})
}
