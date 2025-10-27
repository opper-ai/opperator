// Package anim provides an animated spinner.
package anim

import (
	"image/color"
	"math"
	"math/rand/v2"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/lucasb-eyer/go-colorful"
)

const (
	fps           = 20
	initialChar   = '.'
	labelGap      = " "
	labelGapWidth = 1

	// Periods of ellipsis animation speed in steps.
	// change every 8 frames (400ms).
	ellipsisAnimSpeed = 8

	// The maximum amount of time that can pass before a character appears.
	// This is used to create a staggered entrance effect.
	maxBirthOffset = time.Second

	// Number of frames to prerender for the animation. After this number
	// cycling is disabled.
	prerenderedFrames = 10

	// slideEasePow controls the ease-in strength for the initial dot build.
	// Values >1 produce acceleration (starts slow, ends fast).
	slideEasePow = 2.2
	// dotBuildEasePow lets the dot build ease independently of other timings.
	dotBuildEasePow = 2.0
)

// Default colors for gradient.
var (
	defaultGradColorA = color.RGBA{R: 0xff, G: 0, B: 0, A: 0xff}
	defaultGradColorB = color.RGBA{R: 0, G: 0, B: 0xff, A: 0xff}
	defaultLabelColor = color.RGBA{R: 0xcc, G: 0xcc, B: 0xcc, A: 0xff}
)

var (
	availableRunes = []rune("0123456789abcdefABCDEF~!@#$£€%^&*()+=")
	ellipsisFrames = []string{".", "..", "...", ""}
)

// Internal ID management. Used during animating to ensure that frame messages
// are received only by spinner components that sent them.
var lastID int64

func nextID() int { return int(atomic.AddInt64(&lastID, 1)) }

type StepMsg struct{ id int }

type Settings struct {
	Size        int
	Label       string
	LabelColor  color.Color
	GradColorA  color.Color
	GradColorB  color.Color
	CycleColors bool
	// BuildLabel enables a decode/build effect where the label
	// is revealed left-to-right while each unrevealed position
	// cycles through random characters.
	BuildLabel bool
	// BuildInterval controls how long between each character reveal.
	BuildInterval time.Duration
	// BuildDelay is the time to wait showing just dots
	// before starting the build/reveal.
	BuildDelay time.Duration
	// ShufflePrelude is the duration to show a full-label
	// random shuffling phase after dots and before reveal.
	ShufflePrelude time.Duration
	// Defaults to true if not specified by callers in this repo.
	ShowEllipsis bool
	// CycleReveal makes the reveal animation cycle back to symbols after completion
	CycleReveal bool
	// before scrambling again when CycleReveal is enabled.
	DisplayDuration time.Duration
	// ScrambleBackDuration controls how long the left-to-right scramble-back takes.
	ScrambleBackDuration time.Duration
}

type Anim struct {
	width            int
	cyclingCharWidth int
	label            []string
	labelWidth       int
	labelColor       color.Color
	startTime        time.Time
	birthOffsets     []time.Duration
	initialFrames    [][]string // frames for the initial characters
	initialized      atomic.Bool
	cyclingFrames    [][]string // frames for the cycling characters
	step             atomic.Int64
	ellipsisStep     atomic.Int64
	ellipsisRendered []string // prerendered ellipsis frames
	id               int
	// Cycling mode variables
	cycleMode         int // 0 = symbols, 1 = loading text
	cycleModeStart    time.Time
	cycleModeDuration time.Duration
	labelGradFrames   [][]string // gradient frames for the label text
	// Build-label mode
	buildLabel        bool
	showEllipsis      bool
	labelRunes        []rune
	revealTimes       []time.Time
	revealStart       time.Time
	shufflePreludeEnd time.Time
	shuffleStartTimes []time.Time
	shuffleAllStart   time.Time
	scrambleFrames    [][]string // random runes for unrevealed label positions
	preDots           []string   // gradient-colored dots shown before reveal start
	// Cycle reveal mode
	cycleReveal          bool
	cyclePhase           int // 0=reveal, 1=show text, 2=scramble back
	cyclePhaseStart      time.Time
	revealComplete       time.Time
	buildDelay           time.Duration
	buildInterval        time.Duration
	shufflePrelude       time.Duration
	displayDuration      time.Duration
	scrambleBackDuration time.Duration
}

