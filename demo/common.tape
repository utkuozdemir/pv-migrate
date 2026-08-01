# Settings shared by every demo recording, so the two GIFs cannot drift apart.
#
# The dimensions are twice what the README displays. GitHub renders the image at
# around 880 pixels wide, and recording at twice that leaves enough pixels for
# the text to stay sharp on a high density screen. The font size is doubled with
# them, which keeps the column count and the apparent text size unchanged.
#
# The colours come from GitHub's own dark theme, via its Primer primitives, so
# the terminal in the picture matches the page around it.

Set Shell "bash"
Set FontSize 36
Set Width 2800
Set Height 1240
Set Padding 48
Set Margin 32
Set MarginFill "#0d1117"
Set BorderRadius 20
Set WindowBar Colorful
Set WindowBarSize 80
Set Framerate 30
Set TypingSpeed 45ms
Set CursorBlink false

Set Theme {
  "background": "#0d1117",
  "foreground": "#f0f6fc",
  "cursorColor": "#f0f6fc",
  "selectionBackground": "#264f78",
  "black": "#2f3742",
  "red": "#ff7b72",
  "green": "#3fb950",
  "yellow": "#d29922",
  "blue": "#58a6ff",
  "purple": "#be8fff",
  "cyan": "#39c5cf",
  "white": "#f0f6fc",
  "brightBlack": "#656c76",
  "brightRed": "#ffa198",
  "brightGreen": "#56d364",
  "brightYellow": "#e3b341",
  "brightBlue": "#79c0ff",
  "brightPurple": "#d2a8ff",
  "brightCyan": "#56d4dd",
  "brightWhite": "#ffffff"
}
