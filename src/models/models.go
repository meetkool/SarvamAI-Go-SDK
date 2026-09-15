package models

type Model string

const (
	TTSBulbulV3 Model = "bulbul:v3"
	TTSBulbulV2 Model = "bulbul:v2"
)

const (
	STTSaarasV3         Model = "saaras:v3"
	STTSaarasV4         Model = "saaras:v4"
	STTSaarasV3Realtime Model = "saaras:v3-realtime"
	STTSaarasV4Realtime Model = "saaras:v4-realtime"
)

type Language string

const (
	LangEnglish   Language = "en-IN"
	LangHindi     Language = "hi-IN"
	LangBengali   Language = "bn-IN"
	LangGujarati  Language = "gu-IN"
	LangKannada   Language = "kn-IN"
	LangMalayalam Language = "ml-IN"
	LangMarathi   Language = "mr-IN"
	LangOdia      Language = "od-IN"
	LangPunjabi   Language = "pa-IN"
	LangTamil     Language = "ta-IN"
	LangTelugu    Language = "te-IN"
)

type Mode string

const (
	ModeTranscribe Mode = "transcribe"
	ModeTranslate  Mode = "translate"
	ModeVerbatim   Mode = "verbatim"
	ModeTranslit   Mode = "translit"
	ModeCodemix    Mode = "codemix"
)

type StreamType string

const (
	StreamFast      StreamType = "fast"
	StreamBalanced  StreamType = "balanced"
	StreamSimulated StreamType = "simulated"
)
