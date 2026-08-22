package sarvam

import "fmt"

type Codec string

const (
	CodecMP3   Codec = "mp3"
	CodecWAV   Codec = "wav"
	CodecFLAC  Codec = "flac"
	CodecOpus  Codec = "opus"
	CodecAAC   Codec = "aac"
	CodecPCM16 Codec = "linear16"
	CodecULaw  Codec = "mulaw"
	CodecALaw  Codec = "alaw"
)

type Format struct {
	Codec      Codec
	SampleRate int
	Bitrate    int
}

func MP3(sampleRate, kbps int) Format {
	return Format{Codec: CodecMP3, SampleRate: sampleRate, Bitrate: kbps}
}

func WAV(sampleRate int) Format { return Format{Codec: CodecWAV, SampleRate: sampleRate} }

func FLAC(sampleRate int) Format { return Format{Codec: CodecFLAC, SampleRate: sampleRate} }

func Opus(sampleRate, kbps int) Format {
	return Format{Codec: CodecOpus, SampleRate: sampleRate, Bitrate: kbps}
}

func AAC(sampleRate, kbps int) Format {
	return Format{Codec: CodecAAC, SampleRate: sampleRate, Bitrate: kbps}
}

func PCM16(sampleRate int) Format { return Format{Codec: CodecPCM16, SampleRate: sampleRate} }

func ULaw8000() Format { return Format{Codec: CodecULaw, SampleRate: 8000} }

func ALaw8000() Format { return Format{Codec: CodecALaw, SampleRate: 8000} }

func (f Format) IsZero() bool { return f == (Format{}) }

func (f Format) String() string {
	if f.IsZero() {
		return "unset"
	}
	s := fmt.Sprintf("%s %dHz", f.Codec, f.SampleRate)
	if f.Bitrate > 0 {
		s += fmt.Sprintf(" %dkbps", f.Bitrate)
	}
	return s
}

func (f Format) MIMEType() string {
	switch f.Codec {
	case CodecMP3:
		return "audio/mpeg"
	case CodecWAV:
		return "audio/wav"
	case CodecFLAC:
		return "audio/flac"
	case CodecOpus:
		return "audio/ogg"
	case CodecAAC:
		return "audio/aac"
	case CodecPCM16:
		return "audio/L16"
	case CodecULaw:
		return "audio/basic"
	case CodecALaw:
		return "audio/x-alaw-basic"
	}
	return "application/octet-stream"
}

func (f Format) extension() string {
	switch f.Codec {
	case "":
		return "bin"
	case CodecOpus:
		return "ogg"
	case CodecPCM16, CodecULaw, CodecALaw:
		return "pcm"
	}
	return string(f.Codec)
}

func (f Format) bitrateParam() string {
	if f.Bitrate <= 0 {
		return ""
	}
	return fmt.Sprintf("%dk", f.Bitrate)
}
