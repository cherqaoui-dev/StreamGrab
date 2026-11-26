package media

import (
	"math"
	"strings"
)

var tiers = []struct {
	Height int
	Label  string
	Floor  int
}{
	{2160, "2160p 4K UHD", 1800},
	{1440, "1440p 2K QHD", 1200},
	{1080, "1080p Full HD", 900},
	{720, "720p Standard HD", 600},
}

var presetOrders = [][]string{
	{"av1", "vp9", "h264"},
	{"vp9", "av1", "h264"},
	{"h264", "vp9", "av1"},
}

var containers = []string{"mp4", "webm", "mkv"}

type Format struct {
	FormatID       string
	Height         int
	FPS            float64
	VCodec         string
	ACodec         string
	Ext            string
	Filesize       float64
	FilesizeApprox float64
	TBR            float64
	ABR            float64
	ASR            int
	DynamicRange   string
	URL            string
	HasDRM         bool
}

type Probe struct {
	ID        string
	Title     string
	Channel   string
	Thumbnail string
	Duration  float64
	ViewCount int64
	IsLive    bool
	AgeLimit  int
	Formats   []Format
}

type Row struct {
	Height         int    `json:"height"`
	Label          string `json:"label"`
	FPS            int    `json:"fps"`
	HDR            bool   `json:"hdr"`
	VideoCodec     string `json:"videoCodec"`
	AudioCodec     string `json:"audioCodec"`
	EstimatedBytes int64  `json:"estimatedBytes"`
	Exact          bool   `json:"exact"`
	Fastest        bool   `json:"fastest"`
	VideoFormatID  string `json:"videoFormatId"`
	AudioFormatID  string `json:"audioFormatId"`
	VideoURL       string `json:"videoUrl,omitempty"`
	AudioURL       string `json:"audioUrl,omitempty"`
	Container      string `json:"container,omitempty"`
	Available      bool   `json:"available"`
	Reason         string `json:"reason,omitempty"`
	RecodeAudio    bool   `json:"recodeAudio,omitempty"`
	Transcoded     bool   `json:"transcoded,omitempty"`
}

type AudioRow struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	Spec           string `json:"spec"`
	Tag            string `json:"tag,omitempty"`
	Transcoded     bool   `json:"transcoded"`
	Copy           bool   `json:"copy"`
	Source         string `json:"source"`
	EstimatedBytes int64  `json:"estimatedBytes"`
	Exact          bool   `json:"exact"`
	FormatID       string `json:"formatId"`
	Container      string `json:"container"`
	SampleRate     int    `json:"sampleRate,omitempty"`
	URL            string `json:"url,omitempty"`
}

type Result struct {
	Rows    []Row
	Chips   []string
	Audio   []AudioRow
	Presets map[string]map[string][]Row
}

func Build(probe Probe) Result {
	presets := map[string]map[string][]Row{}
	for _, order := range presetOrders {
		key := strings.Join(order, "-")
		presets[key] = map[string][]Row{}
		for _, container := range containers {
			presets[key][container] = rowsFor(probe, container, order)
		}
	}
	audio := buildAudio(probe)
	rows := availableRows(presets["av1-vp9-h264"]["mp4"])
	label := ""
	if source := bestAudio(probe.Formats, nil); source != nil {
		label = chipAudio(*source)
	}
	return Result{
		Rows:    rows,
		Chips:   chips(presets["av1-vp9-h264"]["mkv"], label),
		Audio:   audio,
		Presets: presets,
	}
}