func New(opts Settings) *Anim {
	a := &Anim{}

	if opts.Size < 1 {
		opts.Size = 10
	}
	if colorIsUnset(opts.GradColorA) {
		opts.GradColorA = defaultGradColorA
	}
	if colorIsUnset(opts.GradColorB) {
		opts.GradColorB = defaultGradColorB
	}
	if colorIsUnset(opts.LabelColor) {
		opts.LabelColor = defaultLabelColor
	}

	a.id = nextID()
	a.startTime = time.Now()
	a.cyclingCharWidth = opts.Size
	a.labelColor = opts.LabelColor
	a.labelWidth = lipgloss.Width(opts.Label)
	a.buildLabel = opts.BuildLabel
	a.cycleReveal = opts.CycleReveal
	a.cyclePhase = 0
	if opts.BuildDelay < 0 {
		opts.BuildDelay = 0
	}
	if opts.ShufflePrelude < 0 {
		opts.ShufflePrelude = 0
	}
	a.buildDelay = opts.BuildDelay
	a.buildInterval = opts.BuildInterval
	if a.buildInterval <= 0 {
		a.buildInterval = 100 * time.Millisecond
	}
	a.shufflePrelude = opts.ShufflePrelude
	a.displayDuration = opts.DisplayDuration
	if a.displayDuration <= 0 {
		a.displayDuration = 2 * time.Second
	}
	a.scrambleBackDuration = opts.ScrambleBackDuration
	if a.scrambleBackDuration <= 0 {
		a.scrambleBackDuration = 1500 * time.Millisecond
	}
	// Default to showing ellipsis unless caller disables.
	a.showEllipsis = true
	if opts.ShowEllipsis {
		a.showEllipsis = true
	} else if a.buildLabel {
		a.showEllipsis = false
	} else {
		a.showEllipsis = true
	}

	a.renderLabel(opts.Label)

	a.cycleMode = 0 // Start with symbols
	a.cycleModeStart = time.Now()
	a.cycleModeDuration = 3 * time.Second // Each mode lasts 3 seconds

	var ramp []color.Color
	numFrames := prerenderedFrames

	if a.buildLabel {
		a.width = a.labelWidth
		a.labelRunes = []rune(opts.Label)
		length := len(a.labelRunes)
		if length <= 0 {
			length = 1
		}
		if opts.CycleColors {
			ramp = makeGradientRamp(length*3, opts.GradColorA, opts.GradColorB, opts.GradColorA, opts.GradColorB)
			numFrames = length * 2
			if numFrames < prerenderedFrames {
				numFrames = prerenderedFrames
			}
		} else {
			ramp = makeGradientRamp(length, opts.GradColorA, opts.GradColorB)
		}
		a.preDots = make([]string, length)
		for j := 0; j < length; j++ {
			idx := j
			if idx >= len(ramp) {
				idx = len(ramp) - 1
				if idx < 0 {
					idx = 0
				}
			}
			a.preDots[j] = lipgloss.NewStyle().Foreground(ramp[idx]).Render(string(initialChar))
		}
	} else {
		a.width = a.cyclingCharWidth
		if a.labelWidth > 0 {
			a.width += labelGapWidth + a.labelWidth
		}
		if opts.CycleColors {
			ramp = makeGradientRamp(a.width*3, opts.GradColorA, opts.GradColorB, opts.GradColorA, opts.GradColorB)
			numFrames = a.width * 2
		} else {
			ramp = makeGradientRamp(a.width, opts.GradColorA, opts.GradColorB)
		}
	}

	if a.buildLabel {
		a.scrambleFrames = make([][]string, numFrames)
		offset := 0
		for i := range a.scrambleFrames {
			a.scrambleFrames[i] = make([]string, len(a.labelRunes))
			for j := range a.scrambleFrames[i] {
				if j+offset >= len(ramp) {
					continue
				}
				r := availableRunes[rand.IntN(len(availableRunes))]
				a.scrambleFrames[i][j] = lipgloss.NewStyle().Foreground(ramp[j+offset]).Render(string(r))
			}
			if opts.CycleColors {
				offset++
			}
		}
		a.labelGradFrames = make([][]string, numFrames)
		offset = 0
		for i := range a.labelGradFrames {
			a.labelGradFrames[i] = make([]string, len(a.labelRunes))
			for j := range a.labelGradFrames[i] {
				if j+offset >= len(ramp) {
					a.labelGradFrames[i][j] = lipgloss.NewStyle().Foreground(opts.LabelColor).Render(string(a.labelRunes[j]))
					continue
				}
				a.labelGradFrames[i][j] = lipgloss.NewStyle().Foreground(ramp[j+offset]).Render(string(a.labelRunes[j]))
			}
			if opts.CycleColors {
				offset++
			}
		}
		interval := opts.BuildInterval
		if interval <= 0 {
			interval = 100 * time.Millisecond
		}
		a.revealStart = a.startTime.Add(opts.BuildDelay)
		prelude := a.shufflePrelude
		a.shufflePreludeEnd = a.revealStart.Add(prelude)
		a.shuffleStartTimes = make([]time.Time, len(a.labelRunes))
		if prelude > 0 && len(a.labelRunes) > 0 {
			total := len(a.labelRunes)
			for i := range a.shuffleStartTimes {
				// Ease the shuffle start so the randomization blooms left-to-right.
				frac := float64(i+1) / float64(total)
				eased := math.Pow(frac, 1.0/slideEasePow)
				if eased < 0 {
					eased = 0
				}
				if eased > 1 {
					eased = 1
				}
				dt := time.Duration(float64(prelude) * eased)
				a.shuffleStartTimes[i] = a.revealStart.Add(dt)
			}
		} else {
			for i := range a.shuffleStartTimes {
				a.shuffleStartTimes[i] = a.revealStart
			}
		}
		a.shuffleAllStart = a.shufflePreludeEnd
		a.revealTimes = make([]time.Time, len(a.labelRunes))
		for i := range a.revealTimes {
			a.revealTimes[i] = a.shuffleAllStart.Add(time.Duration(i) * interval)
		}
	} else {
		a.initialFrames = make([][]string, numFrames)
		offset := 0
		for i := range a.initialFrames {
			a.initialFrames[i] = make([]string, a.width)
			for j := range a.initialFrames[i] {
				if j+offset >= len(ramp) {
					continue
				}
				var c color.Color
				if j <= a.cyclingCharWidth {
					c = ramp[j+offset]
				} else {
					c = opts.LabelColor
				}
				a.initialFrames[i][j] = lipgloss.NewStyle().Foreground(c).Render(string(initialChar))
			}
			if opts.CycleColors {
				offset++
			}
		}
		a.cyclingFrames = make([][]string, numFrames)
		offset = 0
		for i := range a.cyclingFrames {
			a.cyclingFrames[i] = make([]string, a.cyclingCharWidth)
			for j := range a.cyclingFrames[i] {
				if j+offset >= len(ramp) {
					continue
				}
				r := availableRunes[rand.IntN(len(availableRunes))]
				a.cyclingFrames[i][j] = lipgloss.NewStyle().Foreground(ramp[j+offset]).Render(string(r))
			}
			if opts.CycleColors {
				offset++
			}
		}
		a.birthOffsets = make([]time.Duration, a.cyclingCharWidth)
		for i := range a.birthOffsets {
			a.birthOffsets[i] = time.Duration(rand.N(int64(maxBirthOffset))) * time.Nanosecond
		}

		if a.labelWidth > 0 {
			labelLen := len([]rune(opts.Label))
			labelRamp := makeGradientRamp(labelLen*3, opts.GradColorA, opts.GradColorB, opts.GradColorA, opts.GradColorB)
			a.labelGradFrames = make([][]string, labelLen*2)
			labelRunes := []rune(opts.Label)
			offset := 0
			for i := range a.labelGradFrames {
				a.labelGradFrames[i] = make([]string, labelLen)
				for j := range a.labelGradFrames[i] {
					if j+offset >= len(labelRamp) {
						a.labelGradFrames[i][j] = lipgloss.NewStyle().Foreground(opts.LabelColor).Render(string(labelRunes[j]))
						continue
					}
					a.labelGradFrames[i][j] = lipgloss.NewStyle().Foreground(labelRamp[j+offset]).Render(string(labelRunes[j]))
				}
				offset++
			}
		}
	}

	return a
}

