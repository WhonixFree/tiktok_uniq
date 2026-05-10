package config

type SpeedRange struct {
	MinPercent float64
	MaxPercent float64
}

type SineParamsRange struct {
	AmplitudeMin float64
	AmplitudeMax float64
	FrequencyMin float64
	FrequencyMax float64
	PhaseMin     float64
	PhaseMax     float64
}

type EventCountRange struct {
	Min int
	Max int
}

type PercentRange struct {
	Min float64
	Max float64
}

type Config struct {
	InputDir      string
	OutputDir     string
	TmpDir        string
	LogsDir       string
	Recursive     bool
	Jobs          int
	ThreadsPerJob int
	Seed          int64
	DryRun        bool
	Overwrite     bool
	Cleanup       bool

	TrimEnabled  bool
	TrimStartMin float64
	TrimStartMax float64
	TrimEndMin   float64
	TrimEndMax   float64

	CropEnabled    bool
	CropMinPercent float64
	CropMaxPercent float64

	ColorPreset    string
	ColorStrength  string
	ColorConfigDir string

	Captions           string
	CaptionTemplate    string
	CaptionTemplateDir string
	CaptionLanguage    string
	CaptionModel       string

	AudioEnvelope       string
	AudioEnvelopeConfig string

	MusicEnabled bool
	MusicDir     string
	MusicVolume  float64
	MusicDucking bool
	DuckingMod   string

	StreamOverlayDir     string
	StreamOverlayOpacity float64

	MetadataPolicy string

	TemporalShift float64
	FPSTweak      float64
	CodecProfile  string

	AudioBaseSpeed SpeedRange
	VideoBaseSpeed SpeedRange
	AudioSine      SineParamsRange
	VideoSine      SineParamsRange
	AVSineMode     string

	FreezeCount          EventCountRange
	ReplaceCount         EventCountRange
	MinEventDistanceSec  float64
	ReplaceDonorDistance int

	PixelReplacePercent PercentRange
	PixelBlurSigma      PercentRange
	PixelReplaceMode    string
	PixelAreaEdgeInset  PercentRange
	PixelAreaSmartGrid  int
	NeighborOffsetMin   int
	NeighborOffsetMax   int

	MetadataFullMode string
}