func rowsFor(probe Probe, container string, order []string) []Row {
	paired, outputCodec, transcode := pairAudio(probe.Formats, container)
	rows := make([]Row, 0, len(tiers))
	for _, tier := range tiers {
		match := pickVideo(probe.Formats, tier.Floor, tier.Height, order, func(f Format) bool {
			return fitsContainer(container, f.VCodec)
		})
		if match != nil {
			rows = append(rows, makeRow(tier, container, match, paired, outputCodec, transcode, probe.Duration, true, ""))
			continue
		}
		fallback := pickVideo(probe.Formats, tier.Floor, tier.Height, order, nil)
		if fallback == nil {
			continue
		}
		row := makeRow(tier, container, fallback, paired, displayAudioCodec(audioCodecOf(*fallback, paired)), false, probe.Duration, false, unfitReason(container, *fallback))
		row.VideoFormatID = ""
		row.AudioFormatID = ""
		row.VideoURL = ""
		row.AudioURL = ""
		row.RecodeAudio = false
		rows = append(rows, row)
	}
	markFastest(rows)
	return rows
}

func makeRow(tier struct {
	Height int
	Label  string
	Floor  int
}, container string, video *Format, audio *Format, outputCodec string, transcode bool, duration float64, available bool, reason string) Row {
	displayAudio := outputCodec
	recode := transcode && available
	separate := audio
	if hasAudio(*video) {
		separate = nil
		displayAudio = displayAudioCodec(video.ACodec)
		recode = false
		if available && container == "mp4" && displayAudio != "" && displayAudio != "AAC" {
			recode = true
			displayAudio = "AAC"
		}
		if available && container == "webm" && displayAudio != "" && displayAudio != "Opus" {
			recode = true
			displayAudio = "Opus"
		}
	}
	exact, size := estimateSize(*video, separate, duration)
	if recode {
		exact = false
	}
	row := Row{
		Height:         tier.Height,
		Label:          tier.Label,
		FPS:            int(math.Round(video.FPS)),
		HDR:            isHDR(video.DynamicRange),
		VideoCodec:     displayVideoCodec(video.VCodec),
		AudioCodec:     displayAudio,
		EstimatedBytes: size,
		Exact:          exact,
		Container:      container,
		Available:      available,
		Reason:         reason,
		RecodeAudio:    recode,
		Transcoded:     recode,
	}
	if available {
		row.VideoFormatID = video.FormatID
		row.VideoURL = video.URL
		if separate != nil {
			row.AudioFormatID = separate.FormatID
			row.AudioURL = separate.URL
		}
	}
	return row
}

func pairAudio(formats []Format, container string) (*Format, string, bool) {
	opus := bestAudio(formats, isOpus)
	aac := bestAudio(formats, isAAC)
	any := bestAudio(formats, nil)
	switch container {
	case "mp4":
		if aac != nil {
			return aac, "AAC", false
		}
		if any != nil {
			return any, "AAC", true
		}
	case "webm":
		if opus != nil {
			return opus, "Opus", false
		}
		if any != nil {
			return any, "Opus", true
		}
	default:
		if opus != nil {
			return opus, displayAudioCodec(opus.ACodec), false
		}
		if any != nil {
			return any, displayAudioCodec(any.ACodec), false
		}
	}
	return nil, "", false
}

func buildAudio(probe Probe) []AudioRow {
	source := bestAudio(probe.Formats, nil)
	if source == nil {
		return nil
	}
	aac := bestAudio(probe.Formats, isAAC)
	opus := bestAudio(probe.Formats, isOpus)
	origin := spokenAudio(*source)
	rows := []AudioRow{
		flacRow(probe, *source, origin),
		mp3Row(probe, *source, origin),
		opusRow(probe, *source, opus, origin),
		m4aRow(probe, *source, aac, origin),
	}
	assignAudioTags(rows)
	return rows
}

