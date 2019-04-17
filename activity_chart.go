package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/golang/freetype/truetype"
	"github.com/wcharczuk/go-chart"
	"github.com/wcharczuk/go-chart/drawing"
	"github.com/wcharczuk/go-chart/util"
)

const daysPerWeek = 7

var activityChartDefaultColors = []drawing.Color{
	drawing.ColorFromHex("ebedf0"),
	drawing.ColorFromHex("c6e48b"),
	drawing.ColorFromHex("7bc96f"),
	drawing.ColorFromHex("239a3b"),
	drawing.ColorFromHex("196127"),
}

// ActivityChart draws a daily activity chart for one year
type ActivityChart struct {
	Title      string
	TitleStyle chart.Style

	ColorPalette chart.ColorPalette

	Width  int
	Height int
	DPI    float64

	DotSize    int
	DotSpacing int

	Background chart.Style
	Canvas     chart.Style

	Font        *truetype.Font
	defaultFont *truetype.Font

	XAxis chart.Style
	YAxis chart.Style

	Days []int

	// Layout info and other cached valued (all updated in `layout()`)
	titleX      int
	titleY      int
	chartX      int
	chartY      int
	chartWidth  int
	chartHeight int
	numWeeks    int
	maxValue    int
}

// GetColorPalette returns the color palette for the chart.
func (ac ActivityChart) GetColorPalette() chart.ColorPalette {
	if ac.ColorPalette != nil {
		return ac.ColorPalette
	}
	return chart.AlternateColorPalette
}

// GetWidth returns the chart width or the default value
func (ac ActivityChart) GetWidth() int {
	if ac.Width == 0 {
		return chart.DefaultChartWidth
	}
	return ac.Width
}

// GetHeight returns the chart height or the default value
func (ac ActivityChart) GetHeight() int {
	if ac.Width == 0 {
		return chart.DefaultChartHeight
	}
	return ac.Height
}

// GetDPI returns the chart DPI or the default value
func (ac ActivityChart) GetDPI() float64 {
	if ac.DPI == 0 {
		return chart.DefaultDPI
	}
	return ac.DPI
}

// GetDotSize returns the chart dot size or the default value
func (ac ActivityChart) GetDotSize() int {
	if ac.DotSize == 0 {
		return 16
	}
	return ac.DotSize
}

// GetDotSpacing returns the chart dot spacing or the default value
func (ac ActivityChart) GetDotSpacing() int {
	if ac.DotSpacing == 0 {
		return 2
	}
	return ac.DotSpacing
}

// GetBackgroundStyle returns the chart background style or the default value
func (ac ActivityChart) GetBackgroundStyle() chart.Style {
	return ac.Background.InheritFrom(ac.styleDefaultsBackground())
}

// GetFont returns the chart text font or the default value
func (ac ActivityChart) GetFont() *truetype.Font {
	if ac.Font == nil {
		return ac.defaultFont
	}
	return ac.Font
}

// Render renders the chart with the given renderer to the given io.Writer
func (ac ActivityChart) Render(rp chart.RendererProvider, w io.Writer) error {
	if len(ac.Days) == 0 {
		return errors.New("Please provide at least one day of activity")
	}

	// Set the chart default font
	if ac.Font == nil {
		defaultFont, err := chart.GetDefaultFont()
		if err != nil {
			return err
		}
		ac.defaultFont = defaultFont
	}

	// Create and init a new renderer
	r, err := rp(ac.GetWidth(), ac.GetHeight())
	if err != nil {
		return err
	}

	r.SetDPI(ac.GetDPI())

	// TODO: Remove this
	ac.Title = fmt.Sprintf("%d x %d @ %d dpi", ac.GetWidth(), ac.GetHeight(), int(ac.GetDPI()))
	ac.TitleStyle.Show = true

	// Draw
	ac.layout(r)

	ac.drawBackground(r)
	ac.drawTitle(r)
	ac.drawDots(r)

	return r.Save(w)
}