func (a *Anim) renderLabel(label string) {
	if a.labelWidth > 0 {
		rs := []rune(label)
		a.label = make([]string, 0, len(rs))
		for _, r := range rs {
			a.label = append(a.label, lipgloss.NewStyle().Foreground(a.labelColor).Render(string(r)))
		}
		a.ellipsisRendered = make([]string, 0, len(ellipsisFrames))
		for _, frame := range ellipsisFrames {
			a.ellipsisRendered = append(a.ellipsisRendered, lipgloss.NewStyle().Foreground(a.labelColor).Render(frame))
		}
	} else {
		a.label = nil
		a.ellipsisRendered = nil
	}
}

func (a *Anim) Width() int {
	if a.buildLabel {
		w := a.labelWidth
		if a.showEllipsis && a.labelWidth > 0 {
			widest := 0
			for _, f := range ellipsisFrames {
				if fw := lipgloss.Width(f); fw > widest {
					widest = fw
				}
			}
			w += widest
		}
		return w
	}
	w := a.cyclingCharWidth
	if a.labelWidth > 0 {
		w += labelGapWidth + a.labelWidth
		if a.showEllipsis {
			widest := 0
			for _, f := range ellipsisFrames {
				if fw := lipgloss.Width(f); fw > widest {
					widest = fw
				}
			}
			w += widest
		}
	}
	return w
}

