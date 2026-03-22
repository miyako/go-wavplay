package main

import (
    "encoding/binary"
    "fmt"
    "io"
    "os"
    "sync"
    "sync/atomic"
    "time"
    "runtime"

    "github.com/gen2brain/malgo"
)

func main() {
    if len(os.Args) < 2 {
        fmt.Fprintln(os.Stderr, "usage: wavplay <file.wav> | wavplay -")
        os.Exit(1)
    }

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

    sampleRate, channels, bitsPerSample, pcmData, err := parseWAV(data)
    if err != nil {
        fmt.Fprintf(os.Stderr, "wav parse error: %v\n", err)
        os.Exit(1)
    }

    var backends []malgo.Backend
    switch runtime.GOOS {
    case "darwin":
        backends = []malgo.Backend{malgo.BackendCoreaudio}
    case "windows":
        backends = []malgo.Backend{malgo.BackendWasapi}
    default:
        backends = nil // let malgo pick
    }
    
    ctx, err := malgo.InitContext(backends, malgo.ContextConfig{}, nil)
    if err != nil {
        fmt.Fprintf(os.Stderr, "malgo context error: %v\n", err)
        os.Exit(1)
    }
    defer ctx.Uninit()

    deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
    deviceConfig.Playback.Format   = malgo.FormatS16
    deviceConfig.Playback.Channels = uint32(channels)
    deviceConfig.SampleRate        = uint32(sampleRate)

    bytesPerFrame := channels * (bitsPerSample / 8)

    var offset atomic.Int64
    var once   sync.Once
    done       := make(chan struct{})

    callbacks := malgo.DeviceCallbacks{
        Data: func(pOut, _ []byte, frameCount uint32) {
            if len(pOut) == 0 || frameCount == 0 {
                return
            }

            need      := int(frameCount) * bytesPerFrame
            cur       := int(offset.Load())
            available := len(pcmData) - cur

            if available <= 0 {
                for i := range pOut {
                    pOut[i] = 0
                }
                once.Do(func() { close(done) })
                return
            }

            n := need
            if available < n {
                n = available
            }
            if n > len(pOut) {
                n = len(pOut)
            }

            copy(pOut[:n], pcmData[cur:cur+n])
            for i := n; i < len(pOut); i++ {
                pOut[i] = 0
            }
            offset.Add(int64(n))
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

    totalFrames := len(pcmData) / bytesPerFrame
    timeout := time.Duration(float64(time.Second)*
        float64(totalFrames)/float64(sampleRate)) + 2*time.Second

    select {
    case <-done:
        time.Sleep(300 * time.Millisecond)
    case <-time.After(timeout):
        fmt.Fprintln(os.Stderr, "timeout waiting for playback")
    }
}

func parseWAV(data []byte) (sampleRate, channels, bitsPerSample int, pcm []byte, err error) {
    if len(data) < 44 {
        err = fmt.Errorf("file too small to be a WAV (%d bytes)", len(data))
        return
    }
    if string(data[0:4]) != "RIFF" {
        err = fmt.Errorf("not a RIFF file")
        return
    }
    if string(data[8:12]) != "WAVE" {
        err = fmt.Errorf("not a WAVE file")
        return
    }

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
                err = fmt.Errorf("unsupported WAV format %d (only PCM=1)", audioFormat)
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
            return
        }

        pos += chunkSize
        if chunkSize%2 != 0 {
            pos++
        }
    }

    err = fmt.Errorf("no data chunk found")
    return
}