// Fills layout info
func (ac *ActivityChart) layout(r chart.Renderer) {
	r.SetFont(ac.TitleStyle.GetFont(ac.GetFont()))
	r.SetFontSize(ac.TitleStyle.GetFontSize(ac.getTitleFontSize()))

	titleBox := r.MeasureText(ac.Title)
	ac.titleX = (ac.GetWidth() - titleBox.Width()) / 2
	ac.titleY = ac.TitleStyle.Padding.GetTop(chart.DefaultTitleTop) + titleBox.Height()

	ac.numWeeks = (len(ac.Days) + daysPerWeek - 1) / daysPerWeek
	ac.chartWidth = ac.getChartAreaDim(ac.numWeeks)
	ac.chartHeight = ac.getChartAreaDim(daysPerWeek)

	ac.chartX = (ac.GetWidth() - ac.chartWidth) / 2
	ac.chartY = (ac.GetHeight() - ac.titleY - ac.chartHeight) / 2

	// Find max
	ac.maxValue = -1
	for _, value := range ac.Days {
		if value > ac.maxValue {
			ac.maxValue = value
		}
	}
}

func (ac ActivityChart) drawBackground(r chart.Renderer) {
	chart.Draw.Box(
		r,
		chart.Box{Right: ac.GetWidth(), Bottom: ac.GetHeight()},
		ac.GetBackgroundStyle(),
	)
}

func (ac ActivityChart) styleDefaultsBackground() chart.Style {
	return chart.Style{
		FillColor:   ac.GetColorPalette().BackgroundColor(),
		StrokeColor: ac.GetColorPalette().BackgroundStrokeColor(),
		StrokeWidth: chart.DefaultStrokeWidth,
	}
}

func (ac ActivityChart) drawTitle(r chart.Renderer) {
	if ac.TitleStyle.Show {
		r.SetFont(ac.TitleStyle.GetFont(ac.GetFont()))
		r.SetFontColor(ac.TitleStyle.GetFontColor(ac.GetColorPalette().TextColor()))
		r.SetFontSize(ac.TitleStyle.GetFontSize(ac.getTitleFontSize()))

		r.Text(ac.Title, ac.titleX, ac.titleY)
	}
}

func (ac ActivityChart) getTitleFontSize() float64 {
	effectiveDimension := util.Math.MinInt(ac.GetWidth(), ac.GetHeight())
	if effectiveDimension >= 2048 {
		return 48
	} else if effectiveDimension >= 1024 {
		return 24
	} else if effectiveDimension >= 512 {
		return 18
	} else if effectiveDimension >= 256 {
		return 12
	}
	return 10
}

func (ac ActivityChart) drawDots(r chart.Renderer) {
	size := ac.GetDotSize()
	spacing := ac.GetDotSpacing()

	for i, value := range ac.Days {
		week := i / daysPerWeek
		day := i % daysPerWeek

		x := ac.chartX + week*(size+spacing)
		y := ac.chartY + day*(size+spacing)

		box := chart.Box{
			Left:   x,
			Top:    y,
			Right:  x + size,
			Bottom: y + size,
		}

		chart.Draw.Box(r, box, ac.getDotStyle(value))
	}
}

func (ac ActivityChart) getChartAreaDim(numDots int) int {
	return numDots*ac.GetDotSize() + (numDots-1)*ac.GetDotSpacing()
}

func (ac ActivityChart) getDotStyle(value int) chart.Style {
	return chart.Style{
		FillColor:   ac.getDotColor(value),
		StrokeColor: ac.getDotColor(value),
		StrokeWidth: chart.DefaultStrokeWidth,
	}
}

func (ac ActivityChart) getDotColor(value int) drawing.Color {
	if value == 0 {
		return activityChartDefaultColors[0]
	}

	numColors := len(activityChartDefaultColors) - 1
	return activityChartDefaultColors[(value-1)*numColors/ac.maxValue+1]
}