func flacRow(probe Probe, source Format, origin string) AudioRow {
	rate := 48000
	spec := "24-bit, 48 kHz"
	if source.ASR > 0 && source.ASR < 48000 {
		rate = source.ASR
		if source.ASR%1000 == 0 {
			spec = "24-bit, " + itoa(source.ASR/1000) + " kHz"
		} else {
			spec = "24-bit, " + itoa(source.ASR) + " Hz"
		}
	}
	size := int64(0)
	if probe.Duration > 0 {
		size = int64(float64(rate) * 24 * 2 / 8 * probe.Duration * 0.6)
	}
	return AudioRow{
		ID: "flac", Label: "FLAC Lossless", Spec: spec, Source: origin,
		EstimatedBytes: size, FormatID: source.FormatID, Container: "flac",
		SampleRate: rate, URL: source.URL,
	}
}

func mp3Row(probe Probe, source Format, origin string) AudioRow {
	return AudioRow{
		ID: "mp3", Label: "MP3 High Quality", Spec: "320 kbps CBR", Source: origin,
		Transcoded:     source.ABR == 0 || source.ABR < 320,
		EstimatedBytes: bitrateSize(320, probe.Duration), FormatID: source.FormatID,
		Container: "mp3", URL: source.URL,
	}
}

func opusRow(probe Probe, source Format, opus *Format, origin string) AudioRow {
	if opus != nil && displayAudioCodec(source.ACodec) == "Opus" {
		size, exact := sizeOf(*opus, probe.Duration)
		spec := "160 kbps WebM"
		if opus.ABR > 0 {
			spec = trimFloat(opus.ABR) + " kbps WebM"
		}
		return AudioRow{
			ID: "opus", Label: "Opus Native", Spec: spec, Copy: true, Source: origin,
			EstimatedBytes: size, Exact: exact, FormatID: opus.FormatID, Container: "opus", URL: opus.URL,
		}
	}
	return AudioRow{
		ID: "opus", Label: "Opus Native", Spec: "160 kbps WebM", Source: origin,
		Transcoded:     source.ABR == 0 || source.ABR < 160,
		EstimatedBytes: bitrateSize(160, probe.Duration), FormatID: source.FormatID, Container: "opus", URL: source.URL,
	}
}

func m4aRow(probe Probe, source Format, aac *Format, origin string) AudioRow {
	if aac != nil && (aac.ABR == 0 || aac.ABR >= 256) {
		size, exact := sizeOf(*aac, probe.Duration)
		spec := "256 kbps VBR"
		if aac.ABR > 0 {
			spec = trimFloat(aac.ABR) + " kbps AAC"
		}
		return AudioRow{
			ID: "m4a", Label: "M4A / AAC", Spec: spec, Copy: true, Source: origin,
			EstimatedBytes: size, Exact: exact, FormatID: aac.FormatID, Container: "m4a", URL: aac.URL,
		}
	}
	return AudioRow{
		ID: "m4a", Label: "M4A / AAC", Spec: "256 kbps VBR", Source: origin,
		Transcoded:     source.ABR == 0 || source.ABR < 256,
		EstimatedBytes: bitrateSize(256, probe.Duration), FormatID: source.FormatID, Container: "m4a", URL: source.URL,
	}
}

func assignAudioTags(rows []AudioRow) {
	for _, id := range []string{"opus", "m4a"} {
		for i := range rows {
			if rows[i].ID == id && rows[i].Copy {
				rows[i].Tag = "Direct"
				goto best
			}
		}
	}
best:
	bestIndex := -1
	bestScore := -1
	for i, row := range rows {
		if row.Tag != "" || (row.ID != "mp3" && row.ID != "m4a") {
			continue
		}
		score := 256
		if row.ID == "mp3" {
			score = 320
		}
		if score > bestScore {
			bestScore = score
			bestIndex = i
		}
	}
	if bestIndex >= 0 {
		rows[bestIndex].Tag = "Best"
	}
}

func availableRows(rows []Row) []Row {
	out := make([]Row, 0, len(rows))
	for _, row := range rows {
		if row.Available {
			out = append(out, row)
		}
	}
	return out
}