func (a *Anim) Init() tea.Cmd { return a.Step() }

func (a *Anim) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case StepMsg:
		if msg.id != a.id {
			return a, nil
		}
		step := a.step.Add(1)
		framesLen := len(a.cyclingFrames)
		if a.buildLabel {
			framesLen = len(a.scrambleFrames)
		}
		if int(step) >= framesLen {
			a.step.Store(0)
		}

		if a.buildLabel {
			now := time.Now()
			if !a.initialized.Load() && len(a.revealTimes) > 0 && now.After(a.revealTimes[len(a.revealTimes)-1]) {
				a.initialized.Store(true)
				if a.cycleReveal {
					a.revealComplete = now
					a.cyclePhaseStart = now
					a.cyclePhase = 1 // Move to display phase
				}
			} else if a.initialized.Load() && a.cycleReveal {
				switch a.cyclePhase {
				case 1: // Displaying completed label
					if now.Sub(a.cyclePhaseStart) > a.displayDuration {
						a.cyclePhase = 2
						a.cyclePhaseStart = now
					}
				case 2: // Scrambling back left-to-right
					if now.Sub(a.cyclePhaseStart) > a.scrambleBackDuration {
						a.initialized.Store(false)
						a.cyclePhase = 0
						a.cyclePhaseStart = now
						a.startTime = now
						a.buildDelay = 0
						a.shufflePrelude = 0
						a.revealStart = a.startTime.Add(a.buildDelay)
						a.shufflePreludeEnd = a.revealStart.Add(a.shufflePrelude)
						a.shuffleAllStart = a.shufflePreludeEnd
						for i := range a.shuffleStartTimes {
							a.shuffleStartTimes[i] = a.revealStart
						}
						for i := range a.revealTimes {
							a.revealTimes[i] = a.shuffleAllStart.Add(time.Duration(i) * a.buildInterval)
						}
					}
				}
			}
		} else {
			if a.initialized.Load() && a.labelWidth > 0 && a.showEllipsis {
				es := a.ellipsisStep.Add(1)
				if int(es) >= ellipsisAnimSpeed*len(ellipsisFrames) {
					a.ellipsisStep.Store(0)
				}
			} else if !a.initialized.Load() && time.Since(a.startTime) >= maxBirthOffset {
				a.initialized.Store(true)
			}
			if time.Since(a.cycleModeStart) >= a.cycleModeDuration {
				a.cycleMode = (a.cycleMode + 1) % 2
				a.cycleModeStart = time.Now()
			}
		}
		return a, a.Step()
	default:
		return a, nil
	}
}

