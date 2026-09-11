//go:build cgo

package mic

import (
	"fmt"

	"github.com/gen2brain/malgo"
)

func open(m *Mic, sampleRate int) (func(), error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("mic: opening the audio backend: %w", err)
	}
	release := func() {
		_ = ctx.Uninit()
		ctx.Free()
	}

	config := malgo.DefaultDeviceConfig(malgo.Capture)
	config.Capture.Format = malgo.FormatS16
	config.Capture.Channels = 1
	config.SampleRate = uint32(sampleRate)

	device, err := malgo.InitDevice(ctx.Context, config, malgo.DeviceCallbacks{
		Data: func(_, input []byte, _ uint32) { m.push(input) },
	})
	if err != nil {
		release()
		return nil, fmt.Errorf("mic: opening the microphone: %w", err)
	}
	if err := device.Start(); err != nil {
		device.Uninit()
		release()
		return nil, fmt.Errorf("mic: starting the microphone: %w", err)
	}

	return func() {
		device.Uninit()
		release()
	}, nil
}