func pickVideo(formats []Format, floor, ceiling int, order []string, allow func(Format) bool) *Format {
	var best *Format
	for i := range formats {
		f := &formats[i]
		if f.HasDRM || !isVideo(*f) || f.Height < floor || f.Height > ceiling {
			continue
		}
		if allow != nil && !allow(*f) {
			continue
		}
		if best == nil || betterVideo(*f, *best, order) {
			best = f
		}
	}
	return best
}

func betterVideo(a, b Format, order []string) bool {
	if a.Height != b.Height {
		return a.Height > b.Height
	}
	if a.FPS != b.FPS {
		return a.FPS > b.FPS
	}
	return rankIn(codecKey(a.VCodec), order) < rankIn(codecKey(b.VCodec), order)
}

func bestAudio(formats []Format, match func(string) bool) *Format {
	var best *Format
	for i := range formats {
		f := &formats[i]
		if f.HasDRM || !isAudioOnly(*f) {
			continue
		}
		if match != nil && !match(f.ACodec) {
			continue
		}
		if best == nil || betterAudio(*f, *best) {
			best = f
		}
	}
	return best
}

func betterAudio(a, b Format) bool {
	if a.ABR != b.ABR {
		return a.ABR > b.ABR
	}
	return audioRank(a.ACodec) > audioRank(b.ACodec)
}

func audioRank(acodec string) int {
	switch displayAudioCodec(acodec) {
	case "Opus":
		return 3
	case "AAC":
		return 2
	default:
		return 1
	}
}

func fitsContainer(container, vcodec string) bool {
	key := codecKey(vcodec)
	switch container {
	case "webm":
		return key == "av1" || key == "vp9"
	case "mp4", "mkv":
		return key != ""
	default:
		return false
	}
}

func unfitReason(container string, video Format) string {
	name := displayVideoCodec(video.VCodec)
	if name == "" {
		name = "This codec"
	}
	label := strings.ToUpper(container)
	if container == "mp4" {
		label = "MP4"
	}
	return name + " can't be copied into " + label
}

func isVideo(f Format) bool {
	return f.VCodec != "" && f.VCodec != "none" && f.Height > 0
}

func isAudioOnly(f Format) bool {
	return (f.VCodec == "" || f.VCodec == "none") && f.ACodec != "" && f.ACodec != "none"
}

func hasAudio(f Format) bool {
	return f.ACodec != "" && f.ACodec != "none"
}

func audioCodecOf(video Format, audio *Format) string {
	if hasAudio(video) {
		return video.ACodec
	}
	if audio != nil {
		return audio.ACodec
	}
	return ""
}

func estimateSize(video Format, audio *Format, duration float64) (bool, int64) {
	videoSize, videoExact := sizeOf(video, duration)
	if hasAudio(video) || audio == nil {
		return videoExact, videoSize
	}
	audioSize, audioExact := sizeOf(*audio, duration)
	if videoSize == 0 && audioSize == 0 {
		return false, 0
	}
	return videoExact && audioExact, videoSize + audioSize
}

func sizeOf(f Format, duration float64) (int64, bool) {
	if f.Filesize > 0 {
		return int64(f.Filesize), true
	}
	if f.FilesizeApprox > 0 {
		return int64(f.FilesizeApprox), false
	}
	rate := f.TBR
	if rate <= 0 {
		rate = f.ABR
	}
	if rate > 0 && duration > 0 {
		return int64(rate * 1000 / 8 * duration), false
	}
	return 0, false
}

func bitrateSize(kbps, duration float64) int64 {
	if kbps <= 0 || duration <= 0 {
		return 0
	}
	return int64(kbps * 1000 / 8 * duration)
}

func isHDR(dynamicRange string) bool {
	value := strings.ToUpper(strings.TrimSpace(dynamicRange))
	return value != "" && value != "SDR"
}

func codecKey(vcodec string) string {
	switch displayVideoCodec(vcodec) {
	case "AV1":
		return "av1"
	case "VP9":
		return "vp9"
	case "H.264":
		return "h264"
	default:
		return ""
	}
}