func (a *Anim) View() string {
	var b strings.Builder
	step := int(a.step.Load())
	if a.buildLabel {
		now := time.Now()
		frameIdx := 0
		if len(a.labelGradFrames) > 0 {
			frameIdx = step % len(a.labelGradFrames)
		}
		if a.cycleReveal && a.cyclePhase == 2 {
			// Scramble back phase - show random characters sweeping left-to-right.
			total := len(a.labelRunes)
			elapsed := now.Sub(a.cyclePhaseStart)
			var scrambled int
			if a.scrambleBackDuration > 0 {
				frac := float64(elapsed) / float64(a.scrambleBackDuration)
				if frac < 0 {
					frac = 0
				}
				if frac > 1 {
					frac = 1
				}
				scrambled = int(math.Ceil(frac * float64(total)))
			} else {
				scrambled = total
			}
			if scrambled < 0 {
				scrambled = 0
			}
			if scrambled > total {
				scrambled = total
			}
			for i := 0; i < total; i++ {
				if i < scrambled {
					if step < len(a.scrambleFrames) && i < len(a.scrambleFrames[step]) {
						b.WriteString(a.scrambleFrames[step][i])
					} else {
						b.WriteRune(initialChar)
					}
					continue
				}
				if len(a.labelGradFrames) > 0 && frameIdx < len(a.labelGradFrames) && i < len(a.labelGradFrames[frameIdx]) {
					b.WriteString(a.labelGradFrames[frameIdx][i])
				} else if i < len(a.label) {
					b.WriteString(a.label[i])
				} else if i < len(a.labelRunes) {
					b.WriteString(string(a.labelRunes[i]))
				}
			}
			return b.String()
		}
		total := len(a.labelRunes)
		if total == 0 {
			return b.String()
		}
		if now.Before(a.revealStart) {
			dots := 0
			bd := a.revealStart.Sub(a.startTime)
			if bd > 0 {
				frac := float64(time.Since(a.startTime)) / float64(bd)
				if frac < 0 {
					frac = 0
				}
				if frac > 1 {
					frac = 1
				}
				eased := math.Pow(frac, dotBuildEasePow)
				dots = int(math.Ceil(eased * float64(total)))
			} else {
				dots = total
			}
			if total > 0 && dots < 1 {
				dots = 1
			}
			if dots < total {
				for i := 0; i < dots; i++ {
					if i < len(a.preDots) {
						b.WriteString(a.preDots[i])
					} else {
						b.WriteRune(initialChar)
					}
				}
				for i := dots; i < total; i++ {
					b.WriteRune(' ')
				}
				return b.String()
			}
		}
		if now.Before(a.shufflePreludeEnd) {
			for i := 0; i < total; i++ {
				if i < len(a.shuffleStartTimes) && now.Before(a.shuffleStartTimes[i]) {
					if i < len(a.preDots) {
						b.WriteString(a.preDots[i])
					} else {
						b.WriteRune(initialChar)
					}
					continue
				}
				if step < len(a.scrambleFrames) && i < len(a.scrambleFrames[step]) {
					b.WriteString(a.scrambleFrames[step][i])
				} else {
					b.WriteRune(initialChar)
				}
			}
			return b.String()
		}
		revealed := 0
		for revealed < len(a.revealTimes) && now.After(a.revealTimes[revealed]) {
			revealed++
		}
		for i := 0; i < total; i++ {
			if i < revealed {
				if len(a.labelGradFrames) > 0 && frameIdx < len(a.labelGradFrames) && i < len(a.labelGradFrames[frameIdx]) {
					b.WriteString(a.labelGradFrames[frameIdx][i])
				} else if i < len(a.label) {
					b.WriteString(a.label[i])
				} else {
					b.WriteString(string(a.labelRunes[i]))
				}
				continue
			}
			if step < len(a.scrambleFrames) && i < len(a.scrambleFrames[step]) {
				b.WriteString(a.scrambleFrames[step][i])
			} else {
				b.WriteRune(initialChar)
			}
		}
		return b.String()
	}
	if a.cycleMode == 0 {
		// Show cycling symbols
		total := a.cyclingCharWidth
		for i := 0; i < total; i++ {
			if !a.initialized.Load() && i < len(a.birthOffsets) && time.Since(a.startTime) < a.birthOffsets[i] {
				if step < len(a.initialFrames) && i < len(a.initialFrames[step]) {
					b.WriteString(a.initialFrames[step][i])
				} else {
					b.WriteRune(initialChar)
				}
			} else {
				if step < len(a.cyclingFrames) && i < len(a.cyclingFrames[step]) {
					b.WriteString(a.cyclingFrames[step][i])
				} else {
					b.WriteRune(initialChar)
				}
			}
		}
	} else {
		// Show gradient animated label text
		if len(a.labelGradFrames) > 0 {
			frameIdx := step % len(a.labelGradFrames)
			if frameIdx < len(a.labelGradFrames) {
				for _, char := range a.labelGradFrames[frameIdx] {
					b.WriteString(char)
				}
			}
		} else {
			for _, char := range a.label {
				b.WriteString(char)
			}
		}
		if a.showEllipsis {
			es := int(a.ellipsisStep.Load())
			frameIdx := es / ellipsisAnimSpeed
			if frameIdx >= 0 && frameIdx < len(a.ellipsisRendered) {
				b.WriteString(a.ellipsisRendered[frameIdx])
			}
		}
	}
	return b.String()
}

