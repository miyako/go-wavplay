// main.go
// Build: go build -o wavplay .
// Usage: wavplay test.wav
//        cat test.wav | wavplay -
//        echo '{"input":"hello"}' | curl -s -X POST ... | wavplay -

package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gen2brain/malgo"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: wavplay <file.wav> | wavplay -")
		os.Exit(1)
	}

	// Read from file or stdin
	var data []byte
	var err error
	if os.Args[1] == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(os.Args[1])
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "read error: %v\n", err)
		os.Exit(1)
	}

	// Parse WAV header
	sampleRate, channels, bitsPerSample, pcmData, err := parseWAV(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wav parse error: %v\n", err)
		os.Exit(1)
	}

	// Init miniaudio context
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "malgo context error: %v\n", err)
		os.Exit(1)
	}
	defer ctx.Uninit()

	// Device config
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format   = malgo.FormatS16
	deviceConfig.Playback.Channels = uint32(channels)
	deviceConfig.SampleRate        = uint32(sampleRate)
	deviceConfig.Alsa.NoMMap       = 1

	// Playback cursor
	offset := 0
	bytesPerFrame := channels * (bitsPerSample / 8)

	callbacks := malgo.DeviceCallbacks{
		Data: func(_, pOut []byte, frameCount uint32) {
			need := int(frameCount) * bytesPerFrame
			available := len(pcmData) - offset
			if available <= 0 {
				// Fill silence
				for i := range pOut {
					pOut[i] = 0
				}
				return
			}
			n := need
			if available < n {
				n = available
			}
			copy(pOut, pcmData[offset:offset+n])
			// Zero-fill remainder if any
			for i := n; i < len(pOut); i++ {
				pOut[i] = 0
			}
			offset += n
		},
	}

	device, err := malgo.InitDevice(ctx.Context, deviceConfig, callbacks)
	if err != nil {
		fmt.Fprintf(os.Stderr, "malgo device error: %v\n", err)
		os.Exit(1)
	}
	defer device.Uninit()

	if err := device.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "malgo start error: %v\n", err)
		os.Exit(1)
	}

	// Wait until all PCM data has been consumed
	totalFrames := len(pcmData) / bytesPerFrame
	duration := time.Duration(float64(time.Second) *
		float64(totalFrames) / float64(sampleRate))
	time.Sleep(duration + 200*time.Millisecond) // small tail to let device flush
}

// parseWAV reads a minimal WAV header and returns audio parameters + PCM bytes.
// Handles only PCM (format 1) — which is exactly what FloatPCMToWav produces.
func parseWAV(data []byte) (sampleRate, channels, bitsPerSample int, pcm []byte, err error) {
	if len(data) < 44 {
		err = fmt.Errorf("file too small to be a WAV (%d bytes)", len(data))
		return
	}

	// RIFF header
	if string(data[0:4]) != "RIFF" {
		err = fmt.Errorf("not a RIFF file")
		return
	}
	if string(data[8:12]) != "WAVE" {
		err = fmt.Errorf("not a WAVE file")
		return
	}

	// Walk chunks
	pos := 12
	for pos+8 <= len(data) {
		chunkID   := string(data[pos : pos+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		pos += 8

		switch chunkID {
		case "fmt ":
			if chunkSize < 16 {
				err = fmt.Errorf("fmt chunk too small")
				return
			}
			audioFormat := int(binary.LittleEndian.Uint16(data[pos : pos+2]))
			if audioFormat != 1 {
				err = fmt.Errorf("unsupported WAV format %d (only PCM=1 supported)", audioFormat)
				return
			}
			channels      = int(binary.LittleEndian.Uint16(data[pos+2 : pos+4]))
			sampleRate    = int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
			bitsPerSample = int(binary.LittleEndian.Uint16(data[pos+14 : pos+16]))

		case "data":
			if pos+chunkSize > len(data) {
				chunkSize = len(data) - pos
			}
			pcm = data[pos : pos+chunkSize]
			return // done — we have everything
		}

		pos += chunkSize
		// WAV chunks are word-aligned
		if chunkSize%2 != 0 {
			pos++
		}
	}

	err = fmt.Errorf("no data chunk found in WAV file")
	return
}