func rankIn(key string, order []string) int {
	for i, item := range order {
		if item == key {
			return i
		}
	}
	return len(order) + 1
}

func displayVideoCodec(vcodec string) string {
	value := strings.ToLower(vcodec)
	switch {
	case strings.Contains(value, "av01"), strings.Contains(value, "av1"):
		return "AV1"
	case strings.Contains(value, "vp9"), strings.Contains(value, "vp09"):
		return "VP9"
	case strings.Contains(value, "avc"), strings.Contains(value, "h264"), strings.Contains(value, "h.264"):
		return "H.264"
	default:
		if vcodec == "" || vcodec == "none" {
			return ""
		}
		return vcodec
	}
}

func displayAudioCodec(acodec string) string {
	value := strings.ToLower(acodec)
	switch {
	case strings.Contains(value, "mp4a"), strings.Contains(value, "aac"):
		return "AAC"
	case strings.Contains(value, "opus"):
		return "Opus"
	default:
		if acodec == "" || acodec == "none" {
			return ""
		}
		return acodec
	}
}

func isAAC(acodec string) bool {
	return displayAudioCodec(acodec) == "AAC"
}

func isOpus(acodec string) bool {
	return displayAudioCodec(acodec) == "Opus"
}

func isH264(vcodec string) bool {
	return displayVideoCodec(vcodec) == "H.264"
}

func markFastest(rows []Row) {
	best := -1
	bestScore := int64(math.MaxInt64)
	copyBest := -1
	copyScore := int64(math.MaxInt64)
	h264Best := -1
	h264Score := int64(math.MaxInt64)
	for i, row := range rows {
		if !row.Available {
			continue
		}
		score := row.EstimatedBytes
		if score <= 0 {
			score = int64(10000 - row.Height)
		}
		if score < bestScore {
			best = i
			bestScore = score
		}
		if !row.RecodeAudio && score < copyScore {
			copyBest = i
			copyScore = score
		}
		if !row.RecodeAudio && isH264(row.VideoCodec) && row.AudioCodec == "AAC" && score < h264Score {
			h264Best = i
			h264Score = score
		}
	}
	index := h264Best
	if index < 0 {
		index = copyBest
	}
	if index < 0 {
		index = best
	}
	if index >= 0 {
		rows[index].Fastest = true
	}
}

func chips(rows []Row, audioLabel string) []string {
	var top *Row
	for i := range rows {
		if rows[i].Available {
			top = &rows[i]
			break
		}
	}
	if top == nil {
		return nil
	}
	var out []string
	if label := resolutionChip(*top); label != "" {
		out = append(out, label)
	}
	for _, row := range rows {
		if row.Available && row.HDR {
			out = append(out, "HDR")
			break
		}
	}
	if top.VideoCodec != "" {
		out = append(out, top.VideoCodec)
	}
	if audioLabel != "" {
		out = append(out, audioLabel)
	}
	return out
}

func resolutionChip(row Row) string {
	name := ""
	switch {
	case row.Height >= 2160:
		name = "4K"
	case row.Height >= 1440:
		name = "2K"
	case row.Height >= 1080:
		name = "1080p"
	case row.Height >= 720:
		name = "720p"
	default:
		return ""
	}
	if row.FPS >= 50 {
		return name + " 60FPS"
	}
	return name
}

func chipAudio(f Format) string {
	name := displayAudioCodec(f.ACodec)
	if f.ABR > 0 {
		return name + " " + trimFloat(f.ABR) + "k"
	}
	return name
}

func spokenAudio(f Format) string {
	name := displayAudioCodec(f.ACodec)
	if name == "" {
		return "the best available audio"
	}
	if f.ABR > 0 {
		return name + " " + trimFloat(f.ABR) + " kbps"
	}
	return name
}

func trimFloat(value float64) string {
	return itoa(int(math.Round(value)))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [16]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