func (a *Anim) Step() tea.Cmd {
	return tea.Tick(time.Second/time.Duration(fps), func(time.Time) tea.Msg { return StepMsg{id: a.id} })
}

func makeGradientRamp(size int, stops ...color.Color) []color.Color {
	if len(stops) < 2 {
		return nil
	}
	points := make([]colorful.Color, len(stops))
	for i, k := range stops {
		points[i], _ = colorful.MakeColor(k)
	}
	numSegments := len(stops) - 1
	if numSegments <= 0 {
		return nil
	}
	blended := make([]color.Color, 0, size)
	segmentSizes := make([]int, numSegments)
	baseSize := size / numSegments
	remainder := size % numSegments
	for i := 0; i < numSegments; i++ {
		segmentSizes[i] = baseSize
		if i < remainder {
			segmentSizes[i]++
		}
	}
	for i := 0; i < numSegments; i++ {
		c1 := points[i]
		c2 := points[i+1]
		segSize := segmentSizes[i]
		if segSize <= 0 {
			continue
		}
		for j := 0; j < segSize; j++ {
			t := float64(j) / float64(segSize)
			c := c1.BlendHcl(c2, t)
			blended = append(blended, c)
		}
	}
	return blended
}

func colorIsUnset(c color.Color) bool {
	if c == nil {
		return true
	}
	_, _, _, a := c.RGBA()
	return a == 0
}
