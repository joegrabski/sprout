package sprout

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const bannerRaw = `
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢀⡀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣠⣿⣿⣆⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣰⣿⣿⣿⣿⣧⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀
⠀⢰⣶⣶⣤⣄⣀⠀⠀⠀⠀⢠⣿⣿⣿⣿⣿⣿⡆⠀⠀⠀⠀⢀⣀⣤⣴⣶⣶⠀
⠀⠈⣿⣿⣿⣿⣿⣿⣦⣄⠀⢸⣿⣿⣿⣿⣿⣿⡇⠀⢀⣤⣾⣿⣿⣿⣿⣿⡇⠀
⠀⠀⠸⣿⣿⣿⣿⣿⣿⣿⣷⣄⠙⣿⣿⣿⣿⡟⠁⣴⣿⣿⣿⣿⣿⣿⣿⡿⠀⠀
⠀⠀⠀⠙⢿⣿⣿⣿⣿⣿⣿⣿⣦⠈⢿⣿⠟⢀⣾⣿⣿⣿⣿⣿⣿⣿⠟⠁⠀⠀
⠀⠀⠀⠀⠀⠉⠛⠿⢿⣿⣿⣿⣿⣧⠈⠏⢠⣿⣿⣿⣿⣿⠿⠟⠋⠁⠀⠀⠀⠀
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠈⠉⠛⢿⣆⢀⣾⠟⠋⠉⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢿⣿⠇⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢸⣿⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣾⣿⡆⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣰⣿⣿⣷⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣠⣿⣿⣿⣿⣧⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀
⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠐⠛⠛⠛⠛⠛⠛⠃⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀
`

type ColorRGB struct {
	R, G, B int
}

func (c ColorRGB) ToHex() string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

func interpolateRGB(start, end ColorRGB, factor float64) ColorRGB {
	return ColorRGB{
		R: int(float64(start.R) + factor*float64(end.R-start.R)),
		G: int(float64(start.G) + factor*float64(end.G-start.G)),
		B: int(float64(start.B) + factor*float64(end.B-start.B)),
	}
}

func GetBannerANSI() string {
	lines := strings.Split(strings.Trim(bannerRaw, "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}

	startColor := ColorRGB{R: 0xb4, G: 0xbe, B: 0x82} // Muted Lime
	midColor := ColorRGB{R: 0xa3, G: 0xbe, B: 0x8c}   // Muted Green
	endColor := ColorRGB{R: 0x8f, G: 0xbc, B: 0xbb}   // Muted Emerald/Frost

	var sb strings.Builder
	for y, line := range lines {
		for x, r := range line {
			if r == ' ' {
				sb.WriteRune(r)
				continue
			}

			// Horizontal gradient factor
			factor := float64(x) / float64(len(line))
			var c ColorRGB
			if factor < 0.5 {
				c = interpolateRGB(startColor, midColor, factor*2)
			} else {
				c = interpolateRGB(midColor, endColor, (factor-0.5)*2)
			}

			// Add a bit of vertical shift for a diagonal effect
			vFactor := float64(y) / float64(len(lines))
			c = interpolateRGB(c, startColor, vFactor*0.2)

			// Use lipgloss for styling each character
			style := lipgloss.NewStyle().Foreground(lipgloss.Color(c.ToHex()))
			sb.WriteString(style.Render(string(r)))
		}
		sb.WriteRune('\n')
	}

	return sb.String()
}

func GetBanner() string {
	// For tview (TUI), we'll still use the [#hex] format if needed
	// But the user asked NOT to show the logo in TUI, so we just use ANSI for CLI.
	return GetBannerANSI()
}

func GetBannerPlain() string {
	return strings.Trim(bannerRaw, "\n")
}
