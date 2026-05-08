package config

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
	CropPosition   string

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

	MusicDir      string
	MusicVolume   float64
	MusicDucking  bool
	DuckRatio     float64
	DuckAttackMS  int
	DuckReleaseMS int

	StreamOverlayDir     string
	StreamOverlayOpacity float64

	MetadataPolicy string

	TemporalShift float64
	FPSTweak      float64
	CodecProfile  string
}
